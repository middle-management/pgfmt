-- Simple function with DECLARE and IF/ELSE
CREATE FUNCTION get_user_label(p_id int)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
	result text;
BEGIN
	IF p_id > 0 THEN
		SELECT name INTO result FROM users WHERE id = p_id;
	ELSIF p_id = 0 THEN
		result := 'zero';
	ELSE
		result := 'negative';
	END IF;
	RETURN result;
END
$$;

-- Function with loops
CREATE FUNCTION loop_demo()
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
	counter integer := 0;
	v integer := 1;
BEGIN
	FOR i IN 1..10 LOOP
		counter := counter + 1;
	END LOOP;
	WHILE counter > 0 LOOP
		counter := counter - 1;
	END LOOP;
	LOOP
		EXIT WHEN v > 10;
		CONTINUE WHEN v = 5;
		v := v + 1;
	END LOOP;
END
$$;

-- Function with EXCEPTION handling
CREATE OR REPLACE FUNCTION safe_divide(a int, b int)
RETURNS numeric
LANGUAGE plpgsql
STRICT
AS $$
DECLARE
	result numeric;
BEGIN
	result := a::numeric / b::numeric;
	RETURN result;
EXCEPTION
	WHEN division_by_zero THEN
		RETURN NULL;
	WHEN others THEN
		RAISE;
END
$$;

-- Function with PERFORM, EXECUTE, CASE
CREATE FUNCTION kitchen_sink(p_mode int)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
	v integer := 0;
BEGIN
	PERFORM pg_notify('channel', 'hello');
	EXECUTE 'SELECT 1';
	EXECUTE 'SELECT $1' USING 42;
	CASE p_mode
		WHEN 1 THEN
			RAISE NOTICE 'mode one';
		WHEN 2 THEN
			RAISE NOTICE 'mode two';
		ELSE
			RAISE NOTICE 'other mode';
	END CASE;
	CASE
		WHEN v > 0 THEN
			RAISE LOG 'positive';
		ELSE
			RAISE DEBUG 'non-positive';
	END CASE;
END
$$;

-- Function with FOR-query and FOREACH
CREATE FUNCTION iter_demo()
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
	r record;
	arr integer[] := ARRAY[1,2,3];
	v integer;
BEGIN
	FOR r IN SELECT id, name FROM users LOOP
		RAISE NOTICE '% %', r.id, r.name;
	END LOOP;
	FOREACH v IN ARRAY arr LOOP
		RAISE NOTICE 'val=%', v;
	END LOOP;
END
$$;

-- Procedure (not function)
CREATE PROCEDURE refresh_cache()
LANGUAGE plpgsql
AS $$
BEGIN
	PERFORM pg_notify('cache', 'refresh');
END
$$;

-- Non-plpgsql language should preserve raw body
CREATE FUNCTION test_py()
RETURNS void
LANGUAGE plpython3u
AS $$
import sys
print("hello")
$$;

-- Function with RETURN QUERY
CREATE FUNCTION get_active_users()
RETURNS SETOF record
LANGUAGE plpgsql
STABLE
AS $$
BEGIN
	RETURN QUERY SELECT id, name FROM users WHERE active = true;
END
$$;

-- Function options: SET ... TO, SET ... FROM CURRENT (previously dropped)
CREATE OR REPLACE FUNCTION add_schedule_tick()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog', 'pg_temp'
AS $$
BEGIN
	RETURN NEW;
END
$$;

CREATE FUNCTION with_current_setting()
RETURNS void
LANGUAGE plpgsql
SET work_mem FROM CURRENT
SET statement_timeout TO DEFAULT
AS $$
BEGIN
	PERFORM 1;
END
$$;

-- NULL statements are compiled away by the parser; empty bodies must
-- round-trip as NULL; instead of being dropped.
CREATE FUNCTION do_nothing()
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
	NULL;
END
$$;

CREATE FUNCTION null_branches(x int)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
	IF x > 0 THEN
		NULL;
	END IF;
	CASE x
		WHEN 1 THEN
			RAISE NOTICE 'one';
		ELSE
			NULL;
	END CASE;
	BEGIN
		PERFORM 1;
	EXCEPTION
		WHEN others THEN
			NULL;
	END;
END
$$;

