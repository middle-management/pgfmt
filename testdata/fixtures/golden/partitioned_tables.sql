-- Partitioned tables, typed tables, LIKE clauses, and storage options
CREATE TABLE measurements (
	id int,
	ts date
) PARTITION BY RANGE (id, date_trunc('day', ts));

CREATE TABLE m_p1 PARTITION OF measurements FOR VALUES FROM (1, minvalue) TO (10, maxvalue);

CREATE TABLE m_list PARTITION OF measurements FOR VALUES IN ('a', 'b');

CREATE TABLE m_hash PARTITION OF measurements FOR VALUES WITH (MODULUS 4, REMAINDER 0);

CREATE TABLE m_default PARTITION OF measurements DEFAULT;

CREATE TABLE m_sub PARTITION OF measurements (
	b NOT NULL,
	CONSTRAINT positive CHECK (b > 0)
) FOR VALUES IN ('c') PARTITION BY LIST (id);

CREATE TABLE part_coll (
	a text
) PARTITION BY RANGE (a COLLATE "C" text_pattern_ops);

CREATE TABLE part_expr (
	a int,
	b int
) PARTITION BY HASH ((a + b));

CREATE TABLE typed_table OF employee_type (
	id PRIMARY KEY,
	name NOT NULL
);

CREATE TABLE typed_plain OF employee_type;

CREATE TABLE clone (
	LIKE source_table INCLUDING ALL
);

CREATE TABLE partial_clone (
	id int,
	LIKE source_table INCLUDING DEFAULTS INCLUDING INDEXES
);

CREATE TEMPORARY TABLE scratch (
	id int
) WITH (fillfactor = 70) ON COMMIT DROP;

CREATE TABLE with_am (
	id int
) USING heap;

CREATE TABLE in_space (
	id int
) TABLESPACE fast_disk;

