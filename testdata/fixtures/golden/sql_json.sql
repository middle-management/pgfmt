SELECT
	JSON_OBJECT(
		'schema': 's',
		'table': 't',
		'op': 'INSERT',
		'id': 7
	);

SELECT
	JSON_OBJECT('id': 1);

SELECT
	JSON_OBJECT();

SELECT
	JSON_OBJECT(
		'a': 1,
		'b': 2
		ABSENT ON NULL WITH UNIQUE RETURNING jsonb
	);

SELECT
	JSON_ARRAY(1, 2, 3);

SELECT
	JSON_ARRAY(1, 2 NULL ON NULL RETURNING jsonb);

SELECT
	JSON_ARRAY(
		SELECT
			id
		FROM
			t
	);

SELECT
	JSON_OBJECTAGG(k: v)
FROM
	kv;

SELECT
	JSON_OBJECTAGG(k: v ABSENT ON NULL RETURNING jsonb) FILTER (WHERE v > 0)
FROM
	kv;

SELECT
	JSON_ARRAYAGG(x ORDER BY x NULL ON NULL)
FROM
	t;

SELECT
	JSON('{"a": 1}'),
	JSON('{}' WITH UNIQUE KEYS),
	JSON_SCALAR(1),
	JSON_SERIALIZE(x RETURNING text)
FROM
	t;

SELECT
	x IS JSON,
	x IS JSON ARRAY,
	x IS JSON OBJECT WITH UNIQUE,
	x IS JSON SCALAR
FROM
	t;

SELECT
	JSON_EXISTS(j, '$.a'),
	JSON_VALUE(j, '$.b' RETURNING int DEFAULT 0 ON ERROR),
	JSON_QUERY(j, '$.c' WITH UNCONDITIONAL WRAPPER)
FROM
	t;

SELECT
	add_job(
		't',
		JSON_OBJECT(
			'schema': 's',
			'table': 't',
			'op': 'x',
			'id': 9
		),
		5
	);

CREATE FUNCTION notify_task2()
RETURNS trigger
AS $$
BEGIN
	PERFORM studio_internal.add_job(
		tg_argv[0],
		JSON_OBJECT(
			'schema': tg_table_schema,
			'table': tg_table_name,
			'op': tg_op,
			'id': new.id
		),
		job_key := concat('run_task_', tg_argv[0])
	);
	RETURN new;
END
$$
LANGUAGE plpgsql;

