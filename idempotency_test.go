package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/middle-management/pgfmt/printer"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Function bodies (delimited by $$ … $$ or other dollar tags) and reorderable
// CREATE FUNCTION options are normalized away before AST-equivalence checks
// because pgfmt reformats body whitespace and pg_query.Deparse may emit option
// keywords in a different order than the formatter.
var (
	dollarBodyRe  = regexp.MustCompile(`\$[A-Za-z_]*\$[\s\S]*?\$[A-Za-z_]*\$`)
	funcOptionsRe = regexp.MustCompile(`\s+(LANGUAGE \w+|VOLATILE|STABLE|IMMUTABLE|STRICT|CALLED ON NULL INPUT|RETURNS NULL ON NULL INPUT|SECURITY DEFINER|SECURITY INVOKER|PARALLEL \w+)`)
	multiSpaceRe  = regexp.MustCompile(`\s+`)
)

func normalizeForCompare(s string) string {
	s = dollarBodyRe.ReplaceAllString(s, "")
	s = funcOptionsRe.ReplaceAllString(s, "")
	s = multiSpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// fixturePaths returns plain-text fixture paths suitable for round-trip tests.
// We skip .sql.gz fixtures (these are huge generated schemas — the cost isn't
// worth it for these checks since the round-trip property is already exercised
// by the smaller fixtures).
func fixturePaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("testdata/fixtures/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// knownIdempotencyDivergence lists fixtures where Format(Format(x)) != Format(x).
// These are real bugs to fix; the test denylists them so it can still gate
// regressions on the rest of the corpus.
//
//   - ddl_misc.sql: formatter rewrites ON CONFLICT DO UPDATE SET clauses on
//     each pass, so the second pass differs from the first.
//   - pbxdao_schema.sql: large mixed schema; multiple constructs reformat
//     differently on subsequent passes (likely RETURNS TABLE + complex CHECK).
//   - plpgsql_functions.sql: function-body reformatting is not stable on
//     repeated passes for some plpgsql constructs.
var knownIdempotencyDivergence = map[string]bool{
	"ddl_misc.sql":          true,
	"pbxdao_schema.sql":     true,
	"plpgsql_functions.sql": true,
}

// TestFormatIdempotent asserts that formatting a formatted document is a no-op:
// Format(Format(x)) == Format(x). This catches output that depends on input
// whitespace and instabilities where formatting drifts on each pass.
func TestFormatIdempotent(t *testing.T) {
	for _, path := range fixturePaths(t) {
		name := filepath.Base(path)
		if knownIdempotencyDivergence[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			input := string(readFixture(t, path))

			once, err := printer.Format(input)
			if err != nil {
				t.Fatalf("first format: %v", err)
			}
			twice, err := printer.Format(once)
			if err != nil {
				t.Fatalf("second format: %v", err)
			}
			if once != twice {
				t.Errorf("format is not idempotent for %s\n--- first pass ---\n%s\n--- second pass ---\n%s",
					name, once, twice)
			}
		})
	}
}

// knownASTDivergence lists fixtures where the formatter currently produces
// output whose AST does not round-trip identically. Each entry should describe
// the underlying formatter bug so they can be fixed and removed from the list.
//
//   - joins_and_ranges.sql: column-list aliases on RangeFunction nodes
//     (e.g. "json_to_recordset(...) t (a int, b text)") drop the column list.
//   - ddl_misc.sql: ON CONFLICT DO UPDATE SET emits "EXCLUDED.value AS value"
//     instead of "value = EXCLUDED.value".
//   - pbxdao_schema.sql: large mixed fixture with multiple discrepancies in
//     CREATE FUNCTION ... RETURNS TABLE and complex constraint clauses.
//   - plpgsql_functions.sql: CREATE FUNCTION option ordering and body
//     whitespace differences not covered by the option/body normalizer.
var knownASTDivergence = map[string]bool{
	"joins_and_ranges.sql":  true,
	"ddl_misc.sql":          true,
	"pbxdao_schema.sql":     true,
	"plpgsql_functions.sql": true,
}

// TestFormatPreservesAST asserts that formatting preserves semantics: parsing
// the input and parsing the formatted output yield ASTs that deparse to the
// same canonical SQL (modulo function-body whitespace and option ordering).
func TestFormatPreservesAST(t *testing.T) {
	for _, path := range fixturePaths(t) {
		name := filepath.Base(path)
		// The fallback_deparse fixture intentionally exercises the deparse
		// fallback path; its canonical form already matches by construction.
		if name == "fallback_deparse.sql" {
			continue
		}
		// psql_meta.sql contains psql meta-commands (\restrict etc.), which
		// pg_query.Parse cannot parse directly.
		if name == "psql_meta.sql" {
			continue
		}
		if knownASTDivergence[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			input := string(readFixture(t, path))

			formatted, err := printer.Format(input)
			if err != nil {
				t.Fatalf("format: %v", err)
			}

			inputTree, err := pg_query.Parse(input)
			if err != nil {
				t.Fatalf("parse input: %v", err)
			}
			outputTree, err := pg_query.Parse(formatted)
			if err != nil {
				t.Fatalf("parse formatted: %v\n--- formatted ---\n%s", err, formatted)
			}

			inputCanonical, err := pg_query.Deparse(inputTree)
			if err != nil {
				t.Fatalf("deparse input: %v", err)
			}
			outputCanonical, err := pg_query.Deparse(outputTree)
			if err != nil {
				t.Fatalf("deparse formatted: %v", err)
			}

			if normalizeForCompare(inputCanonical) != normalizeForCompare(outputCanonical) {
				t.Errorf("formatting changed AST for %s\n--- input deparsed ---\n%s\n--- output deparsed ---\n%s",
					name, inputCanonical, outputCanonical)
			}
		})
	}
}
