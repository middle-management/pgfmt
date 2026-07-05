-- JOIN USING, natural join variants, join aliases, quoted identifiers,
-- and object references in COMMENT ON / DROP
SELECT
	*
FROM
	a
	JOIN b USING (x)
	JOIN c ON c.id = b.id
	LEFT JOIN d USING (y) AS dx;

SELECT
	*
FROM
	t1
	NATURAL LEFT JOIN t2;

SELECT
	*
FROM
	(t1
	JOIN t2 ON t1.a = t2.a) AS jj;

SELECT
	1 AS "my alias",
	CURRENT_USER AS "user"
FROM
	"My Table" AS "T"("Col A", "Col B");

SELECT
	"Weird Col"
FROM
	sch."Mixed Case";

WITH 
"my cte"("a col") AS (
	SELECT
		1
)
SELECT
	*
FROM
	"my cte";

CREATE INDEX i ON t USING brin (v) WITH (pages_per_range = 2, autosummarize = true);

COMMENT ON RULE r1 ON v1 IS 'rule comment';

COMMENT ON TRIGGER tg ON sch.tbl IS 'trigger comment';

COMMENT ON POLICY p ON t IS NULL;

COMMENT ON EVENT TRIGGER et IS 'event trigger comment';

COMMENT ON MATERIALIZED VIEW mv IS 'mv comment';

COMMENT ON CONSTRAINT c ON DOMAIN d IS 'domain constraint';

COMMENT ON CAST (int AS bool) IS 'cast comment';

COMMENT ON OPERATOR CLASS oc USING btree IS 'opclass comment';

COMMENT ON AGGREGATE myavg(int) IS 'aggregate comment';

DROP OPERATOR CLASS test_ops USING btree;

DROP ROUTINE f1(), p1();

DROP EVENT TRIGGER et;

DROP RULE r1 ON v1;

DROP POLICY IF EXISTS p ON t;

DROP INDEX CONCURRENTLY idx;

DROP CAST (int AS bool);

DROP STATISTICS s1;

DROP SERVER srv CASCADE;

