package printer

import (
	"encoding/json"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v5"
)

// plContext holds state during PL/pgSQL body formatting.
type plContext struct {
	printer *Printer
	datums  []interface{}
}

func (ctx *plContext) w(s string) {
	ctx.printer.Builder.WriteString(s)
}

func (ctx *plContext) plIndent(level int) {
	for i := 0; i < level; i++ {
		ctx.w("\t")
	}
}

// JSON navigation helpers

func jsonObj(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func jsonArr(v interface{}) []interface{} {
	a, _ := v.([]interface{})
	return a
}

func jsonStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		s, _ := v.(string)
		return s
	}
	return ""
}

func jsonFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		f, _ := v.(float64)
		return f
	}
	return 0
}

func jsonBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		b, _ := v.(bool)
		return b
	}
	return false
}

func jsonGetObj(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		return jsonObj(v)
	}
	return nil
}

func jsonGetArr(m map[string]interface{}, key string) []interface{} {
	if v, ok := m[key]; ok {
		return jsonArr(v)
	}
	return nil
}

// unwrapNode takes {"PLpgSQL_stmt_if": {...}} and returns ("PLpgSQL_stmt_if", {...}).
func unwrapNode(v interface{}) (string, map[string]interface{}) {
	m := jsonObj(v)
	if m == nil {
		return "", nil
	}
	for k, val := range m {
		if inner := jsonObj(val); inner != nil {
			return k, inner
		}
	}
	return "", nil
}

// getExprQuery extracts the query string from a PLpgSQL_expr node.
func getExprQuery(v interface{}) string {
	m := jsonObj(v)
	if m == nil {
		return ""
	}
	if expr := jsonGetObj(m, "PLpgSQL_expr"); expr != nil {
		return jsonStr(expr, "query")
	}
	return jsonStr(m, "query")
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

	var parsed []interface{}
	if err := json.Unmarshal([]byte(jsonResult), &parsed); err != nil || len(parsed) == 0 {
		output.Builder.WriteString("\n")
		output.Builder.WriteString(body)
		return
	}

	_, fn := unwrapNode(parsed[0])
	if fn == nil {
		output.Builder.WriteString("\n")
		output.Builder.WriteString(body)
		return
	}

	ctx := &plContext{
		printer: output,
		datums:  jsonGetArr(fn, "datums"),
	}

	ctx.writeDeclare(indentLevel)

	actionWrapper := jsonGetObj(fn, "action")
	block := jsonGetObj(actionWrapper, "PLpgSQL_stmt_block")
	if block != nil {
		ctx.writeBlock(block, indentLevel, true)
	}
}

// writeDeclare emits the DECLARE section for user-declared variables.
func (ctx *plContext) writeDeclare(indent int) {
	var decls []string
	for _, d := range ctx.datums {
		kind, inner := unwrapNode(d)
		if kind != "PLpgSQL_var" {
			continue
		}
		name := jsonStr(inner, "refname")
		if name == "" || strings.HasPrefix(name, "__") {
			continue
		}
		if jsonFloat(inner, "lineno") == 0 {
			continue // implicit (found, params)
		}
		dt := jsonGetObj(inner, "datatype")
		if dt == nil {
			continue
		}
		pt := jsonGetObj(dt, "PLpgSQL_type")
		if pt == nil {
			continue
		}
		typ := strings.TrimSpace(jsonStr(pt, "typname"))
		if typ == "" || typ == "UNKNOWN" {
			continue
		}

		var parts []string
		parts = append(parts, name)
		if jsonBool(inner, "isconst") {
			parts = append(parts, "CONSTANT")
		}
		parts = append(parts, typ)
		if jsonBool(inner, "notnull") {
			parts = append(parts, "NOT NULL")
		}
		decl := strings.Join(parts, " ")

		if defVal := jsonGetObj(inner, "default_val"); defVal != nil {
			if defExpr := jsonGetObj(defVal, "PLpgSQL_expr"); defExpr != nil {
				decl += " := " + jsonStr(defExpr, "query")
			}
		}

		decls = append(decls, decl)
	}

	if len(decls) > 0 {
		ctx.w("\n")
		ctx.plIndent(indent)
		ctx.w("DECLARE")
		for _, d := range decls {
			ctx.w("\n")
			ctx.plIndent(indent + 1)
			ctx.w(d + ";")
		}
	}
}

