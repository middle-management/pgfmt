package printer

import (
	"encoding/json"
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/encoding/protojson"
)

// plComment represents a comment extracted from a PL/pgSQL body.
type plComment struct {
	text   string
	lineNo int // line number in the wrapped CREATE FUNCTION statement
}

// extractPLComments scans a PL/pgSQL body string for comments, returning
// them with line numbers relative to the wrapped statement (prefix is line 1).
func extractPLComments(body string) []plComment {
	var comments []plComment
	lineNo := 1 // prefix "CREATE FUNCTION ... AS $$" is line 1
	i := 0
	for i < len(body) {
		ch := body[i]
		switch {
		case ch == '\n':
			lineNo++
			i++
		case ch == '-' && i+1 < len(body) && body[i+1] == '-':
			start := i
			cLine := lineNo
			i += 2
			for i < len(body) && body[i] != '\n' {
				i++
			}
			text := strings.TrimRight(body[start:i], " \t")
			comments = append(comments, plComment{text: text, lineNo: cLine})
		case ch == '/' && i+1 < len(body) && body[i+1] == '*':
			start := i
			cLine := lineNo
			i += 2
			for i+1 < len(body) {
				if body[i] == '*' && body[i+1] == '/' {
					i += 2
					break
				}
				if body[i] == '\n' {
					lineNo++
				}
				i++
			}
			comments = append(comments, plComment{text: body[start:i], lineNo: cLine})
		case ch == '\'':
			// Skip single-quoted string literals.
			i++
			for i < len(body) {
				if body[i] == '\'' {
					i++
					if i < len(body) && body[i] == '\'' {
						i++ // escaped ''
						continue
					}
					break
				}
				if body[i] == '\n' {
					lineNo++
				}
				i++
			}
		case ch == '$':
			// Skip dollar-quoted strings ($tag$...$tag$).
			j := i + 1
			for j < len(body) && (body[j] == '_' ||
				(body[j] >= 'a' && body[j] <= 'z') ||
				(body[j] >= 'A' && body[j] <= 'Z') ||
				(body[j] >= '0' && body[j] <= '9')) {
				j++
			}
			if j < len(body) && body[j] == '$' {
				tag := body[i : j+1]
				i = j + 1
				for i < len(body) {
					if body[i] == '$' && strings.HasPrefix(body[i:], tag) {
						i += len(tag)
						break
					}
					if body[i] == '\n' {
						lineNo++
					}
					i++
				}
			} else {
				i++
			}
		default:
			i++
		}
	}
	return comments
}

// plContext holds state during PL/pgSQL body formatting.
type plContext struct {
	printer    *Printer
	datums     []plDatum
	comments   []plComment
	commentIdx int
}

// emitCommentsBeforeLine writes all pending comments with lineNo < line.
func (ctx *plContext) emitCommentsBeforeLine(line int, ind int) {
	for ctx.commentIdx < len(ctx.comments) && ctx.comments[ctx.commentIdx].lineNo < line {
		ctx.newlineIndent(ind)
		ctx.w(ctx.comments[ctx.commentIdx].text)
		ctx.commentIdx++
	}
}

// flushComments emits all remaining comments.
func (ctx *plContext) flushComments(ind int) {
	ctx.emitCommentsBeforeLine(1<<31-1, ind)
}

