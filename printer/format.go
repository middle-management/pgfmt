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
	// Fast path: skip the scanner-based split for single statements.
	// This avoids a redundant pgScan call when the input was already
	// split by the caller (e.g. the JS worker).
	trimmed := strings.TrimSpace(sql)
	if idx := strings.IndexByte(trimmed, ';'); idx == -1 || idx == len(trimmed)-1 {
		return formatOne(sql)
	}

	stmts, err := splitStatements(sql)
	if err != nil {
		return "", err
	}

	// Format each statement independently. This avoids passing large
	// multi-statement strings to pgParse, which can hang in WASM.
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

// FormatParsed formats SQL using pre-parsed results, avoiding any pgParse/pgScan calls.
// This is used by the WASM playground where parsing is done in JS.
func FormatParsed(parseResult *pg_query.ParseResult, scanResult *pg_query.ScanResult, originalSQL string) (string, error) {
	comments := extractComments(originalSQL, scanResult)

	var out strings.Builder
	ci := 0

	for _, stmt := range parseResult.Stmts {
		stmtEnd := stmtEndPos(stmt, int32(len(originalSQL)))
		realStart := firstRealTokenStart(scanResult, stmt.StmtLocation, stmtEnd)

		for ci < len(comments) && comments[ci].start < realStart {
			out.WriteString(comments[ci].text)
			out.WriteString("\n")
			ci++
		}

		var inlineComments []comment
		for ci < len(comments) && comments[ci].start < stmtEnd {
			inlineComments = append(inlineComments, comments[ci])
			ci++
		}

		b := &strings.Builder{}
		p := &Printer{Builder: b, comments: inlineComments, RawStmt: stmt, OriginalSQL: originalSQL}
		p.Print(stmt.Stmt)
		out.WriteString(b.String())
		out.WriteString(";\n\n")
	}

	for ci < len(comments) {
		out.WriteString(comments[ci].text)
		out.WriteString("\n")
		ci++
	}

	return out.String(), nil
}

// formatOne formats a single SQL string (which may still contain multiple
// statements, but typically contains one or few).
func formatOne(sql string) (string, error) {
	parseResult, err := pgParse(sql)
	if err != nil {
		return "", err
	}

	// Only scan for tokens if the input might contain comments.
	// This avoids a costly pgScan round-trip for the common case.
	hasComments := strings.Contains(sql, "--") || strings.Contains(sql, "/*")

	var scanResult *pg_query.ScanResult
	var comments []comment
	if hasComments {
		scanResult, err = pgScan(sql)
		if err != nil {
			return "", err
		}
		comments = extractComments(sql, scanResult)
	}

	var out strings.Builder
	ci := 0 // comment index

	for _, stmt := range parseResult.Stmts {
		stmtEnd := stmtEndPos(stmt, int32(len(sql)))

		if hasComments {
			// Find where the actual SQL keyword starts (skip leading comments/whitespace)
			realStart := firstRealTokenStart(scanResult, stmt.StmtLocation, stmtEnd)

			// Emit leading comments (those before the first real token)
			for ci < len(comments) && comments[ci].start < realStart {
				out.WriteString(comments[ci].text)
				out.WriteString("\n")
				ci++
			}
		}

		// Collect inline comments (within the statement body, after first real token)
		var inlineComments []comment
		if hasComments {
			for ci < len(comments) && comments[ci].start < stmtEnd {
				inlineComments = append(inlineComments, comments[ci])
				ci++
			}
		}

		// Format the statement, passing inline comments to the printer
		b := &strings.Builder{}
		p := &Printer{Builder: b, comments: inlineComments, RawStmt: stmt, OriginalSQL: sql}
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