// writeBlock emits a BEGIN/[EXCEPTION/]END block.
func (ctx *plContext) writeBlock(block map[string]interface{}, indent int, topLevel bool) {
	body := jsonGetArr(block, "body")
	exceptions := jsonGetObj(block, "exceptions")

	// Detect parser-generated wrapper: [inner_block_with_exceptions, bare_return]
	// and flatten to a single block.
	if exceptions == nil && len(body) >= 1 {
		firstKind, firstNode := unwrapNode(body[0])
		if firstKind == "PLpgSQL_stmt_block" && jsonGetObj(firstNode, "exceptions") != nil {
			allTrailingBare := true
			for _, s := range body[1:] {
				sk, sn := unwrapNode(s)
				if sk == "PLpgSQL_stmt_return" && jsonGetObj(sn, "expr") == nil && jsonFloat(sn, "lineno") == 0 {
					continue
				}
				allTrailingBare = false
				break
			}
			if allTrailingBare {
				block = firstNode
				body = jsonGetArr(block, "body")
				exceptions = jsonGetObj(block, "exceptions")
			}
		}
	}

	ctx.w("\n")
	ctx.plIndent(indent)

	label := jsonStr(block, "label")
	if label != "" {
		ctx.w("<<" + label + ">>\n")
		ctx.plIndent(indent)
	}

	ctx.w("BEGIN")
	ctx.writeStmts(body, indent+1)

	if exceptions != nil {
		excBlock := jsonGetObj(exceptions, "PLpgSQL_exception_block")
		if excBlock != nil {
			ctx.w("\n")
			ctx.plIndent(indent)
			ctx.w("EXCEPTION")
			for _, e := range jsonGetArr(excBlock, "exc_list") {
				_, excNode := unwrapNode(e)
				if excNode == nil {
					continue
				}
				var condNames []string
				for _, c := range jsonGetArr(excNode, "conditions") {
					cond := jsonGetObj(jsonObj(c), "PLpgSQL_condition")
					if cond != nil {
						condNames = append(condNames, jsonStr(cond, "condname"))
					}
				}
				ctx.w("\n")
				ctx.plIndent(indent + 1)
				ctx.w("WHEN " + strings.Join(condNames, " OR ") + " THEN")
				ctx.writeStmts(jsonGetArr(excNode, "action"), indent+2)
			}
		}
	}

	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("END")
	if !topLevel {
		ctx.w(";")
	}
}

// writeStmts emits a list of PL/pgSQL statements.
func (ctx *plContext) writeStmts(stmts []interface{}, indent int) {
	for _, s := range stmts {
		kind, node := unwrapNode(s)
		if node == nil {
			continue
		}
		ctx.writeStmt(kind, node, indent)
	}
}

func (ctx *plContext) writeStmt(kind string, node map[string]interface{}, indent int) {
	switch kind {
	case "PLpgSQL_stmt_assign":
		ctx.writeAssign(node, indent)
	case "PLpgSQL_stmt_if":
		ctx.writeIf(node, indent)
	case "PLpgSQL_stmt_case":
		ctx.writeCase(node, indent)
	case "PLpgSQL_stmt_loop":
		ctx.writeLoop(node, indent)
	case "PLpgSQL_stmt_while":
		ctx.writeWhile(node, indent)
	case "PLpgSQL_stmt_fori":
		ctx.writeForI(node, indent)
	case "PLpgSQL_stmt_fors":
		ctx.writeForS(node, indent)
	case "PLpgSQL_stmt_foreach_a":
		ctx.writeForEach(node, indent)
	case "PLpgSQL_stmt_exit":
		ctx.writeExit(node, indent)
	case "PLpgSQL_stmt_return":
		ctx.writeReturn(node, indent)
	case "PLpgSQL_stmt_return_next":
		ctx.writeReturnNext(node, indent)
	case "PLpgSQL_stmt_return_query":
		ctx.writeReturnQuery(node, indent)
	case "PLpgSQL_stmt_raise":
		ctx.writeRaise(node, indent)
	case "PLpgSQL_stmt_execsql":
		ctx.writeExecSQL(node, indent)
	case "PLpgSQL_stmt_perform":
		ctx.writePerform(node, indent)
	case "PLpgSQL_stmt_dynexecute":
		ctx.writeDynExecute(node, indent)
	case "PLpgSQL_stmt_block":
		ctx.writeBlock(node, indent, false)
	}
}

func (ctx *plContext) writeAssign(node map[string]interface{}, indent int) {
	expr := getExprQuery(jsonGetObj(node, "expr"))
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w(expr + ";")
}

func (ctx *plContext) writeReturn(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)
	if exprNode := jsonGetObj(node, "expr"); exprNode != nil {
		ctx.w("RETURN " + getExprQuery(exprNode) + ";")
	} else {
		ctx.w("RETURN;")
	}
}

func (ctx *plContext) writeReturnNext(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("RETURN NEXT " + getExprQuery(jsonGetObj(node, "expr")) + ";")
}

