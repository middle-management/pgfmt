package printer

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// comment represents a SQL comment extracted from source text.
type comment struct {
	text  string
	start int32
	end   int32
}

// Format parses and formats a SQL string, preserving inter-statement comments.
func Format(sql string) (string, error) {
	parseResult, err := pg_query.Parse(sql)
	if err != nil {
		return "", err
	}

	scanResult, err := pg_query.Scan(sql)
	if err != nil {
		return "", err
	}

	comments := extractComments(sql, scanResult)

	var out strings.Builder
	ci := 0 // comment index

	for _, stmt := range parseResult.Stmts {
		stmtEnd := stmtEndPos(stmt, int32(len(sql)))

		// Find where the actual SQL keyword starts (skip leading comments/whitespace)
		realStart := firstRealTokenStart(scanResult, stmt.StmtLocation, stmtEnd)

		// Emit leading comments (those before the first real token)
		for ci < len(comments) && comments[ci].start < realStart {
			out.WriteString(comments[ci].text)
			out.WriteString("\n")
			ci++
		}

		// Skip any inline comments within the statement body
		for ci < len(comments) && comments[ci].start < stmtEnd {
			ci++
		}

		// Format the statement
		b := &strings.Builder{}
		p := &Printer{Builder: b}
		p.Print(stmt.Stmt)
		out.WriteString(b.String())
		out.WriteString(";\n\n")
	}

	// Emit trailing comments (after last statement)
	for ci < len(comments) {
		out.WriteString(comments[ci].text)
		out.WriteString("\n")
		ci++
	}

	return out.String(), nil
}

// stmtEndPos returns the end byte position for a statement.
// StmtLen is 0 for the last statement, meaning "to end of input".
func stmtEndPos(stmt *pg_query.RawStmt, inputLen int32) int32 {
	if stmt.StmtLen > 0 {
		return stmt.StmtLocation + stmt.StmtLen
	}
	return inputLen
}

// firstRealTokenStart finds the byte position of the first non-comment token
// within the given range of the scan result. Returns rangeEnd if none found.
func firstRealTokenStart(scanResult *pg_query.ScanResult, rangeStart, rangeEnd int32) int32 {
	for _, tok := range scanResult.Tokens {
		if tok.Start < rangeStart {
			continue
		}
		if tok.Start >= rangeEnd {
			break
		}
		if tok.Token != pg_query.Token_SQL_COMMENT && tok.Token != pg_query.Token_C_COMMENT {
			return tok.Start
		}
	}
	return rangeEnd
}

func extractComments(sql string, scanResult *pg_query.ScanResult) []comment {
	var comments []comment
	for _, token := range scanResult.Tokens {
		if token.Token == pg_query.Token_SQL_COMMENT || token.Token == pg_query.Token_C_COMMENT {
			comments = append(comments, comment{
				text:  sql[token.Start:token.End],
				start: token.Start,
				end:   token.End,
			})
		}
	}
	return comments
}
