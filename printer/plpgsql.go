package printer

import (
	"encoding/json"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// plContext holds state during PL/pgSQL body formatting.
type plContext struct {
	printer *Printer
	datums  []plDatum
}

func (ctx *plContext) w(s string) {
	ctx.printer.Builder.WriteString(s)
}

func (ctx *plContext) indent(level int) {
	for i := 0; i < level; i++ {
		ctx.w("\t")
	}
}

func (ctx *plContext) newlineIndent(level int) {
	ctx.w("\n")
	ctx.indent(level)
}

// formatSQL formats a SQL query string using the main SQL formatter.
// Returns the original string unchanged if parsing fails.
func formatSQL(query string) string {
	result, err := pg_query.Parse(query)
	if err != nil || len(result.Stmts) == 0 {
		return query
	}
	b := &strings.Builder{}
	p := &Printer{Builder: b}
	p.Print(result.Stmts[0].Stmt)
	return b.String()
}

// compactSQL collapses a formatted SQL string to a single line.
func compactSQL(formatted string) string {
	return strings.Join(strings.Fields(formatted), " ")
}

// writeSQL formats a SQL query and writes it. Uses compact single-line form
// if it fits within ~100 characters at the current indent level.
func (ctx *plContext) writeSQL(query string, ind int) {
	formatted := formatSQL(query)
	compact := compactSQL(formatted)
	if len(compact) <= 100-ind*4 {
		ctx.w(compact)
		return
	}
	ctx.writeIndented(formatted, ind)
}

// writeIndented writes a possibly multi-line string, re-indenting continuation lines.
func (ctx *plContext) writeIndented(s string, ind int) {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			ctx.newlineIndent(ind)
		}
		ctx.w(line)
	}
}

var raiseLevelName = map[int]string{
	10: "DEBUG",
	14: "DEBUG",
	15: "LOG",
	17: "INFO",
	18: "NOTICE",
	19: "WARNING",
	20: "EXCEPTION",
	21: "EXCEPTION",
}

// formatPLpgSQLBody parses and formats a PL/pgSQL function body.
func (output *Printer) formatPLpgSQLBody(body string, indentLevel int) {
	wrappers := []string{
		"CREATE FUNCTION _plpgsql_fmt_() RETURNS void AS $$",
		"CREATE FUNCTION _plpgsql_fmt_() RETURNS SETOF record AS $$",
	}

	var jsonResult string
	var err error
	for _, prefix := range wrappers {
		stmt := prefix + body + "\n$$ LANGUAGE plpgsql;"
		jsonResult, err = pg_query.ParsePlPgSqlToJSON(stmt)
		if err == nil {
			break
		}
	}
	if err != nil {
		output.Builder.WriteString("\n")
		output.Builder.WriteString(body)
		return
	}

	var parsed []plFunctionW
	if err := json.Unmarshal([]byte(jsonResult), &parsed); err != nil || len(parsed) == 0 {
		output.Builder.WriteString("\n")
		output.Builder.WriteString(body)
		return
	}

	fn := &parsed[0].F
	ctx := &plContext{
		printer: output,
		datums:  fn.Datums,
	}

	ctx.writeDeclare(indentLevel)
	ctx.writeBlock(&fn.Action.B, indentLevel, true)
}

// writeDeclare emits the DECLARE section for user-declared variables.
func (ctx *plContext) writeDeclare(ind int) {
	var decls []string
	for i := range ctx.datums {
		d := &ctx.datums[i]
		switch {
		case d.Var != nil:
			if decl := varDecl(d.Var); decl != "" {
				decls = append(decls, decl)
			}
		case d.Rec != nil:
			name := d.Rec.RefName
			if name == "" || strings.HasPrefix(name, "(") || d.Rec.LineNo == 0 {
				continue
			}
			decls = append(decls, name+" record")
		}
	}

	if len(decls) > 0 {
		ctx.newlineIndent(ind)
		ctx.w("DECLARE")
		for _, d := range decls {
			ctx.newlineIndent(ind + 1)
			ctx.w(d + ";")
		}
	}
}

