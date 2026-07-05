-- Multiple pairs break one pair per line
SELECT
	json_build_object(
		'schema', 's',
		'table', 't',
		'op', o.op,
		'id', o.id
	) AS payload
FROM
	ops AS o;

-- Single pair stays on one line
SELECT
	json_build_object('id', 1);

-- jsonb variant
SELECT
	jsonb_build_object(
		'a', 1,
		'b', 2,
		'c', 3
	);

-- Nested key/value calls
SELECT
	json_build_object(
		'user', json_build_object(
			'id', u.id,
			'name', u.name,
			'email', u.email
		),
		'active', u.active
	)
FROM
	users AS u;

-- A call containing a multi-line key/value call breaks one argument per line
SELECT
	COALESCE(NULLIF(json_build_object(
		'a', 1,
		'b', 2,
		'c', 3
	)::text, '{}'), '{}');

-- Odd number of arguments (invalid at runtime, still formats)
SELECT
	json_build_object('a', 1, 'b');

-- Inside a plpgsql trigger function with named arguments
CREATE FUNCTION notify_task()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
	PERFORM studio_internal.add_job(
		tg_argv[0],
		json_build_object(
			'schema', tg_table_schema,
			'table', tg_table_name,
			'op', tg_op,
			'id', new.id
		),
		job_key := concat('run_task_', tg_argv[0], '_', new.task_key),
		queue_name := concat('run_task_', tg_argv[0], '_', new.queue_name)
	);
	RETURN new;
END
$$;

-- Short enough plpgsql statements stay compact
CREATE FUNCTION tiny()
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
	PERFORM log_event(json_build_object('op', 'x', 'id', 1));
END
$$;

-- In WHERE and UPDATE SET
UPDATE jobs
SET
	payload = jsonb_build_object(
		'schema', 's',
		'table', 't',
		'op', 'UPDATE',
		'id', 7
	)
WHERE
	payload = jsonb_build_object('id', 7);