// skipSQLComments advances the comment index past comments that fall within
// the line range of a SQL expression starting at startLine. This prevents
// comments inside SQL expressions (already handled by formatSQL) from being
// emitted again by the PL/pgSQL comment handler.
func (ctx *plContext) skipSQLComments(startLine int, query string) {
	endLine := startLine + strings.Count(query, "\n")
	for ctx.commentIdx < len(ctx.comments) && ctx.comments[ctx.commentIdx].lineNo <= endLine {
		ctx.commentIdx++
	}
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
// Preserves inline comments when possible.
func formatSQL(query string) string {
	result, err := pgParse(query)
	if err != nil || len(result.Stmts) == 0 {
		return query
	}

	scanResult, scanErr := pgScan(query)
	if scanErr != nil {
		b := &strings.Builder{}
		p := &Printer{Builder: b, RawStmt: result.Stmts[0], OriginalSQL: query}
		p.Print(result.Stmts[0].Stmt)
		return b.String()
	}

	allComments := ExtractComments(query, scanResult)
	stmt := result.Stmts[0]
	stmtEnd := stmtEndPos(stmt, int32(len(query)))
	realStart := FirstRealTokenStart(scanResult, stmt.StmtLocation, stmtEnd)

	b := &strings.Builder{}
	ci := 0

	// Emit leading comments (before the first real token).
	for ci < len(allComments) && allComments[ci].start < realStart {
		b.WriteString(allComments[ci].text)
		b.WriteString("\n")
		ci++
	}

	// Collect inline comments (within the statement body).
	var inlineComments []comment
	for ci < len(allComments) && allComments[ci].start < stmtEnd {
		inlineComments = append(inlineComments, allComments[ci])
		ci++
	}

	p := &Printer{Builder: b, comments: inlineComments, RawStmt: stmt, OriginalSQL: query}
	p.Print(stmt.Stmt)

	// Emit trailing comments (after the statement).
	for ci < len(allComments) {
		b.WriteString("\n")
		b.WriteString(allComments[ci].text)
		ci++
	}

	return b.String()
}

// compactSQL collapses a formatted SQL string to a single line.
func compactSQL(formatted string) string {
	s := strings.Join(strings.Fields(formatted), " ")
	// Multi-line key/value calls put newlines directly inside parentheses;
	// drop the space they leave behind when collapsed.
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, " )", ")")
	return s
}

// hasLineComment reports whether s contains a SQL line comment (--).
func hasLineComment(s string) bool {
	return strings.Contains(s, "--")
}

// writeSQL formats a SQL query and writes it. Uses compact single-line form
// if it fits within ~100 characters at the current indent level.
func (ctx *plContext) writeSQL(query string, ind int) {
	formatted := ctx.formatSQLCached(query)
	if !hasLineComment(formatted) {
		compact := compactSQL(formatted)
		if len(compact) <= 100-ind*4 {
			ctx.w(compact)
			return
		}
	}
	ctx.writeIndented(formatted, ind)
}

// formatSQLCached formats a SQL query, using the printer's bodyCache when available.
func (ctx *plContext) formatSQLCached(query string) string {
	cache := ctx.printer.bodyCache
	if cached, ok := cache[query]; ok {
		// The cache stores protojson-encoded ParseResult.
		result := &pg_query.ParseResult{}
		if err := protojson.Unmarshal([]byte(cached), result); err == nil && len(result.Stmts) > 0 {
			b := &strings.Builder{}
			p := &Printer{Builder: b, bodyCache: cache, RawStmt: result.Stmts[0], OriginalSQL: query}
			p.Print(result.Stmts[0].Stmt)
			return b.String()
		}
	}
	return formatSQL(query)
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
		// Check bodyCache first.
		if cached, ok := output.bodyCache[stmt]; ok {
			jsonResult = cached
			err = nil
			break
		}
		jsonResult, err = pgParsePlPgSqlToJSON(stmt)
		if err == nil {
			break
		}
	}
	if err != nil {
		warn("failed to parse PL/pgSQL body: %v", err)
		output.writeRawPLpgSQLBody(body)
		return
	}

	var parsed []plFunctionW
	if err := json.Unmarshal([]byte(jsonResult), &parsed); err != nil || len(parsed) == 0 {
		warn("failed to unmarshal PL/pgSQL JSON: %v", err)
		output.writeRawPLpgSQLBody(body)
		return
	}

	fn := &parsed[0].F
	ctx := &plContext{
		printer:  output,
		datums:   fn.Datums,
		comments: extractPLComments(body),
	}

	ctx.writeDeclare(indentLevel)
	ctx.writeBlock(&fn.Action.B, indentLevel, true)
}

// writeRawPLpgSQLBody emits a body that could not be formatted, verbatim.
// Surrounding newlines are normalized so repeated formatting is idempotent
// (the fallback itself adds one leading newline each time).
func (output *Printer) writeRawPLpgSQLBody(body string) {
	output.Builder.WriteString("\n")
	output.Builder.WriteString(strings.Trim(body, "\n"))
}