// varDecl builds a DECLARE line for a PLpgSQL_var, or returns "" to skip it.
func varDecl(v *plVar) string {
	if v.RefName == "" || strings.HasPrefix(v.RefName, "__") {
		return ""
	}
	if v.LineNo == 0 {
		return "" // implicit (found, params)
	}
	if v.IsConst && (v.RefName == "sqlstate" || v.RefName == "sqlerrm") {
		return "" // implicit exception variables
	}
	if v.DataType == nil {
		return ""
	}
	typ := strings.TrimSpace(v.DataType.T.TypeName)
	if typ == "" || typ == "UNKNOWN" {
		return ""
	}
	// Skip parser-inferred types (FOR loop variables, etc.)
	if strings.HasPrefix(typ, "pg_catalog.") {
		return ""
	}

	var parts []string
	parts = append(parts, v.RefName)
	if v.IsConst {
		parts = append(parts, "CONSTANT")
	}
	parts = append(parts, typ)
	if v.NotNull {
		parts = append(parts, "NOT NULL")
	}
	decl := strings.Join(parts, " ")

	if v.DefaultVal != nil && v.DefaultVal.E.Query != "" {
		decl += " := " + v.DefaultVal.E.Query
	}
	return decl
}

// writeBlock emits a BEGIN/[EXCEPTION/]END block.
func (ctx *plContext) writeBlock(block *plStmtBlock, ind int, topLevel bool) {
	body := block.Body
	exceptions := block.Exceptions

	// Detect parser-generated wrapper: [inner_block_with_exceptions, bare_return]
	// and flatten to a single block.
	if exceptions == nil && len(body) >= 1 && body[0].Block != nil {
		inner := body[0].Block
		if inner.Exceptions != nil {
			allTrailingBare := true
			for _, s := range body[1:] {
				if s.Return != nil && s.Return.Expr == nil && s.Return.LineNo == 0 {
					continue
				}
				allTrailingBare = false
				break
			}
			if allTrailingBare {
				body = inner.Body
				exceptions = inner.Exceptions
				block = inner
			}
		}
	}

	ctx.newlineIndent(ind)
	if block.Label != "" {
		ctx.w("<<" + block.Label + ">>\n")
		ctx.indent(ind)
	}

	ctx.w("BEGIN")
	ctx.writeStmts(body, ind+1)

	if exceptions != nil {
		ctx.newlineIndent(ind)
		ctx.w("EXCEPTION")
		for _, ew := range exceptions.B.ExcList {
			exc := &ew.E
			var condNames []string
			for _, cw := range exc.Conditions {
				condNames = append(condNames, cw.C.CondName)
			}
			ctx.newlineIndent(ind + 1)
			ctx.w("WHEN " + strings.Join(condNames, " OR ") + " THEN")
			ctx.writeStmts(exc.Action, ind+2)
		}
	}

	ctx.newlineIndent(ind)
	ctx.w("END")
	if !topLevel {
		ctx.w(";")
	}
}

// writeStmts emits a list of PL/pgSQL statements.
func (ctx *plContext) writeStmts(stmts []plStmt, ind int) {
	for i := range stmts {
		ctx.writeStmt(&stmts[i], ind)
	}
}

