//go:build js && wasm

package printer

import (
	pg_query "github.com/pganalyze/pg_query_go/v6"
	wasm_pg_query "github.com/wasilibs/go-pgquery"
)

func pgParse(input string) (*pg_query.ParseResult, error) {
	return wasm_pg_query.Parse(input)
}

func pgScan(input string) (*pg_query.ScanResult, error) {
	return wasm_pg_query.Scan(input)
}

func pgParsePlPgSqlToJSON(input string) (string, error) {
	return wasm_pg_query.ParsePlPgSqlToJSON(input)
}