// writeDeclare emits the DECLARE section for user-declared variables.
func (ctx *plContext) writeDeclare(ind int) {
	type declInfo struct {
		text   string
		lineNo int
	}

	// Variables that exist only as bound-cursor arguments are re-emitted
	// inside the cursor's argument list, not as standalone declarations.
	cursorArgs := map[int]bool{}
	for i := range ctx.datums {
		v := ctx.datums[i].Var
		if v == nil || v.CursorExplicitExpr == nil || v.CursorArgRow <= 0 || v.CursorArgRow >= len(ctx.datums) {
			continue
		}
		if row := ctx.datums[v.CursorArgRow].Row; row != nil {
			cursorArgs[v.CursorArgRow] = true
			for _, f := range row.Fields {
				cursorArgs[f.VarNo] = true
			}
		}
	}

	var decls []declInfo
	seen := map[string]bool{} // dedupe implicit loop records that shadow declared ones
	for i := range ctx.datums {
		if cursorArgs[i] {
			continue
		}
		d := &ctx.datums[i]
		switch {
		case d.Var != nil && d.Var.CursorExplicitExpr != nil:
			if decl := ctx.cursorDecl(d.Var); decl != "" {
				decls = append(decls, declInfo{text: decl, lineNo: d.Var.LineNo})
			}
		case d.Var != nil:
			if decl := varDecl(d.Var); decl != "" {
				decls = append(decls, declInfo{text: decl, lineNo: d.Var.LineNo})
			}
		case d.Rec != nil:
			name := d.Rec.RefName
			if name == "" || strings.HasPrefix(name, "(") || d.Rec.LineNo == 0 {
				continue
			}
			decls = append(decls, declInfo{text: name + " record", lineNo: d.Rec.LineNo})
		}
	}

	if len(decls) > 0 {
		ctx.newlineIndent(ind)
		ctx.w("DECLARE")
		for _, d := range decls {
			if seen[d.text] {
				continue
			}
			seen[d.text] = true
			ctx.emitCommentsBeforeLine(d.lineNo, ind+1)
			ctx.newlineIndent(ind + 1)
			ctx.w(d.text + ";")
		}
	}
}

// cursorDecl builds a DECLARE line for a bound cursor:
// name CURSOR [(arg type, ...)] FOR query
func (ctx *plContext) cursorDecl(v *plVar) string {
	if v.RefName == "" || v.LineNo == 0 {
		return ""
	}
	decl := v.RefName + " CURSOR"
	if v.CursorArgRow > 0 && v.CursorArgRow < len(ctx.datums) {
		if row := ctx.datums[v.CursorArgRow].Row; row != nil {
			var args []string
			for _, f := range row.Fields {
				arg := f.Name
				if f.VarNo >= 0 && f.VarNo < len(ctx.datums) {
					if av := ctx.datums[f.VarNo].Var; av != nil && av.DataType != nil {
						arg += " " + strings.TrimSpace(av.DataType.T.TypeName)
					}
				}
				args = append(args, arg)
			}
			decl += " (" + strings.Join(args, ", ") + ")"
		}
	}
	return decl + " FOR " + compactSQL(ctx.formatSQLCached(v.CursorExplicitExpr.E.Query))
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

	// Emit comments between DECLARE and BEGIN.
	if block.LineNo > 0 {
		ctx.emitCommentsBeforeLine(block.LineNo, ind)
	}

	ctx.newlineIndent(ind)
	ctx.writeLabel(block.Label, ind)

	ctx.w("BEGIN")
	ctx.writeBody(body, ind+1)

	// Flush trailing comments inside the block body (before END/EXCEPTION).
	if topLevel {
		ctx.flushComments(ind + 1)
	}

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
			ctx.writeBody(exc.Action, ind+2)
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
		if ln := stmts[i].lineNo(); ln > 0 {
			ctx.emitCommentsBeforeLine(ln, ind)
		}
		ctx.writeStmt(&stmts[i], ind)
	}
}

