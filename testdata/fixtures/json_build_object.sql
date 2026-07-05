-- Multiple pairs break one pair per line
SELECT json_build_object('schema', 's', 'table', 't', 'op', o.op, 'id', o.id) AS payload FROM ops o;

-- Single pair stays on one line
SELECT json_build_object('id', 1);

-- jsonb variant
SELECT jsonb_build_object('a', 1, 'b', 2, 'c', 3);

-- Nested key/value calls
SELECT json_build_object('user', json_build_object('id', u.id, 'name', u.name, 'email', u.email), 'active', u.active) FROM users u;

-- A call containing a multi-line key/value call breaks one argument per line
SELECT coalesce(nullif(json_build_object('a', 1, 'b', 2, 'c', 3)::text, '{}'), '{}');

-- Odd number of arguments (invalid at runtime, still formats)
SELECT json_build_object('a', 1, 'b');

-- Inside a plpgsql trigger function with named arguments
CREATE FUNCTION notify_task() RETURNS trigger AS $$
BEGIN
  PERFORM studio_internal.add_job(
    TG_ARGV[0],
    json_build_object('schema', TG_TABLE_SCHEMA, 'table', TG_TABLE_NAME, 'op', TG_OP, 'id', new.id),
    job_key := CONCAT('run_task_', TG_ARGV[0], '_', new.task_key),
    queue_name := CONCAT('run_task_', TG_ARGV[0], '_', new.queue_name)
  );
  RETURN new;
END
$$ LANGUAGE plpgsql;

-- Short enough plpgsql statements stay compact
CREATE FUNCTION tiny() RETURNS void AS $$
BEGIN
  PERFORM log_event(json_build_object('op', 'x', 'id', 1));
END
$$ LANGUAGE plpgsql;

-- In WHERE and UPDATE SET
UPDATE jobs SET payload = jsonb_build_object('schema', 's', 'table', 't', 'op', 'UPDATE', 'id', 7) WHERE payload = jsonb_build_object('id', 7);
