-- FROM-item fallbacks, cursors, grants, COPY variants, multi-assignment,
-- vacuum options, schemas, sequences, and interval typmods
SELECT t.id FROM test_tablesample AS t TABLESAMPLE SYSTEM (50) REPEATABLE (0);

DELETE FROM uctest WHERE CURRENT OF c1;

UPDATE uctest SET f1 = 8 WHERE CURRENT OF c1;

GRANT EXECUTE ON FUNCTION f_leak(text) TO public;

GRANT ALL (one) ON atest5 TO u3;

GRANT SELECT ON ALL TABLES IN SCHEMA app TO readonly WITH GRANT OPTION;

REVOKE GRANT OPTION FOR SELECT ON t FROM u CASCADE;

GRANT USAGE ON SCHEMA app TO worker;

SELECT (jsonb_populate_record(NULL::jsbrec, js)).* FROM jsbpoptest;

SELECT ('123'::jsonb)['a'];

COPY (UPDATE copydml_test SET t = 'g' WHERE t = 'f' RETURNING id) TO STDOUT;

COPY (SELECT * FROM copy_t ORDER BY a) TO STDOUT WITH (DELIMITER ',', FORMAT csv, HEADER true);

UPDATE update_test SET (c, b, a) = ('bugle', b + 11, DEFAULT) WHERE c = 'foo';

INSERT INTO upsert_test VALUES (1, 'Baz') ON CONFLICT (a)
  DO UPDATE SET (b, a) = (SELECT b, a FROM aaa) RETURNING *;

INSERT INTO odd_names ("mixed Case column") VALUES (10);

SELECT INTERVAL '999' SECOND(3);

SELECT INTERVAL '1-2' YEAR TO MONTH;

SELECT '1 day'::interval(2);

SELECT XMLPARSE(CONTENT doc PRESERVE WHITESPACE) FROM x;

VACUUM (FULL, FREEZE, ANALYZE) vaccluster, vactst;

VACUUM ANALYZE vactst (a, b);

CREATE SCHEMA AUTHORIZATION owner_role
  CREATE TABLE tab (id int);

CREATE SEQUENCE seq_typed AS smallint INCREMENT 2;

ALTER SEQUENCE seq_typed AS bigint;

WITH RECURSIVE outermost(x) AS (
  SELECT 1
  UNION (WITH innermost1 AS (SELECT 2) SELECT * FROM innermost1)
)
SELECT * FROM outermost;