// writeBody emits a statement list, falling back to NULL; when it produces
// no output. The PL/pgSQL parser compiles NULL statements away entirely, so
// a body that was only `NULL;` arrives as an empty (or implicit-RETURN-only)
// list and would otherwise be silently dropped.
func (ctx *plContext) writeBody(stmts []plStmt, ind int) {
	before := ctx.printer.Builder.Len()
	ctx.writeStmts(stmts, ind)
	if ctx.printer.Builder.Len() == before {
		ctx.newlineIndent(ind)
		ctx.w("NULL;")
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
		ctx.writeLabel(s.While.Label, ind)
		ctx.w("WHILE " + s.While.Cond.E.Query + " LOOP")
		ctx.writeBody(s.While.Body, ind+1)
		ctx.newlineIndent(ind)
		ctx.w("END LOOP;")
	case s.ForI != nil:
		ctx.writeForI(s.ForI, ind)
	case s.ForS != nil:
		ctx.newlineIndent(ind)
		ctx.writeLabel(s.ForS.Label, ind)
		ctx.w("FOR " + s.ForS.Var.name() + " IN ")
		ctx.writeSQL(s.ForS.Query.E.Query, ind)
		ctx.skipSQLComments(s.ForS.LineNo, s.ForS.Query.E.Query)
		ctx.w(" LOOP")
		ctx.writeBody(s.ForS.Body, ind+1)
		ctx.newlineIndent(ind)
		ctx.w("END LOOP;")
	case s.ForC != nil:
		ctx.writeForC(s.ForC, ind)
	case s.DynForS != nil:
		ctx.writeDynForS(s.DynForS, ind)
	case s.ForEachA != nil:
		ctx.newlineIndent(ind)
		ctx.writeLabel(s.ForEachA.Label, ind)
		varName := ctx.getDatumName(s.ForEachA.VarNo)
		ctx.w("FOREACH " + varName)
		if s.ForEachA.Slice > 0 {
			ctx.w(fmt.Sprintf(" SLICE %d", s.ForEachA.Slice))
		}
		ctx.w(" IN ARRAY " + s.ForEachA.Expr.E.Query + " LOOP")
		ctx.writeBody(s.ForEachA.Body, ind+1)
		ctx.newlineIndent(ind)
		ctx.w("END LOOP;")
	case s.Exit != nil:
		ctx.writeExit(s.Exit, ind)
	case s.Return != nil:
		// Skip implicit parser-generated bare RETURN at end of void functions.
		if s.Return.Expr == nil && s.Return.LineNo == 0 {
			return
		}
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
		if s.ReturnQuery.DynQuery != nil {
			ctx.w("RETURN QUERY EXECUTE " + s.ReturnQuery.DynQuery.E.Query)
			ctx.writeUsingParams(s.ReturnQuery.Params)
			ctx.w(";")
			return
		}
		ctx.w("RETURN QUERY ")
		ctx.writeSQL(s.ReturnQuery.Query.query(), ind)
		ctx.skipSQLComments(s.ReturnQuery.LineNo, s.ReturnQuery.Query.query())
		ctx.w(";")
	case s.Raise != nil:
		ctx.writeRaise(s.Raise, ind)
	case s.ExecSQL != nil:
		ctx.writeExecSQL(s.ExecSQL, ind)
		ctx.skipSQLComments(s.ExecSQL.LineNo, s.ExecSQL.SQLStmt.E.Query)
	case s.Perform != nil:
		ctx.writePerform(s.Perform, ind)
		ctx.skipSQLComments(s.Perform.LineNo, s.Perform.Expr.E.Query)
	case s.DynExecute != nil:
		ctx.writeDynExecute(s.DynExecute, ind)
	case s.Block != nil:
		ctx.writeBlock(s.Block, ind, false)
	case s.Call != nil:
		// expr.query holds the full CALL/DO statement text. Emit it verbatim:
		// running it through the SQL formatter could rewrite an inner DO body
		// with $$ delimiters, which would break the enclosing body's own
		// dollar quoting.
		ctx.newlineIndent(ind)
		ctx.w(s.Call.Expr.E.Query + ";")
		ctx.skipSQLComments(s.Call.LineNo, s.Call.Expr.E.Query)
	case s.Commit != nil:
		ctx.newlineIndent(ind)
		ctx.w("COMMIT")
		if s.Commit.Chain {
			ctx.w(" AND CHAIN")
		}
		ctx.w(";")
	case s.Rollback != nil:
		ctx.newlineIndent(ind)
		ctx.w("ROLLBACK")
		if s.Rollback.Chain {
			ctx.w(" AND CHAIN")
		}
		ctx.w(";")
	case s.GetDiag != nil:
		ctx.newlineIndent(ind)
		ctx.w("GET ")
		if s.GetDiag.IsStacked {
			ctx.w("STACKED ")
		}
		ctx.w("DIAGNOSTICS ")
		for i, item := range s.GetDiag.DiagItems {
			if i > 0 {
				ctx.w(", ")
			}
			ctx.w(ctx.getDatumName(item.I.Target) + " = " + item.I.Kind)
		}
		ctx.w(";")
	case s.Assert != nil:
		ctx.newlineIndent(ind)
		ctx.w("ASSERT " + s.Assert.Cond.E.Query)
		if s.Assert.Message != nil {
			ctx.w(", " + s.Assert.Message.E.Query)
		}
		ctx.w(";")
	case s.Open != nil:
		ctx.writeOpen(s.Open, ind)
	case s.Fetch != nil:
		ctx.writeFetch(s.Fetch, ind)
	case s.Close != nil:
		ctx.newlineIndent(ind)
		ctx.w("CLOSE " + ctx.getDatumName(s.Close.CurVar) + ";")
	}
}

