-- UNION
SELECT
	id,
	name
FROM
	customers
UNION
SELECT
	id,
	name
FROM
	suppliers;

-- UNION ALL
SELECT
	id,
	email
FROM
	users
WHERE
	active = true
UNION ALL
SELECT
	id,
	email
FROM
	archived_users;

-- INTERSECT
SELECT
	user_id
FROM
	orders
WHERE
	total > 100
INTERSECT
SELECT
	user_id
FROM
	reviews
WHERE
	rating >= 4;

-- EXCEPT
SELECT
	id
FROM
	all_users
EXCEPT
SELECT
	user_id
FROM
	banned_users;

-- Combined set operations
(
	SELECT
		id,
		name
	FROM
		customers
	UNION ALL
	SELECT
		id,
		name
	FROM
		suppliers
)
EXCEPT
SELECT
	id,
	name
FROM
	blacklist
ORDER BY
	name;

-- Three-way UNION chain (should flatten, not nest)
SELECT
	'A'::text AS type,
	id
FROM
	t1
UNION
SELECT
	'B'::text,
	id
FROM
	t2
UNION
SELECT
	'C'::text,
	id
FROM
	t3;

