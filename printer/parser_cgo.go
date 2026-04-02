//go:build !js && !wasip1

package printer

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

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

func splitStatements(sql string) ([]string, error) {
	stmts, err := pg_query.SplitWithScanner(sql, false)
	if err != nil {
		return nil, err
	}

	// SplitWithScanner only returns semicolon-delimited statements.
	// Append any trailing text (e.g. comments after the last statement).
	consumed := 0
	for _, s := range stmts {
		idx := strings.Index(sql[consumed:], s)
		if idx >= 0 {
			consumed += idx + len(s)
		}
	}
	if trailing := strings.TrimSpace(sql[consumed:]); trailing != "" {
		stmts = append(stmts, sql[consumed:])
	}

	return stmts, nil
}