func (ctx *plContext) writeStmt(s *plStmt, ind int) {
	switch {
	case s.Assign != nil:
		ctx.newlineIndent(ind)
		ctx.w(s.Assign.Expr.E.Query + ";")
	case s.If != nil:
		ctx.writeIf(s.If, ind)
	case s.Case != nil:
		ctx.writeCase(s.Case, ind)
	case s.Loop != nil:
		ctx.writeLoop(s.Loop, ind)
	case s.While != nil:
		ctx.newlineIndent(ind)
		ctx.w("WHILE " + s.While.Cond.E.Query + " LOOP")
		ctx.writeStmts(s.While.Body, ind+1)
		ctx.newlineIndent(ind)
		ctx.w("END LOOP;")
	case s.ForI != nil:
		ctx.writeForI(s.ForI, ind)
	case s.ForS != nil:
		ctx.newlineIndent(ind)
		ctx.w("FOR " + s.ForS.Var.name() + " IN ")
		ctx.writeSQL(s.ForS.Query.E.Query, ind)
		ctx.w(" LOOP")
		ctx.writeStmts(s.ForS.Body, ind+1)
		ctx.newlineIndent(ind)
		ctx.w("END LOOP;")
	case s.ForEachA != nil:
		ctx.newlineIndent(ind)
		varName := ctx.getDatumName(s.ForEachA.VarNo)
		ctx.w("FOREACH " + varName + " IN ARRAY " + s.ForEachA.Expr.E.Query + " LOOP")
		ctx.writeStmts(s.ForEachA.Body, ind+1)
		ctx.newlineIndent(ind)
		ctx.w("END LOOP;")
	case s.Exit != nil:
		ctx.writeExit(s.Exit, ind)
	case s.Return != nil:
		ctx.newlineIndent(ind)
		if s.Return.Expr != nil {
			ctx.w("RETURN " + s.Return.Expr.E.Query + ";")
		} else {
			ctx.w("RETURN;")
		}
	case s.ReturnNext != nil:
		ctx.newlineIndent(ind)
		if s.ReturnNext.Expr != nil {
			ctx.w("RETURN NEXT " + s.ReturnNext.Expr.E.Query + ";")
		} else {
			ctx.w("RETURN NEXT;")
		}
	case s.ReturnQuery != nil:
		ctx.newlineIndent(ind)
		ctx.w("RETURN QUERY ")
		ctx.writeSQL(s.ReturnQuery.Query.query(), ind)
		ctx.w(";")
	case s.Raise != nil:
		ctx.writeRaise(s.Raise, ind)
	case s.ExecSQL != nil:
		ctx.writeExecSQL(s.ExecSQL, ind)
	case s.Perform != nil:
		ctx.writePerform(s.Perform, ind)
	case s.DynExecute != nil:
		ctx.writeDynExecute(s.DynExecute, ind)
	case s.Block != nil:
		ctx.writeBlock(s.Block, ind, false)
	}
}

func (ctx *plContext) writeIf(node *plStmtIf, ind int) {
	ctx.newlineIndent(ind)
	ctx.w("IF " + node.Cond.E.Query + " THEN")
	ctx.writeStmts(node.ThenBody, ind+1)

	for _, ew := range node.ElsIfList {
		ctx.newlineIndent(ind)
		ctx.w("ELSIF " + ew.E.Cond.E.Query + " THEN")
		ctx.writeStmts(ew.E.Stmts, ind+1)
	}

	if len(node.ElseBody) > 0 {
		ctx.newlineIndent(ind)
		ctx.w("ELSE")
		ctx.writeStmts(node.ElseBody, ind+1)
	}

	ctx.newlineIndent(ind)
	ctx.w("END IF;")
}

func (ctx *plContext) writeCase(node *plStmtCase, ind int) {
	ctx.newlineIndent(ind)
	hasTestExpr := node.TExpr != nil
	if hasTestExpr {
		ctx.w("CASE " + node.TExpr.E.Query)
	} else {
		ctx.w("CASE")
	}

	for _, ww := range node.CaseWhenList {
		w := &ww.W
		ctx.newlineIndent(ind + 1)
		expr := w.Expr.E.Query
		if hasTestExpr {
			expr = extractCaseWhenValue(expr)
		}
		ctx.w("WHEN " + expr + " THEN")
		ctx.writeStmts(w.Stmts, ind+2)
	}

	if node.HaveElse && len(node.ElseStmts) > 0 {
		ctx.newlineIndent(ind + 1)
		ctx.w("ELSE")
		ctx.writeStmts(node.ElseStmts, ind+2)
	}

	ctx.newlineIndent(ind)
	ctx.w("END CASE;")
}

// extractCaseWhenValue extracts the value from a pattern like
// "__Case__Variable_N__" IN (val1, val2).
func extractCaseWhenValue(expr string) string {
	idx := strings.Index(expr, " IN (")
	if idx < 0 {
		return expr
	}
	val := expr[idx+5:]
	if strings.HasSuffix(val, ")") {
		val = val[:len(val)-1]
	}
	return val
}

