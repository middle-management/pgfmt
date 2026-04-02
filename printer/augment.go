//go:build !js && !wasip1

package printer

import (
	"encoding/json"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/encoding/protojson"
)

var protoMarshalOpts = protojson.MarshalOptions{EmitUnpopulated: false}

func Augment(sql string) ([]byte, error) {
	parseResult, err := pgParse(sql)
	if err != nil {
		return nil, err
	}

	scanResult, err := pgScan(sql)
	if err != nil {
		return nil, err
	}

	comments := ExtractComments(sql, scanResult)
	ast := AugmentedAST{Version: int(parseResult.Version)}

	ci := 0
	for _, rawStmt := range parseResult.Stmts {
		stmtEnd := stmtEndPos(rawStmt, int32(len(sql)))
		realStart := FirstRealTokenStart(scanResult, rawStmt.StmtLocation, stmtEnd)

		// Emit leading comments before this statement.
		for ci < len(comments) && comments[ci].start < realStart {
			ast.Stmts = append(ast.Stmts, AugmentedStmt{
				Comment: &AugmentedComment{
					Text: comments[ci].text,
					Type: commentType(comments[ci].text),
				},
			})
			ci++
		}

		// Collect inline comments for this statement.
		var inlineComments []comment
		for ci < len(comments) && comments[ci].start < stmtEnd {
			inlineComments = append(inlineComments, comments[ci])
			ci++
		}

		// Marshal the statement node to JSON.
		stmtJSON, err := protoMarshalOpts.Marshal(rawStmt.Stmt)
		if err != nil {
			return nil, err
		}

		// Embed inline comments in the JSON.
		if len(inlineComments) > 0 {
			stmtJSON = embedInlineComments(stmtJSON, inlineComments)
		}

		// Pre-parse function bodies.
		bodiesMap := preParseBodies(rawStmt)
		if bodiesMap != nil {
			stmtJSON = embedBodies(stmtJSON, bodiesMap)
		}

		// Pre-compute deparsed text so the WASI printer can use it as fallback
		// for unsupported node types (where pgDeparse is not available).
		deparsed, _ := pgDeparse(&pg_query.ParseResult{
			Stmts: []*pg_query.RawStmt{rawStmt},
		})

		ast.Stmts = append(ast.Stmts, AugmentedStmt{
			Stmt:         json.RawMessage(stmtJSON),
			StmtLocation: rawStmt.StmtLocation,
			StmtLen:      rawStmt.StmtLen,
			Deparsed:     deparsed,
		})
	}

	// Trailing comments.
	for ci < len(comments) {
		ast.Stmts = append(ast.Stmts, AugmentedStmt{
			Comment: &AugmentedComment{
				Text: comments[ci].text,
				Type: commentType(comments[ci].text),
			},
		})
		ci++
	}

	return json.Marshal(ast)
}

// embedInlineComments adds a _comments array to the top-level JSON object.
func embedInlineComments(stmtJSON []byte, comments []comment) []byte {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(stmtJSON, &node); err != nil {
		return stmtJSON
	}

	type jsonComment struct {
		Text  string `json:"text"`
		Start int32  `json:"start"`
		End   int32  `json:"end"`
	}
	var jc []jsonComment
	for _, c := range comments {
		jc = append(jc, jsonComment{Text: c.text, Start: c.start, End: c.end})
	}
	commentsJSON, err := json.Marshal(jc)
	if err != nil {
		return stmtJSON
	}
	node["_comments"] = json.RawMessage(commentsJSON)

	result, err := json.Marshal(node)
	if err != nil {
		return stmtJSON
	}
	return result
}

func commentType(text string) string {
	if strings.HasPrefix(text, "/*") {
		return "block"
	}
	return "line"
}