// writeLabel emits a <<label>> line before a loop or block keyword.
func (ctx *plContext) writeLabel(label string, ind int) {
	if label != "" {
		ctx.w("<<" + label + ">>\n")
		ctx.indent(ind)
	}
}

// writeUsingParams emits a " USING p1, p2" suffix for dynamic SQL statements.
func (ctx *plContext) writeUsingParams(params []plExprW) {
	for i, p := range params {
		if i == 0 {
			ctx.w(" USING ")
		} else {
			ctx.w(", ")
		}
		ctx.w(p.E.Query)
	}
}

func (ctx *plContext) writeOpen(node *plStmtOpen, ind int) {
	ctx.newlineIndent(ind)
	ctx.w("OPEN " + ctx.getDatumName(node.CurVar))
	switch {
	case node.Query != nil:
		ctx.w(" FOR ")
		ctx.writeSQL(node.Query.E.Query, ind)
		ctx.skipSQLComments(node.LineNo, node.Query.E.Query)
	case node.DynQuery != nil:
		ctx.w(" FOR EXECUTE " + node.DynQuery.E.Query)
		ctx.writeUsingParams(node.Params)
	case node.ArgQuery != nil:
		ctx.w("(" + node.ArgQuery.E.Query + ")")
	}
	ctx.w(";")
}

func (ctx *plContext) writeFetch(node *plStmtFetch, ind int) {
	ctx.newlineIndent(ind)
	if node.IsMove {
		ctx.w("MOVE ")
	} else {
		ctx.w("FETCH ")
	}

	// Reconstruct the direction clause. The exact source keyword (NEXT vs
	// FORWARD, LAST vs ABSOLUTE -1) is not preserved by the parser, so emit
	// a canonical equivalent.
	switch node.Direction {
	case plFetchForward:
		if node.Expr != nil {
			ctx.w("FORWARD " + node.Expr.E.Query + " FROM ")
		} else if node.HowMany == -1 {
			ctx.w("ALL FROM ")
		} else if node.HowMany != 1 {
			ctx.w(fmt.Sprintf("FORWARD %d FROM ", node.HowMany))
		}
	case plFetchBackward:
		if node.Expr != nil {
			ctx.w("BACKWARD " + node.Expr.E.Query + " FROM ")
		} else if node.HowMany == -1 {
			ctx.w("BACKWARD ALL FROM ")
		} else {
			ctx.w("BACKWARD FROM ")
		}
	case plFetchAbsolute:
		if node.Expr != nil {
			ctx.w("ABSOLUTE " + node.Expr.E.Query + " FROM ")
		} else if node.HowMany == -1 {
			ctx.w("LAST FROM ")
		} else {
			ctx.w("FIRST FROM ")
		}
	case plFetchRelative:
		if node.Expr != nil {
			ctx.w("RELATIVE " + node.Expr.E.Query + " FROM ")
		}
	}

	ctx.w(ctx.getDatumName(node.CurVar))
	if !node.IsMove && node.Target != nil {
		ctx.w(" INTO " + node.Target.fieldNames())
	}
	ctx.w(";")
}

