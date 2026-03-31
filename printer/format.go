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
	stmts, err := splitStatements(sql)
	if err != nil {
		return "", err
	}

	// Format each statement independently. This avoids passing large
	// multi-statement strings to pgParse, which can hang in WASM.
	if len(stmts) > 1 {
		var out strings.Builder
		for _, s := range stmts {
			result, err := formatOne(s)
			if err != nil {
				return "", err
			}
			out.WriteString(result)
		}
		return out.String(), nil
	}

	return formatOne(sql)
}

// formatOne formats a single SQL string (which may still contain multiple
// statements, but typically contains one or few).
func formatOne(sql string) (string, error) {
	parseResult, err := pgParse(sql)
	if err != nil {
		return "", err
	}

	scanResult, err := pgScan(sql)
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

		// Collect inline comments (within the statement body, after first real token)
		var inlineComments []comment
		for ci < len(comments) && comments[ci].start < stmtEnd {
			inlineComments = append(inlineComments, comments[ci])
			ci++
		}

		// Format the statement, passing inline comments to the printer
		b := &strings.Builder{}
		p := &Printer{Builder: b, comments: inlineComments, RawStmt: stmt}
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

// splitStatements splits SQL input into individual statements using the
// scanner to find semicolon boundaries. Each returned string includes
// any leading comments/whitespace and the trailing semicolon.
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

	// Handle trailing text after last semicolon (comments or a statement without ';')
	if start < len(sql) {
		trailing := sql[start:]
		if strings.TrimSpace(trailing) != "" {
			stmts = append(stmts, trailing)
		}
	}

	return stmts, nil
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