// preParseBodies extracts and pre-parses function bodies from CREATE FUNCTION or DO statements.
func preParseBodies(rawStmt *pg_query.RawStmt) map[string]map[string]string {
	bodies := map[string]map[string]string{
		"sql":     {},
		"plpgsql": {},
	}

	var lang, body string
	if cfs := rawStmt.Stmt.GetCreateFunctionStmt(); cfs != nil {
		for _, opt := range cfs.Options {
			de := opt.GetDefElem()
			if de == nil {
				continue
			}
			switch de.Defname {
			case "language":
				lang = strings.ToLower(de.Arg.GetString_().GetSval())
			case "as":
				if l := de.Arg.GetList(); l != nil && len(l.Items) > 0 {
					body = l.Items[0].GetString_().GetSval()
				}
			}
		}
	} else if ds := rawStmt.Stmt.GetDoStmt(); ds != nil {
		lang = "plpgsql" // default language for DO statements
		for _, arg := range ds.Args {
			de := arg.GetDefElem()
			if de == nil {
				continue
			}
			switch de.Defname {
			case "language":
				lang = strings.ToLower(de.Arg.GetString_().GetSval())
			case "as":
				if s := de.Arg.GetString_(); s != nil {
					body = s.GetSval()
				} else if l := de.Arg.GetList(); l != nil && len(l.Items) > 0 {
					body = l.Items[0].GetString_().GetSval()
				}
			}
		}
	}

	if body == "" || lang == "" {
		return nil
	}

	switch lang {
	case "sql":
		result, err := pgParse(body)
		if err == nil {
			j, err := protoMarshalOpts.Marshal(result)
			if err == nil {
				bodies["sql"][body] = string(j)
			}
		}
	case "plpgsql":
		wrappers := []string{
			"CREATE FUNCTION _plpgsql_fmt_() RETURNS void AS $$",
			"CREATE FUNCTION _plpgsql_fmt_() RETURNS SETOF record AS $$",
		}
		for _, prefix := range wrappers {
			wrapped := prefix + body + "\n$$ LANGUAGE plpgsql;"
			jsonResult, err := pgParsePlPgSqlToJSON(wrapped)
			if err == nil {
				bodies["plpgsql"][wrapped] = jsonResult
				// Pre-parse embedded SQL queries within PL/pgSQL bodies.
				preParseEmbeddedSQL(jsonResult, bodies["sql"])
				break
			}
		}
	default:
		return nil
	}

	if len(bodies["sql"]) == 0 && len(bodies["plpgsql"]) == 0 {
		return nil
	}
	return bodies
}

// preParseEmbeddedSQL extracts SQL query strings from PL/pgSQL JSON and
// pre-parses them into the SQL body cache. This enables the WASI build
// (which cannot call pgParse) to format embedded SQL within PL/pgSQL bodies.
func preParseEmbeddedSQL(plJSON string, sqlCache map[string]string) {
	queries := extractPLpgSQLQueries(plJSON)
	for _, q := range queries {
		if _, ok := sqlCache[q]; ok {
			continue
		}
		result, err := pgParse(q)
		if err != nil || len(result.Stmts) == 0 {
			continue
		}
		j, err := protoMarshalOpts.Marshal(result)
		if err != nil {
			continue
		}
		sqlCache[q] = string(j)
	}
}

// extractPLpgSQLQueries walks PL/pgSQL JSON and extracts all PLpgSQL_expr query strings.
func extractPLpgSQLQueries(jsonStr string) []string {
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil
	}
	var queries []string
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch val := v.(type) {
		case map[string]interface{}:
			// Check if this is a PLpgSQL_expr with a query field.
			if _, ok := val["PLpgSQL_expr"]; ok {
				if expr, ok := val["PLpgSQL_expr"].(map[string]interface{}); ok {
					if q, ok := expr["query"].(string); ok {
						queries = append(queries, q)
					}
				}
				return
			}
			for _, child := range val {
				walk(child)
			}
		case []interface{}:
			for _, item := range val {
				walk(item)
			}
		}
	}
	walk(data)
	return queries
}

// embedBodies adds a _bodies object to the top-level JSON object.
func embedBodies(stmtJSON []byte, bodies map[string]map[string]string) []byte {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(stmtJSON, &node); err != nil {
		return stmtJSON
	}
	bodiesJSON, err := json.Marshal(bodies)
	if err != nil {
		return stmtJSON
	}
	node["_bodies"] = json.RawMessage(bodiesJSON)
	result, err := json.Marshal(node)
	if err != nil {
		return stmtJSON
	}
	return result
}