func (ctx *plContext) writeLoop(node *plStmtLoop, ind int) {
	ctx.newlineIndent(ind)
	if node.Label != "" {
		ctx.w("<<" + node.Label + ">>\n")
		ctx.indent(ind)
	}
	ctx.w("LOOP")
	ctx.writeStmts(node.Body, ind+1)
	ctx.newlineIndent(ind)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeForI(node *plStmtForI, ind int) {
	ctx.newlineIndent(ind)
	ctx.w("FOR " + node.Var.name() + " IN ")
	if node.Reverse {
		ctx.w("REVERSE ")
	}
	ctx.w(node.Lower.E.Query + ".." + node.Upper.E.Query)
	if node.Step != nil {
		ctx.w(" BY " + node.Step.E.Query)
	}
	ctx.w(" LOOP")
	ctx.writeStmts(node.Body, ind+1)
	ctx.newlineIndent(ind)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeExit(node *plStmtExit, ind int) {
	ctx.newlineIndent(ind)
	if node.IsExit {
		ctx.w("EXIT")
	} else {
		ctx.w("CONTINUE")
	}
	if node.Label != "" {
		ctx.w(" " + node.Label)
	}
	if node.Cond != nil {
		ctx.w(" WHEN " + node.Cond.E.Query)
	}
	ctx.w(";")
}

func (ctx *plContext) writeRaise(node *plStmtRaise, ind int) {
	ctx.newlineIndent(ind)

	if node.Message == "" && len(node.Params) == 0 {
		ctx.w("RAISE;")
		return
	}

	levelStr := raiseLevelName[node.ElogLevel]
	if levelStr == "" {
		levelStr = "EXCEPTION"
	}

	ctx.w("RAISE " + levelStr)
	if node.Message != "" {
		ctx.w(" '" + node.Message + "'")
	}
	for _, p := range node.Params {
		ctx.w(", " + p.E.Query)
	}
	ctx.w(";")
}

func (ctx *plContext) writeExecSQL(node *plStmtExecSQL, ind int) {
	formatted := formatSQL(node.SQLStmt.E.Query)

	insertInto := func(sql, sep string) string {
		intoClause := "INTO "
		if node.Strict {
			intoClause = "INTO STRICT "
		}
		intoClause += node.Target.fieldNames()

		upper := strings.ToUpper(sql)
		if fromIdx := strings.Index(upper, sep+"FROM"); fromIdx >= 0 {
			return sql[:fromIdx] + sep + intoClause + sql[fromIdx:]
		}
		return sql + " " + intoClause
	}

	ctx.newlineIndent(ind)
	if node.Into {
		compact := insertInto(compactSQL(formatted), " ")
		if len(compact) <= 100-ind*4 {
			ctx.w(compact + ";")
			return
		}
		formatted = insertInto(formatted, "\n")
	} else {
		compact := compactSQL(formatted)
		if len(compact) <= 100-ind*4 {
			ctx.w(compact + ";")
			return
		}
	}

	ctx.writeIndented(formatted, ind)
	ctx.w(";")
}

func (ctx *plContext) writePerform(node *plStmtPerform, ind int) {
	// Parser converts PERFORM to SELECT; format as SQL then swap back
	formatted := formatSQL(node.Expr.E.Query)

	swapSelectToPerform := func(s string) string {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(strings.ToUpper(s), "SELECT") {
			return "PERFORM" + s[6:]
		}
		return "PERFORM " + s
	}

	compact := swapSelectToPerform(compactSQL(formatted))
	ctx.newlineIndent(ind)
	if len(compact) <= 100-ind*4 {
		ctx.w(compact)
	} else {
		ctx.writeIndented(swapSelectToPerform(formatted), ind)
	}
	ctx.w(";")
}

func (ctx *plContext) writeDynExecute(node *plStmtDynExecute, ind int) {
	ctx.newlineIndent(ind)
	ctx.w("EXECUTE " + node.Query.E.Query)

	if node.Into {
		ctx.w(" INTO ")
		if node.Strict {
			ctx.w("STRICT ")
		}
		ctx.w(node.Target.fieldNames())
	}

	if len(node.Params) > 0 {
		ctx.w(" USING ")
		for i, p := range node.Params {
			if i > 0 {
				ctx.w(", ")
			}
			ctx.w(p.E.Query)
		}
	}

	ctx.w(";")
}

// getDatumName looks up a datum name by index.
func (ctx *plContext) getDatumName(varno int) string {
	if varno < 0 || varno >= len(ctx.datums) {
		return "???"
	}
	if name := ctx.datums[varno].name(); name != "" {
		return name
	}
	return "???"
}
