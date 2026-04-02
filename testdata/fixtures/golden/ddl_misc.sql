-- DROP various object types
DROP TABLE IF EXISTS old_data CASCADE;

DROP INDEX IF EXISTS idx_users_email;

DROP VIEW IF EXISTS v_active_users;

DROP FUNCTION IF EXISTS my_func(int, text);

DROP SCHEMA IF EXISTS staging CASCADE;

DROP SEQUENCE IF EXISTS order_id_seq;

DROP EXTENSION IF EXISTS pgcrypto;

-- ALTER TABLE operations
ALTER TABLE users
	ADD COLUMN phone text;

ALTER TABLE users
	DROP COLUMN IF EXISTS legacy_field;

ALTER TABLE users
	ALTER COLUMN name SET NOT NULL;

ALTER TABLE users
	ALTER COLUMN status DROP NOT NULL;

ALTER TABLE users
	ALTER COLUMN name TYPE varchar(255);

ALTER TABLE users
	ALTER COLUMN score SET DEFAULT 0;

ALTER TABLE users
	ALTER COLUMN score DROP DEFAULT;

ALTER TABLE users
	ADD CONSTRAINT uq_email UNIQUE (email);

ALTER TABLE users
	DROP CONSTRAINT IF EXISTS old_check;

-- Transaction control
BEGIN;

COMMIT;

-- CREATE TABLE with various constraints
CREATE TABLE orders (
	id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	user_id int NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	total numeric(10, 2) NOT NULL CHECK (total >= 0),
	status text NOT NULL DEFAULT 'pending',
	metadata jsonb,
	created_at timestamptz NOT NULL DEFAULT now()
);

-- CREATE TEMPORARY TABLE
CREATE TEMPORARY TABLE tmp_results (
	id serial PRIMARY KEY,
	value text
);

-- INSERT with ON CONFLICT DO UPDATE
INSERT INTO
	settings (key, value)
VALUES ('theme', 'dark')
ON CONFLICT (key) DO UPDATE SET excluded.value AS value;

-- INSERT with ON CONFLICT DO NOTHING
INSERT INTO
	tags (name)
VALUES ('new')
ON CONFLICT DO NOTHING;

-- SET CONSTRAINTS
SET CONSTRAINTS ALL DEFERRED;

SET CONSTRAINTS my_fk IMMEDIATE;

-- ALTER TABLE ENABLE ROW LEVEL SECURITY
ALTER TABLE t
	ENABLE ROW LEVEL SECURITY;

-- COMMENT ON CONSTRAINT
COMMENT ON CONSTRAINT my_constraint ON my_table IS 'hello';

-- SQL-language function
CREATE FUNCTION add_numbers(a int, b int)
RETURNS int
LANGUAGE sql
STABLE
AS $$
	SELECT
		a + b
$$;

-- Function with OUT parameters
CREATE FUNCTION get_stats(OUT min_val int, OUT max_val int)
LANGUAGE sql
AS $$
	SELECT
		min(value),
		max(value)
	FROM
		data
$$;

-- Function with default parameter
CREATE FUNCTION greet(name text DEFAULT 'World')
RETURNS text
LANGUAGE sql
AS $$
	SELECT
		('Hello, ' || name) || '!'
$$;

-- CHECK constraint with complex boolean expression
CREATE TABLE asset_package (
	id uuid NOT NULL,
	type text,
	element_id uuid,
	variable_id uuid,
	CONSTRAINT variable_type_check CHECK (
		(
			(type = 'VARIABLE'::text)
			AND variable_id IS NOT NULL
			AND element_id IS NOT NULL
		)
		OR (type <> 'VARIABLE'::text)
	)
);

