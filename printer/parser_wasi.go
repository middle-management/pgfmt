//go:build wasip1

package printer

import (
	"errors"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

var errNoParser = errors.New("parsing not available in WASI build")

func pgParse(_ string) (*pg_query.ParseResult, error) {
	return nil, errNoParser
}

func pgScan(_ string) (*pg_query.ScanResult, error) {
	return nil, errNoParser
}

func pgParsePlPgSqlToJSON(_ string) (string, error) {
	return "", errNoParser
}

func pgDeparse(_ *pg_query.ParseResult) (string, error) {
	return "", errNoParser
}

func splitStatements(_ string) ([]string, error) {
	return nil, errNoParser
}
