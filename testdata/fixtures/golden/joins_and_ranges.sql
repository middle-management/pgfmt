-- INNER JOIN
SELECT
	u.id,
	o.total
FROM
	users AS u
	JOIN orders AS o ON u.id = o.user_id;

-- Multiple JOINs
SELECT
	u.name,
	o.id,
	p.name
FROM
	users AS u
	JOIN orders AS o ON o.user_id = u.id
	JOIN order_items AS oi ON oi.order_id = o.id
	JOIN products AS p ON p.id = oi.product_id;

-- LEFT/RIGHT/FULL OUTER JOIN
SELECT
	u.name,
	o.id
FROM
	users AS u
	LEFT JOIN orders AS o ON u.id = o.user_id;

SELECT
	u.name,
	o.id
FROM
	users AS u
	RIGHT JOIN orders AS o ON u.id = o.user_id;

SELECT
	a.id,
	b.id
FROM
	table_a AS a
	FULL JOIN table_b AS b ON a.key = b.key;

-- CROSS JOIN
SELECT
	u.name,
	r.role_name
FROM
	users AS u
	CROSS JOIN roles AS r;

-- NATURAL JOIN
SELECT
	*
FROM
	users NATURAL JOIN profiles;

-- Subselect in FROM (derived table)
SELECT
	sub.total_count
FROM
	(
		SELECT
			count(*) AS total_count
		FROM
			users
		WHERE
			active = true
	) sub;

-- LATERAL join
SELECT
	u.name,
	recent.title
FROM
	users AS u, LATERAL (
		SELECT
			title
		FROM
			posts AS p
		WHERE
			p.user_id = u.id
		ORDER BY
			created_at DESC
		LIMIT 3
	) recent;

-- Range function (generate_series)
SELECT
	d::date
FROM
	generate_series('2024-01-01'::date, '2024-12-31'::date, '1 month'::interval) d;

-- Range function with column definition
SELECT
	*
FROM
	json_to_recordset('[{"a":1,"b":"x"},{"a":2,"b":"y"}]') t;

