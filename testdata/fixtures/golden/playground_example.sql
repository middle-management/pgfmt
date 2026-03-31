SELECT
	u.id,
	u.name,
	u.email,
	count(o.id) AS order_count,
	sum(o.total) AS total_spent
FROM
	users AS u
	LEFT JOIN orders AS o ON o.user_id = u.id
WHERE
	(u.created_at >= '2024-01-01')
	AND (u.status = 'active')
GROUP BY
	u.id, u.name, u.email
HAVING
	count(o.id) > 0
ORDER BY
	total_spent DESC
LIMIT 50;

CREATE TABLE IF NOT EXISTS products (
	id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	name text NOT NULL,
	description text,
	price numeric(10, 2) NOT NULL CHECK (price > 0),
	category_id int REFERENCES categories (id) ON DELETE SET NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

WITH 
monthly_revenue AS (
	SELECT
		date_trunc('month', o.created_at) AS month,
		sum(o.total) AS revenue
	FROM
		orders AS o
	WHERE
		o.created_at >= (now() - '12 months'::interval)
	GROUP BY
		1
)
SELECT
	month,
	revenue,
	revenue - lag(revenue) OVER (ORDER BY month) AS change
FROM
	monthly_revenue
ORDER BY
	month;

