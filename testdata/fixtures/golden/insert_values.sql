-- Multi-row INSERT with ON CONFLICT DO UPDATE
INSERT INTO
	studio.permission (type, description, metadata, created_at)
VALUES
	('organisation:r', 'List and view organisations', '{}', now()),
	('organisation:w', 'Create, edit or delete organisations', '{}', now()),
	('group:r', 'List groups in the organisation', '{}', now())
ON CONFLICT (type)
DO UPDATE SET
	description = excluded.description,
	metadata = excluded.metadata;

-- Single-row INSERT stays on one line
INSERT INTO
	settings (key, value)
VALUES ('theme', 'dark');

-- Multi-row INSERT without ON CONFLICT
INSERT INTO
	tags (id, name)
VALUES
	(1, 'red'),
	(2, 'green'),
	(3, 'blue');

-- Multi-row VALUES inside a CTE
WITH 
inserted AS (
	INSERT INTO
		t (a, b)
	VALUES
		(1, 2),
		(3, 4)
	RETURNING
		a
)
SELECT
	a
FROM
	inserted;

-- Standalone VALUES statement
VALUES
	(1, 'one'),
	(2, 'two');

-- ON CONFLICT DO UPDATE with WHERE clause
INSERT INTO
	counters (name, hits)
VALUES ('home', 1)
ON CONFLICT (name)
DO UPDATE SET
	hits = counters.hits + 1
WHERE
	counters.hits < 100;

