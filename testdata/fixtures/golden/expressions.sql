-- COALESCE
SELECT
	COALESCE(nickname, name, 'Anonymous') AS display_name
FROM
	users;

-- IS NULL / IS NOT NULL
SELECT
	id
FROM
	users
WHERE
	email IS NOT NULL
	AND phone IS NULL;

-- Type casts
SELECT
	id::text,
	created_at::date,
	'100'::int
FROM
	users;

-- Boolean expression nesting
SELECT
	id
FROM
	users
WHERE
	(
		(active = true)
		AND (verified = true)
	)
	OR (
		(role = 'admin')
		AND (created_at < '2024-01-01')
	);

-- BETWEEN
SELECT
	id
FROM
	events
WHERE
	created_at BETWEEN '2024-01-01' AND '2024-12-31';

-- LIKE / ILIKE
SELECT
	name
FROM
	users
WHERE
	(name LIKE '%alice%')
	OR (email ILIKE '%@example.com');

-- DISTINCT ON
SELECT DISTINCT ON (user_id)
	user_id,
	created_at,
	message
FROM
	notifications
ORDER BY
	user_id, created_at DESC;

-- LIMIT and OFFSET
SELECT
	id,
	name
FROM
	users
ORDER BY
	created_at DESC
LIMIT 10
OFFSET 20;

