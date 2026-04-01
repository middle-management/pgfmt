//go:build js && wasm

package printer

import (
	"errors"
	"syscall/js"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// In the emscripten playground architecture, parsing is done in JavaScript
// using pg-query-emscripten. The Go WASM only handles printing. These
// functions call back to JS for parsing or return errors if called directly.

func pgParse(_ string) (*pg_query.ParseResult, error) {
	return nil, errors.New("pgParse not available in WASM; use JS pgfmtPrintParseResult")
}

func pgScan(_ string) (*pg_query.ScanResult, error) {
	return nil, errors.New("pgScan not available in WASM; use JS parsing")
}

func pgParsePlPgSqlToJSON(input string) (string, error) {
	fn := js.Global().Get("pgfmtParsePlPgSQL")
	if fn.IsUndefined() {
		return "", errors.New("pgfmtParsePlPgSQL not defined in JS")
	}
	result := fn.Invoke(input)
	errStr := result.Get("error").String()
	if errStr != "" && errStr != "<undefined>" {
		return "", errors.New(errStr)
	}
	return result.Get("result").String(), nil
}

func pgDeparse(_ *pg_query.ParseResult) (string, error) {
	return "", errors.New("pgDeparse not available in WASM")
}

func splitStatements(_ string) ([]string, error) {
	return nil, errors.New("splitStatements not available in WASM; use JS splitting")
}