func (ctx *plContext) writeReturnQuery(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)
	query := getExprQuery(jsonGetObj(node, "query"))
	ctx.w("RETURN QUERY " + query + ";")
}

func (ctx *plContext) writeIf(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("IF " + getExprQuery(jsonGetObj(node, "cond")) + " THEN")

	ctx.writeStmts(jsonGetArr(node, "then_body"), indent+1)

	for _, e := range jsonGetArr(node, "elsif_list") {
		elsif := jsonGetObj(jsonObj(e), "PLpgSQL_if_elsif")
		if elsif == nil {
			continue
		}
		ctx.w("\n")
		ctx.plIndent(indent)
		ctx.w("ELSIF " + getExprQuery(jsonGetObj(elsif, "cond")) + " THEN")
		ctx.writeStmts(jsonGetArr(elsif, "stmts"), indent+1)
	}

	if elseBody := jsonGetArr(node, "else_body"); len(elseBody) > 0 {
		ctx.w("\n")
		ctx.plIndent(indent)
		ctx.w("ELSE")
		ctx.writeStmts(elseBody, indent+1)
	}

	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("END IF;")
}

func (ctx *plContext) writeCase(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)

	hasTestExpr := jsonGetObj(node, "t_expr") != nil
	if hasTestExpr {
		ctx.w("CASE " + getExprQuery(jsonGetObj(node, "t_expr")))
	} else {
		ctx.w("CASE")
	}

	for _, w := range jsonGetArr(node, "case_when_list") {
		when := jsonGetObj(jsonObj(w), "PLpgSQL_case_when")
		if when == nil {
			continue
		}
		ctx.w("\n")
		ctx.plIndent(indent + 1)

		expr := getExprQuery(jsonGetObj(when, "expr"))
		if hasTestExpr {
			expr = extractCaseWhenValue(expr)
		}
		ctx.w("WHEN " + expr + " THEN")
		ctx.writeStmts(jsonGetArr(when, "stmts"), indent+2)
	}

	if jsonBool(node, "have_else") {
		if elseStmts := jsonGetArr(node, "else_stmts"); len(elseStmts) > 0 {
			ctx.w("\n")
			ctx.plIndent(indent + 1)
			ctx.w("ELSE")
			ctx.writeStmts(elseStmts, indent+2)
		}
	}

	ctx.w("\n")
	ctx.plIndent(indent)
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

func (ctx *plContext) writeLoop(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)
	if label := jsonStr(node, "label"); label != "" {
		ctx.w("<<" + label + ">>\n")
		ctx.plIndent(indent)
	}
	ctx.w("LOOP")
	ctx.writeStmts(jsonGetArr(node, "body"), indent+1)
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeWhile(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("WHILE " + getExprQuery(jsonGetObj(node, "cond")) + " LOOP")
	ctx.writeStmts(jsonGetArr(node, "body"), indent+1)
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeForI(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)

	varNode := jsonGetObj(node, "var")
	varInner := jsonGetObj(varNode, "PLpgSQL_var")
	varName := jsonStr(varInner, "refname")

	lower := getExprQuery(jsonGetObj(node, "lower"))
	upper := getExprQuery(jsonGetObj(node, "upper"))

	ctx.w("FOR " + varName + " IN ")
	if jsonBool(node, "reverse") {
		ctx.w("REVERSE ")
	}
	ctx.w(lower + ".." + upper)
	if step := jsonGetObj(node, "step"); step != nil {
		ctx.w(" BY " + getExprQuery(step))
	}
	ctx.w(" LOOP")

	ctx.writeStmts(jsonGetArr(node, "body"), indent+1)
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeForS(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)

	varName := ctx.getRowVarName(jsonGetObj(node, "var"))
	query := getExprQuery(jsonGetObj(node, "query"))

	ctx.w("FOR " + varName + " IN " + query + " LOOP")
	ctx.writeStmts(jsonGetArr(node, "body"), indent+1)
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeForEach(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)

	varName := ctx.getDatumName(int(jsonFloat(node, "varno")))
	expr := getExprQuery(jsonGetObj(node, "expr"))

	ctx.w("FOREACH " + varName + " IN ARRAY " + expr + " LOOP")
	ctx.writeStmts(jsonGetArr(node, "body"), indent+1)
	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("END LOOP;")
}

func (ctx *plContext) writeExit(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)

	if jsonBool(node, "is_exit") {
		ctx.w("EXIT")
	} else {
		ctx.w("CONTINUE")
	}
	if label := jsonStr(node, "label"); label != "" {
		ctx.w(" " + label)
	}
	if cond := jsonGetObj(node, "cond"); cond != nil {
		ctx.w(" WHEN " + getExprQuery(cond))
	}
	ctx.w(";")
}