func (ctx *plContext) writeForC(node *plStmtForC, ind int) {
	ctx.newlineIndent(ind)
	ctx.writeLabel(node.Label, ind)
	ctx.w("FOR " + node.Var.name() + " IN " + ctx.getDatumName(node.CurVar))
	if node.ArgQuery != nil {
		ctx.w("(" + node.ArgQuery.E.Query + ")")
	}
	ctx.w(" LOOP")
	ctx.writeBody(node.Body, ind+1)
	ctx.newlineIndent(ind)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeDynForS(node *plStmtDynForS, ind int) {
	ctx.newlineIndent(ind)
	ctx.writeLabel(node.Label, ind)
	ctx.w("FOR " + node.Var.name() + " IN EXECUTE " + node.Query.E.Query)
	ctx.writeUsingParams(node.Params)
	ctx.w(" LOOP")
	ctx.writeBody(node.Body, ind+1)
	ctx.newlineIndent(ind)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeIf(node *plStmtIf, ind int) {
	ctx.newlineIndent(ind)
	ctx.w("IF " + node.Cond.E.Query + " THEN")
	ctx.writeBody(node.ThenBody, ind+1)

	for _, ew := range node.ElsIfList {
		ctx.newlineIndent(ind)
		ctx.w("ELSIF " + ew.E.Cond.E.Query + " THEN")
		ctx.writeBody(ew.E.Stmts, ind+1)
	}

	if len(node.ElseBody) > 0 {
		ctx.newlineIndent(ind)
		ctx.w("ELSE")
		ctx.writeBody(node.ElseBody, ind+1)
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
		ctx.writeBody(w.Stmts, ind+2)
	}

	if node.HaveElse {
		ctx.newlineIndent(ind + 1)
		ctx.w("ELSE")
		ctx.writeBody(node.ElseStmts, ind+2)
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
	ctx.writeLabel(node.Label, ind)
	ctx.w("LOOP")
	ctx.writeBody(node.Body, ind+1)
	ctx.newlineIndent(ind)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeForI(node *plStmtForI, ind int) {
	ctx.newlineIndent(ind)
	ctx.writeLabel(node.Label, ind)
	ctx.w("FOR " + node.Var.name() + " IN ")
	if node.Reverse {
		ctx.w("REVERSE ")
	}
	ctx.w(node.Lower.E.Query + ".." + node.Upper.E.Query)
	if node.Step != nil {
		ctx.w(" BY " + node.Step.E.Query)
	}
	ctx.w(" LOOP")
	ctx.writeBody(node.Body, ind+1)
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

	if node.Message == "" && node.CondName == "" && len(node.Params) == 0 && len(node.Options) == 0 {
		ctx.w("RAISE;")
		return
	}

	levelStr := raiseLevelName[node.ElogLevel]
	if levelStr == "" {
		levelStr = "EXCEPTION"
	}

	ctx.w("RAISE " + levelStr)
	if node.CondName != "" {
		ctx.w(" " + node.CondName)
	}
	if node.Message != "" {
		ctx.w(" '" + strings.ReplaceAll(node.Message, "'", "''") + "'")
	}
	for _, p := range node.Params {
		ctx.w(", " + p.E.Query)
	}
	for i, ow := range node.Options {
		if i == 0 {
			ctx.w(" USING ")
		} else {
			ctx.w(", ")
		}
		name := raiseOptionName[ow.O.OptType]
		if name == "" {
			name = "MESSAGE"
		}
		ctx.w(name + " = " + ow.O.Expr.E.Query)
	}
	ctx.w(";")
}

func (ctx *plContext) writeExecSQL(node *plStmtExecSQL, ind int) {
	formatted := ctx.formatSQLCached(node.SQLStmt.E.Query)

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
		if !hasLineComment(formatted) {
			compact := insertInto(compactSQL(formatted), " ")
			if len(compact) <= 100-ind*4 {
				ctx.w(compact + ";")
				return
			}
		}
		formatted = insertInto(formatted, "\n")
	} else {
		if !hasLineComment(formatted) {
			compact := compactSQL(formatted)
			if len(compact) <= 100-ind*4 {
				ctx.w(compact + ";")
				return
			}
		}
	}

	ctx.writeIndented(formatted, ind)
	ctx.w(";")
}

func (ctx *plContext) writePerform(node *plStmtPerform, ind int) {
	// Parser converts PERFORM to SELECT; format as SQL then swap back
	formatted := ctx.formatSQLCached(node.Expr.E.Query)

	swapSelectToPerform := func(s string) string {
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(strings.ToUpper(s), "SELECT") {
			return "PERFORM " + s
		}
		// When the statement is just a target list (no FROM/WHERE/... at the
		// top level), keep the expression on the PERFORM line and dedent the
		// continuation lines one level.
		if rest, ok := strings.CutPrefix(s, "SELECT\n"); ok {
			lines := strings.Split(rest, "\n")
			targetsOnly := true
			for _, line := range lines {
				if !strings.HasPrefix(line, "\t") {
					targetsOnly = false
					break
				}
			}
			if targetsOnly {
				for i, line := range lines {
					lines[i] = strings.TrimPrefix(line, "\t")
				}
				return "PERFORM " + strings.Join(lines, "\n")
			}
		}
		return "PERFORM" + s[6:]
	}

	ctx.newlineIndent(ind)
	if !hasLineComment(formatted) {
		compact := swapSelectToPerform(compactSQL(formatted))
		if len(compact) <= 100-ind*4 {
			ctx.w(compact)
			ctx.w(";")
			return
		}
	}
	ctx.writeIndented(swapSelectToPerform(formatted), ind)
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
