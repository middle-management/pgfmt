package printer

import (
	"regexp"
	"strings"
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// normalizeDeparseForCompare strips $$ function bodies and reorderable
// function options so that reformatted bodies don't cause false mismatches.
var (
	dollarBodyRe  = regexp.MustCompile(`\$\$[\s\S]*?\$\$`)
	funcOptionsRe = regexp.MustCompile(`\s+(LANGUAGE \w+|VOLATILE|STABLE|IMMUTABLE|STRICT|CALLED ON NULL INPUT|RETURNS NULL ON NULL INPUT|SECURITY DEFINER|SECURITY INVOKER|PARALLEL \w+)`)
	multiSpaceRe  = regexp.MustCompile(`\s+`)
)

func normalizeDeparseForCompare(s string) string {
	s = dollarBodyRe.ReplaceAllString(s, "")
	s = funcOptionsRe.ReplaceAllString(s, "")
	s = multiSpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func format(t *testing.T, sql string) string {
	t.Helper()
	result, err := pg_query.Parse(sql)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	var out strings.Builder
	for _, stmt := range result.Stmts {
		b := &strings.Builder{}
		p := &Printer{Builder: b}
		p.Print(stmt.Stmt)
		out.WriteString(b.String())
	}
	formatted := out.String()

	// Verify the formatted output is semantically identical to the input
	// by comparing deparsed (canonical) forms of both parse trees.
	inputCanonical, err := pg_query.Deparse(result)
	if err != nil {
		t.Fatalf("deparse input error: %v", err)
	}
	outputTree, err := pg_query.Parse(formatted)
	if err != nil {
		t.Fatalf("formatted output failed to parse: %v\nformatted:\n%s", err, formatted)
	}
	outputCanonical, err := pg_query.Deparse(outputTree)
	if err != nil {
		t.Fatalf("deparse output error: %v", err)
	}
	if normalizeDeparseForCompare(inputCanonical) != normalizeDeparseForCompare(outputCanonical) {
		t.Errorf("formatting changed query semantics:\n--- input deparsed ---\n%s\n--- output deparsed ---\n%s", inputCanonical, outputCanonical)
	}

	return formatted
}

func TestJoinIndentInSubquery(t *testing.T) {
	got := format(t, `SELECT * FROM a WHERE EXISTS (SELECT 1 FROM x JOIN y ON x.id = y.id)`)
	// JOINs inside subqueries should be indented relative to the subquery, not column 0
	if strings.Contains(got, "\n\tJOIN") && !strings.Contains(got, "\t\t\tJOIN") {
		t.Errorf("expected JOINs indented inside subquery, got:\n%s", got)
	}
}

func TestNoPanics(t *testing.T) {
	// These should not panic even if output is imperfect
	sqls := []string{
		"SELECT * FROM a WHERE x IN (SELECT id FROM b)",
		"SELECT * FROM a WHERE x NOT IN (1,2,3)",
		"SELECT NULLIF(a, 0)",
		"SELECT 1 WHERE a IS DISTINCT FROM b",
		"SELECT 1 WHERE x BETWEEN 1 AND 10",
		"SELECT 'x'::text",
		"SELECT * FROM a JOIN b ON a.id = b.id",
		"BEGIN",
		"COMMIT",
		"ROLLBACK",
		"DROP TABLE foo",
		"CREATE TABLE t (id int)",
		"ALTER TABLE t ADD COLUMN x int",
		"INSERT INTO t VALUES (1) ON CONFLICT DO NOTHING",
		"INSERT INTO t VALUES (1) RETURNING id",
		"UPDATE t SET x = 1",
		"DELETE FROM t WHERE id = 1",
	}
	for _, sql := range sqls {
		t.Run(sql, func(t *testing.T) {
			// Should not panic
			format(t, sql)
		})
	}
}
