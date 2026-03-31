//go:build !js

package printer

import pg_query "github.com/pganalyze/pg_query_go/v6"

func pgParse(input string) (*pg_query.ParseResult, error) {
	return pg_query.Parse(input)
}

func pgScan(input string) (*pg_query.ScanResult, error) {
	return pg_query.Scan(input)
}

func pgParsePlPgSqlToJSON(input string) (string, error) {
	return pg_query.ParsePlPgSqlToJSON(input)
}

func pgDeparse(result *pg_query.ParseResult) (string, error) {
	return pg_query.Deparse(result)
}
