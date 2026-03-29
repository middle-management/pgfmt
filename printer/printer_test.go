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

func TestColumnRefDots(t *testing.T) {
	got := format(t, "SELECT t.id, t.name FROM tbl t")
	if !strings.Contains(got, "t.id") {
		t.Errorf("expected t.id, got: %s", got)
	}
	if !strings.Contains(got, "t.name") {
		t.Errorf("expected t.name, got: %s", got)
	}
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

func TestAExprLike(t *testing.T) {
	got := format(t, "SELECT 1 WHERE name LIKE '%foo%'")
	if !strings.Contains(got, "LIKE '%foo%'") {
		t.Errorf("expected LIKE clause, got: %s", got)
	}
}

func TestAExprILike(t *testing.T) {
	got := format(t, "SELECT 1 WHERE name ILIKE '%foo%'")
	if !strings.Contains(got, "ILIKE '%foo%'") {
		t.Errorf("expected ILIKE clause, got: %s", got)
	}
}

func TestAExprBetween(t *testing.T) {
	got := format(t, "SELECT 1 WHERE x BETWEEN 1 AND 10")
	if !strings.Contains(got, "BETWEEN 1 AND 10") {
		t.Errorf("expected BETWEEN clause, got: %s", got)
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

func TestTypeCastAConst(t *testing.T) {
	got := format(t, "SELECT 'hello'::text")
	if !strings.Contains(got, "'hello'::") {
		t.Errorf("expected typecast, got: %s", got)
	}
}

func TestJoinExpr(t *testing.T) {
	got := format(t, "SELECT * FROM a JOIN b ON a.id = b.id")
	if !strings.Contains(got, "JOIN") {
		t.Errorf("expected JOIN, got: %s", got)
	}
	if !strings.Contains(got, "a.id") {
		t.Errorf("expected a.id, got: %s", got)
	}
}

func TestLeftJoin(t *testing.T) {
	got := format(t, "SELECT * FROM a LEFT JOIN b ON a.id = b.id")
	if !strings.Contains(got, "LEFT JOIN") {
		t.Errorf("expected LEFT JOIN, got: %s", got)
	}
}

func TestCreateTable(t *testing.T) {
	got := format(t, "CREATE TABLE foo (id int PRIMARY KEY, name varchar(255) NOT NULL)")
	if !strings.Contains(got, "CREATE TABLE") {
		t.Errorf("expected CREATE TABLE, got: %s", got)
	}
	if !strings.Contains(got, "PRIMARY KEY") {
		t.Errorf("expected PRIMARY KEY, got: %s", got)
	}
}

func TestDropTable(t *testing.T) {
	got := format(t, "DROP TABLE IF EXISTS foo")
	if !strings.Contains(got, "DROP TABLE IF EXISTS foo") {
		t.Errorf("expected DROP TABLE IF EXISTS, got: %s", got)
	}
}

func TestTransaction(t *testing.T) {
	for _, sql := range []string{"BEGIN", "COMMIT", "ROLLBACK"} {
		got := format(t, sql)
		if !strings.Contains(got, sql) {
			t.Errorf("expected %s, got: %s", sql, got)
		}
	}
}

func TestInsertValues(t *testing.T) {
	got := format(t, "INSERT INTO foo (a, b) VALUES (1, 2)")
	if !strings.Contains(got, "VALUES (1, 2)") {
		t.Errorf("expected VALUES (1, 2), got: %s", got)
	}
}

func TestInsertOnConflict(t *testing.T) {
	got := format(t, "INSERT INTO foo (id, name) VALUES (1, 'a') ON CONFLICT (id) DO NOTHING")
	if !strings.Contains(got, "ON CONFLICT") {
		t.Errorf("expected ON CONFLICT, got: %s", got)
	}
	if !strings.Contains(got, "DO NOTHING") {
		t.Errorf("expected DO NOTHING, got: %s", got)
	}
}

func TestInsertReturning(t *testing.T) {
	got := format(t, "INSERT INTO foo (name) VALUES ('a') RETURNING id")
	if !strings.Contains(got, "RETURNING") || !strings.Contains(got, "id") {
		t.Errorf("expected RETURNING with id, got: %s", got)
	}
}

func TestUpdate(t *testing.T) {
	got := format(t, "UPDATE foo SET name = 'bar' WHERE id = 1")
	if !strings.Contains(got, "UPDATE") {
		t.Errorf("expected UPDATE, got: %s", got)
	}
	if !strings.Contains(got, "SET") {
		t.Errorf("expected SET, got: %s", got)
	}
}

func TestDelete(t *testing.T) {
	got := format(t, "DELETE FROM foo WHERE id = 1")
	if !strings.Contains(got, "DELETE FROM") {
		t.Errorf("expected DELETE FROM, got: %s", got)
	}
}

func TestAlterTable(t *testing.T) {
	got := format(t, "ALTER TABLE foo ADD COLUMN bar int")
	if !strings.Contains(got, "ALTER TABLE") {
		t.Errorf("expected ALTER TABLE, got: %s", got)
	}
	if !strings.Contains(got, "ADD COLUMN") {
		t.Errorf("expected ADD COLUMN, got: %s", got)
	}
}

func TestAggOrderBy(t *testing.T) {
	got := format(t, "SELECT string_agg(x, ',' ORDER BY y) FROM t")
	if !strings.Contains(got, "ORDER BY y") {
		t.Errorf("expected ORDER BY in aggregate, got: %s", got)
	}
}

func TestAggFilter(t *testing.T) {
	got := format(t, "SELECT count(*) FILTER (WHERE x > 5) FROM t")
	if !strings.Contains(got, "FILTER (WHERE") {
		t.Errorf("expected FILTER clause, got: %s", got)
	}
}

func TestAggWithinGroup(t *testing.T) {
	got := format(t, "SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY x) FROM t")
	if !strings.Contains(got, "WITHIN GROUP (ORDER BY x)") {
		t.Errorf("expected WITHIN GROUP, got: %s", got)
	}
}

func TestWindowFunction(t *testing.T) {
	got := format(t, "SELECT count(*) OVER (PARTITION BY x ORDER BY y) FROM t")
	if !strings.Contains(got, "OVER (PARTITION BY x ORDER BY y)") {
		t.Errorf("expected OVER clause, got: %s", got)
	}
}

func TestWindowFunctionNamedRef(t *testing.T) {
	got := format(t, "SELECT sum(x) OVER w FROM t WINDOW w AS (PARTITION BY y)")
	if !strings.Contains(got, "OVER w") {
		t.Errorf("expected OVER w, got: %s", got)
	}
	if !strings.Contains(got, "WINDOW") {
		t.Errorf("expected WINDOW clause, got: %s", got)
	}
}

func TestWindowFrameClause(t *testing.T) {
	got := format(t, "SELECT row_number() OVER (ORDER BY x ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM t")
	if !strings.Contains(got, "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW") {
		t.Errorf("expected frame clause, got: %s", got)
	}
}

func TestGroupByDistinct(t *testing.T) {
	got := format(t, "SELECT x FROM t GROUP BY DISTINCT x")
	if !strings.Contains(got, "GROUP BY DISTINCT") {
		t.Errorf("expected GROUP BY DISTINCT, got: %s", got)
	}
}

func TestForUpdate(t *testing.T) {
	got := format(t, "SELECT * FROM t FOR UPDATE")
	if !strings.Contains(got, "FOR UPDATE") {
		t.Errorf("expected FOR UPDATE, got: %s", got)
	}
}

func TestForShareNowait(t *testing.T) {
	got := format(t, "SELECT * FROM t FOR SHARE OF t NOWAIT")
	if !strings.Contains(got, "FOR SHARE OF t NOWAIT") {
		t.Errorf("expected FOR SHARE OF t NOWAIT, got: %s", got)
	}
}

func TestForUpdateSkipLocked(t *testing.T) {
	got := format(t, "SELECT * FROM t FOR UPDATE SKIP LOCKED")
	if !strings.Contains(got, "FOR UPDATE SKIP LOCKED") {
		t.Errorf("expected FOR UPDATE SKIP LOCKED, got: %s", got)
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

func TestPLpgSQLSimpleFunction(t *testing.T) {
	got := format(t, `CREATE FUNCTION greet(name text) RETURNS text AS $$
DECLARE result text;
BEGIN result := 'Hello, ' || name; RETURN result; END;
$$ LANGUAGE plpgsql;`)
	if !strings.Contains(got, "DECLARE") {
		t.Errorf("expected DECLARE, got: %s", got)
	}
	if !strings.Contains(got, "RETURN result;") {
		t.Errorf("expected RETURN result, got: %s", got)
	}
	if !strings.Contains(got, "result := 'Hello, ' || name;") {
		t.Errorf("expected assignment, got: %s", got)
	}
}

func TestPLpgSQLIfElse(t *testing.T) {
	got := format(t, `CREATE FUNCTION test_if(x integer) RETURNS text AS $$
BEGIN
  IF x > 0 THEN RETURN 'pos';
  ELSIF x = 0 THEN RETURN 'zero';
  ELSE RETURN 'neg';
  END IF;
END;
$$ LANGUAGE plpgsql;`)
	if !strings.Contains(got, "IF x > 0 THEN") {
		t.Errorf("expected IF, got: %s", got)
	}
	if !strings.Contains(got, "ELSIF x = 0 THEN") {
		t.Errorf("expected ELSIF, got: %s", got)
	}
	if !strings.Contains(got, "ELSE") {
		t.Errorf("expected ELSE, got: %s", got)
	}
	if !strings.Contains(got, "END IF;") {
		t.Errorf("expected END IF, got: %s", got)
	}
}

func TestPLpgSQLForLoop(t *testing.T) {
	got := format(t, `CREATE FUNCTION test_loop() RETURNS void AS $$
DECLARE i integer;
BEGIN
  FOR i IN 1..10 LOOP
    RAISE NOTICE '%', i;
  END LOOP;
END;
$$ LANGUAGE plpgsql;`)
	if !strings.Contains(got, "FOR i IN 1..10 LOOP") {
		t.Errorf("expected FOR loop, got: %s", got)
	}
	if !strings.Contains(got, "END LOOP;") {
		t.Errorf("expected END LOOP, got: %s", got)
	}
}

func TestPLpgSQLException(t *testing.T) {
	got := format(t, `CREATE FUNCTION test_exc() RETURNS void AS $$
BEGIN
  RAISE EXCEPTION 'fail';
EXCEPTION
  WHEN others THEN
    RAISE NOTICE 'caught';
END;
$$ LANGUAGE plpgsql;`)
	if !strings.Contains(got, "EXCEPTION") {
		t.Errorf("expected EXCEPTION, got: %s", got)
	}
	if !strings.Contains(got, "WHEN others THEN") {
		t.Errorf("expected WHEN others, got: %s", got)
	}
}

func TestPLpgSQLPerform(t *testing.T) {
	got := format(t, `CREATE FUNCTION test_perf() RETURNS void AS $$
BEGIN
  PERFORM pg_notify('chan', 'msg');
END;
$$ LANGUAGE plpgsql;`)
	if !strings.Contains(got, "PERFORM") || !strings.Contains(got, "pg_notify") {
		t.Errorf("expected PERFORM with pg_notify, got: %s", got)
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
