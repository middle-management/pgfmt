-- PL/pgSQL statement types: cursors, transaction control, diagnostics,
-- assertions, dynamic loops, and loop labels
DO $$
DECLARE
	n int;
	c CURSOR FOR SELECT id FROM t;
	cb CURSOR (key int) FOR SELECT id FROM t WHERE id = key;
	rc refcursor;
	r record;
BEGIN
	UPDATE t SET v = 1;
	GET DIAGNOSTICS n = ROW_COUNT;
	CALL other_proc(n, 2);
	ASSERT n > 0, 'no rows';
	OPEN c;
	OPEN cb(5);
	OPEN rc FOR SELECT * FROM t;
	OPEN rc FOR EXECUTE 'select 1' USING n;
	FETCH c INTO r;
	FETCH LAST FROM c INTO r;
	MOVE FORWARD 2 FROM c;
	CLOSE c;
	<<dynloop>>
	FOR r IN EXECUTE 'select * from t' USING n LOOP
		EXIT dynloop WHEN true;
	END LOOP;
	FOR r IN cb(7) LOOP
		RAISE NOTICE 'y';
	END LOOP;
	<<wl>>
	WHILE n > 0 LOOP
		n := n - 1;
	END LOOP;
END
$$;

CREATE PROCEDURE batch_update()
LANGUAGE plpgsql
AS $$
BEGIN
	UPDATE t SET v = 1;
	COMMIT;
	UPDATE t SET v = 2;
	ROLLBACK AND CHAIN;
	RAISE EXCEPTION 'bad % thing', 42 USING HINT = 'try again', ERRCODE = 'P0001';
END
$$;

CREATE FUNCTION dynamic_rows()
RETURNS SETOF int
LANGUAGE plpgsql
AS $$
BEGIN
	RETURN QUERY EXECUTE 'select 1' USING 2;
END
$$;

CREATE FUNCTION raise_variants()
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
	RAISE EXCEPTION unique_violation;
EXCEPTION
	WHEN others THEN
		RAISE NOTICE 'it''s fine';
		RAISE;
END
$$;

CREATE FUNCTION slice_walk(arr int[])
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
	row_slice int[];
BEGIN
	<<slices>>
	FOREACH row_slice SLICE 1 IN ARRAY arr LOOP
		RAISE NOTICE '%', row_slice;
	END LOOP;
END
$$;

