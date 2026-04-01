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

func TestAExprIn(t *testing.T) {
	got := format(t, "SELECT 1 WHERE x IN (1, 2, 3)")
	if !strings.Contains(got, "IN (1, 2, 3)") {
		t.Errorf("expected IN clause, got: %s", got)
	}
}

func TestAExprNotIn(t *testing.T) {
	got := format(t, "SELECT 1 WHERE x NOT IN (1, 2, 3)")
	if !strings.Contains(got, "NOT IN (1, 2, 3)") {
		t.Errorf("expected NOT IN clause, got: %s", got)
	}
}

func TestAExprNotBetween(t *testing.T) {
	got := format(t, "SELECT 1 WHERE x NOT BETWEEN 1 AND 10")
	if !strings.Contains(got, "NOT BETWEEN 1 AND 10") {
		t.Errorf("expected NOT BETWEEN clause, got: %s", got)
	}
}

func TestNullif(t *testing.T) {
	got := format(t, "SELECT NULLIF(a, b)")
	if !strings.Contains(got, "NULLIF(a, b)") {
		t.Errorf("expected NULLIF, got: %s", got)
	}
}

func TestIsDistinctFrom(t *testing.T) {
	got := format(t, "SELECT 1 WHERE a IS DISTINCT FROM b")
	if !strings.Contains(got, "IS DISTINCT FROM") {
		t.Errorf("expected IS DISTINCT FROM, got: %s", got)
	}
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

func TestPLpgSQLFallbackOnParseError(t *testing.T) {
	// Non-plpgsql language should still emit raw body
	got := format(t, `CREATE FUNCTION test_py() RETURNS void AS $$
import sys
print("hello")
$$ LANGUAGE plpython3u;`)
	if !strings.Contains(got, "import sys") {
		t.Errorf("expected raw body, got: %s", got)
	}
}

func TestAArrayExpr(t *testing.T) {
	got := format(t, `SELECT ARRAY[1, 2, 3]`)
	expected := "SELECT\n\tARRAY[1, 2, 3]"
	if got != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, got)
	}
}

func TestAIndirection(t *testing.T) {
	got := format(t, `SELECT (my_row).field_name`)
	if !strings.Contains(got, "(my_row).field_name") {
		t.Errorf("expected indirection with parens, got: %s", got)
	}
}

func TestAIndirectionArraySubscript(t *testing.T) {
	got := format(t, `SELECT arr[1]`)
	if !strings.Contains(got, "arr[1]") {
		t.Errorf("expected array subscript, got: %s", got)
	}
}

func TestRowExpr(t *testing.T) {
	got := format(t, `SELECT ROW(1, 2, 3)`)
	if !strings.Contains(got, "ROW(1, 2, 3)") {
		t.Errorf("expected ROW expression, got: %s", got)
	}
}

func TestConstraintsSetStmt(t *testing.T) {
	got := format(t, `SET CONSTRAINTS ALL DEFERRED`)
	if got != "SET CONSTRAINTS ALL DEFERRED" {
		t.Errorf("expected SET CONSTRAINTS ALL DEFERRED, got: %s", got)
	}
}

func TestConstraintsSetStmtNamed(t *testing.T) {
	got := format(t, `SET CONSTRAINTS my_fk IMMEDIATE`)
	if got != "SET CONSTRAINTS my_fk IMMEDIATE" {
		t.Errorf("expected SET CONSTRAINTS my_fk IMMEDIATE, got: %s", got)
	}
}

func TestAlterTableEnableRowSecurity(t *testing.T) {
	got := format(t, `ALTER TABLE t ENABLE ROW LEVEL SECURITY`)
	if !strings.Contains(got, "ENABLE ROW LEVEL SECURITY") {
		t.Errorf("expected ENABLE ROW LEVEL SECURITY, got: %s", got)
	}
}

func TestCommentOnConstraint(t *testing.T) {
	got := format(t, `COMMENT ON CONSTRAINT my_constraint ON my_table IS 'hello'`)
	if !strings.Contains(got, "CONSTRAINT my_constraint ON my_table") {
		t.Errorf("expected COMMENT ON CONSTRAINT ... ON ..., got: %s", got)
	}
}

func TestDoStatementSQL(t *testing.T) {
	got := format(t, `DO LANGUAGE sql $$
  SELECT 1;
$$;`)
	if !strings.Contains(got, "DO LANGUAGE sql $$") {
		t.Errorf("expected DO LANGUAGE sql, got: %s", got)
	}
}
