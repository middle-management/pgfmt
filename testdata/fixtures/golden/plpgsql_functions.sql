CREATE FUNCTION get_user_label(p_id "pg_catalog"."int4")
RETURNS "text"
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

CREATE FUNCTION loop_demo()
RETURNS "void"
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
	RETURN;
END
$$;

CREATE OR REPLACE FUNCTION safe_divide(a "pg_catalog"."int4", b "pg_catalog"."int4")
RETURNS "pg_catalog"."numeric"
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

CREATE FUNCTION kitchen_sink(p_mode "pg_catalog"."int4")
RETURNS "void"
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
	RETURN;
END
$$;

CREATE FUNCTION iter_demo()
RETURNS "void"
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
	RETURN;
END
$$;

CREATE PROCEDURE refresh_cache()
LANGUAGE plpgsql
AS $$
BEGIN
	PERFORM pg_notify('cache', 'refresh');
	RETURN;
END
$$;

CREATE FUNCTION get_active_users()
RETURNS SETOF "record"
LANGUAGE plpgsql
STABLE
AS $$
BEGIN
	RETURN QUERY SELECT id, name FROM users WHERE active = true;
	RETURN;
END
$$;

