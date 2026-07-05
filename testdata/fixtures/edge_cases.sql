-- Identifier quoting in DDL lists, literals, and statement edge cases
ALTER TABLE atacc1 ALTER "........pg.dropped.1........" SET STORAGE PLAIN;

ALTER TABLE atacc1 ADD PRIMARY KEY ("Weird Col");

ALTER TABLE atacc1 ADD FOREIGN KEY ("a b") REFERENCES other ("c d");

ANALYZE atacc1 ("........pg.dropped.1........");

VACUUM (BUFFER_USAGE_LIMIT '512 kB') vac_option_tab;

COMMENT ON COLUMN atacc1."........pg.dropped.1........" IS 'testing';

COPY weird ("mixed Case") FROM stdin;

COPY (SELECT t FROM test1) TO STDOUT WITH (FORMAT csv, HEADER true, FORCE_QUOTE (t));

UNLISTEN *;

DEALLOCATE ALL;

SELECT b'0101', x'1F';

CREATE TABLE error_tbl (b1 bool DEFAULT (1 IN (1, 2)));

CREATE TABLE opts_tbl (i int) WITH (fillfactor = 30, autovacuum_analyze_scale_factor = 0.2);

COMMENT ON AGGREGATE newcnt(*) IS 'an agg(*) comment';

DROP AGGREGATE IF EXISTS test_aggregate_exists(*);

SET custom."bad-guc" = 42;

SHOW custom."bad-guc";

DROP SCHEMA "CURRENT_SCHEMA" CASCADE;

SELECT xmlpi(name "xml-stylesheet", 'href="mystyle.css"');

SELECT xmlroot(doc, version '2.0', standalone yes) FROM x;

SELECT xmlroot(doc, version no value, standalone no value) FROM x;

SELECT "normalize"('abc', 'def');

CREATE TRIGGER tsvectorupdate BEFORE INSERT OR UPDATE ON test_tsvector
FOR EACH ROW EXECUTE FUNCTION tsvector_update_trigger(a, 'pg_catalog.english', t);

SELECT * FROM ROWS FROM (rngfunc_sql(11, 13), rngfunc_mat(11, 13)) WITH ORDINALITY AS f (i1, s1, i2, s2, o);

SELECT * FROM json_to_recordset('[]') AS t (a int, b text);

SELECT thousand FROM onek ORDER BY thousand FETCH FIRST (NULL + 1) ROWS WITH TIES;

DO $do$
BEGIN
  RAISE EXCEPTION $$Nested "%" dollars$$, 42;
END
$do$;