func (ctx *plContext) writeRaise(node map[string]interface{}, indent int) {
	ctx.w("\n")
	ctx.plIndent(indent)

	message := jsonStr(node, "message")
	params := jsonGetArr(node, "params")

	if message == "" && len(params) == 0 {
		ctx.w("RAISE;")
		return
	}

	level := int(jsonFloat(node, "elog_level"))
	levelStr := raiseLevelName[level]
	if levelStr == "" {
		levelStr = "EXCEPTION"
	}

	ctx.w("RAISE " + levelStr)
	if message != "" {
		ctx.w(" '" + message + "'")
	}
	for _, p := range params {
		ctx.w(", " + getExprQuery(p))
	}
	ctx.w(";")
}

func (ctx *plContext) writeExecSQL(node map[string]interface{}, indent int) {
	query := getExprQuery(jsonGetObj(node, "sqlstmt"))

	if jsonBool(node, "into") {
		target := jsonGetObj(node, "target")
		targetName := ctx.getTargetName(target)

		intoClause := "INTO "
		if jsonBool(node, "strict") {
			intoClause = "INTO STRICT "
		}
		intoClause += targetName

		// Normalize whitespace left by parser removing INTO
		query = strings.Join(strings.Fields(query), " ")

		// Insert INTO before FROM
		upper := strings.ToUpper(query)
		if fromIdx := strings.Index(upper, " FROM "); fromIdx >= 0 {
			query = query[:fromIdx] + " " + intoClause + query[fromIdx:]
		} else {
			query += " " + intoClause
		}
	}

	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w(query + ";")
}

func (ctx *plContext) writePerform(node map[string]interface{}, indent int) {
	query := getExprQuery(jsonGetObj(node, "expr"))
	// Parser converts PERFORM to SELECT; convert back
	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "SELECT ") {
		query = "PERFORM " + trimmed[7:]
	} else {
		query = "PERFORM " + trimmed
	}

	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w(query + ";")
}

func (ctx *plContext) writeDynExecute(node map[string]interface{}, indent int) {
	query := getExprQuery(jsonGetObj(node, "query"))

	ctx.w("\n")
	ctx.plIndent(indent)
	ctx.w("EXECUTE " + query)

	if jsonBool(node, "into") {
		target := jsonGetObj(node, "target")
		targetName := ctx.getTargetName(target)
		ctx.w(" INTO ")
		if jsonBool(node, "strict") {
			ctx.w("STRICT ")
		}
		ctx.w(targetName)
	}

	if params := jsonGetArr(node, "params"); len(params) > 0 {
		ctx.w(" USING ")
		for i, p := range params {
			if i > 0 {
				ctx.w(", ")
			}
			ctx.w(getExprQuery(p))
		}
	}

	ctx.w(";")
}

// Helper: look up a datum name by index.
func (ctx *plContext) getDatumName(varno int) string {
	if varno < 0 || varno >= len(ctx.datums) {
		return "???"
	}
	kind, inner := unwrapNode(ctx.datums[varno])
	switch kind {
	case "PLpgSQL_var":
		return jsonStr(inner, "refname")
	case "PLpgSQL_row":
		return ctx.rowFieldNames(inner)
	case "PLpgSQL_rec":
		return jsonStr(inner, "refname")
	}
	return "???"
}

func (ctx *plContext) rowFieldNames(row map[string]interface{}) string {
	var names []string
	for _, f := range jsonGetArr(row, "fields") {
		fo := jsonObj(f)
		if fo != nil {
			names = append(names, jsonStr(fo, "name"))
		}
	}
	return strings.Join(names, ", ")
}

// getRowVarName extracts a variable name from a PLpgSQL_row or PLpgSQL_var wrapper.
func (ctx *plContext) getRowVarName(node map[string]interface{}) string {
	if node == nil {
		return "rec"
	}
	if row := jsonGetObj(node, "PLpgSQL_row"); row != nil {
		fields := jsonGetArr(row, "fields")
		if len(fields) > 0 {
			fo := jsonObj(fields[0])
			if fo != nil {
				return jsonStr(fo, "name")
			}
		}
	}
	if v := jsonGetObj(node, "PLpgSQL_var"); v != nil {
		return jsonStr(v, "refname")
	}
	return "rec"
}

// getTargetName returns the target variable name for INTO clauses.
func (ctx *plContext) getTargetName(node map[string]interface{}) string {
	if node == nil {
		return "???"
	}
	if row := jsonGetObj(node, "PLpgSQL_row"); row != nil {
		return ctx.rowFieldNames(row)
	}
	if v := jsonGetObj(node, "PLpgSQL_var"); v != nil {
		return jsonStr(v, "refname")
	}
	return "???"
}
