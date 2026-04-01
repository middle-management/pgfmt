//go:build js && wasm

package printer

import (
	"errors"
	"strings"
	"syscall/js"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/encoding/protojson"
)

// In the emscripten playground architecture, parsing is done in JavaScript
// using pg-query-emscripten. These functions call back to JS and unmarshal
// the JSON results into protobuf types.

var jsonOpts = protojson.UnmarshalOptions{DiscardUnknown: true}

func pgParse(input string) (*pg_query.ParseResult, error) {
	fn := js.Global().Get("pgfmtParse")
	if fn.IsUndefined() {
		return nil, errors.New("pgfmtParse not defined in JS")
	}
	res := fn.Invoke(input)
	if errStr := res.Get("error").String(); errStr != "" && errStr != "<undefined>" {
		return nil, errors.New(errStr)
	}
	jsonStr := res.Get("result").String()
	result := &pg_query.ParseResult{}
	if err := jsonOpts.Unmarshal([]byte(jsonStr), result); err != nil {
		return nil, err
	}
	return result, nil
}

func pgScan(input string) (*pg_query.ScanResult, error) {
	fn := js.Global().Get("pgfmtScan")
	if fn.IsUndefined() {
		return nil, errors.New("pgfmtScan not defined in JS")
	}
	res := fn.Invoke(input)
	if errStr := res.Get("error").String(); errStr != "" && errStr != "<undefined>" {
		return nil, errors.New(errStr)
	}
	// Build ScanResult from JS scan tokens.
	tokens := res.Get("tokens")
	length := tokens.Length()
	scanResult := &pg_query.ScanResult{}
	for i := 0; i < length; i++ {
		tok := tokens.Index(i)
		scanResult.Tokens = append(scanResult.Tokens, &pg_query.ScanToken{
			Start:   int32(tok.Get("start").Int()),
			End:     int32(tok.Get("end").Int()),
			Token:       pg_query.Token(tokenKindToEnum(tok.Get("token_kind").String())),
			KeywordKind: pg_query.KeywordKind(keywordKindToEnum(tok.Get("keyword_kind").String())),
		})
	}
	return scanResult, nil
}

func pgParsePlPgSqlToJSON(input string) (string, error) {
	fn := js.Global().Get("pgfmtParsePlPgSQL")
	if fn.IsUndefined() {
		return "", errors.New("pgfmtParsePlPgSQL not defined in JS")
	}
	result := fn.Invoke(input)
	if errStr := result.Get("error").String(); errStr != "" && errStr != "<undefined>" {
		return "", errors.New(errStr)
	}
	return result.Get("result").String(), nil
}

func pgDeparse(_ *pg_query.ParseResult) (string, error) {
	return "", errors.New("pgDeparse not available in WASM")
}

func splitStatements(sql string) ([]string, error) {
	scanResult, err := pgScan(sql)
	if err != nil {
		return nil, err
	}

	var stmts []string
	start := 0
	for _, tok := range scanResult.Tokens {
		if tok.Token == pg_query.Token_ASCII_59 { // ';'
			stmt := sql[start:tok.End]
			if strings.TrimSpace(stmt) != ";" && strings.TrimSpace(stmt) != "" {
				stmts = append(stmts, stmt)
			}
			start = int(tok.End)
		}
	}
	if start < len(sql) {
		trailing := sql[start:]
		if strings.TrimSpace(trailing) != "" {
			stmts = append(stmts, trailing)
		}
	}
	return stmts, nil
}

// tokenKindToEnum maps pg-query-emscripten token_kind strings to pg_query.Token enum values.
func tokenKindToEnum(kind string) int32 {
	if v, ok := pg_query.Token_value[kind]; ok {
		return v
	}
	return 0
}

func keywordKindToEnum(kind string) int32 {
	if v, ok := pg_query.KeywordKind_value[kind]; ok {
		return v
	}
	return 0
}
