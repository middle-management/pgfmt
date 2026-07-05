-- COLLATE clauses on expressions and index columns
SELECT
	name COLLATE "en_US"
FROM
	t;

SELECT
	*
FROM
	t
ORDER BY
	name COLLATE "C";

SELECT
	(a || b) COLLATE "C"
FROM
	t;

SELECT
	*
FROM
	t
WHERE
	name < 'm' COLLATE pg_catalog."en_US";

CREATE TABLE names (
	name text COLLATE "sv_SE"
);

CREATE INDEX idx_name_c ON t USING btree (name COLLATE "C");

