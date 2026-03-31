-- Simple UPDATE
UPDATE users
SET
	name = 'Alice'
WHERE
	id = 1;

-- UPDATE with multiple SET columns
UPDATE products
SET
	price = 19.99,
	updated_at = now()
WHERE
	category = 'sale';

-- UPDATE with FROM clause
UPDATE orders
SET
	status = 'shipped'
FROM
	shipments AS s
WHERE
	(orders.id = s.order_id)
AND s.shipped_at IS NOT NULL;

-- UPDATE with RETURNING
UPDATE accounts
SET
	balance = balance - 100
WHERE
	id = $1
RETURNING
	id, balance;

-- UPDATE with CTE
WITH 
expired AS (
	SELECT
		id
	FROM
		sessions
	WHERE
		expires_at < now()
)
UPDATE sessions
SET
	active = false
WHERE
	id IN (
	SELECT
		id
	FROM
		expired
)
RETURNING
	id;

-- Simple DELETE
DELETE FROM logs
WHERE
	created_at < '2024-01-01';

-- DELETE with USING
DELETE FROM order_items
USING
	orders
WHERE
	(order_items.order_id = orders.id)
AND (orders.status = 'cancelled');

-- DELETE with RETURNING
DELETE FROM tokens
WHERE
	expires_at < now()
RETURNING
	id, user_id;

-- DELETE with CTE
WITH 
old_users AS (
	SELECT
		id
	FROM
		users
	WHERE
		last_login < (now() - '1 year'::interval)
)
DELETE FROM sessions
WHERE
	user_id IN (
	SELECT
		id
	FROM
		old_users
)
RETURNING
	session_id;

