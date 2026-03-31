-- EXISTS subquery
SELECT id, name FROM users u WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id);

-- NOT EXISTS subquery
SELECT id, name FROM users u WHERE NOT EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id);

-- IN subquery
SELECT name FROM products WHERE category_id IN (SELECT id FROM categories WHERE active = true);

-- Scalar subquery in SELECT list
SELECT id, name, (SELECT count(*) FROM orders o WHERE o.user_id = u.id) AS order_count FROM users u;

-- CTE with multiple common table expressions
WITH active_users AS (SELECT id, name FROM users WHERE active = true), user_orders AS (SELECT user_id, count(*) AS cnt FROM orders GROUP BY user_id)
SELECT au.name, COALESCE(uo.cnt, 0) AS order_count FROM active_users au LEFT JOIN user_orders uo ON au.id = uo.user_id;

-- Recursive CTE
WITH RECURSIVE tree AS (SELECT id, name, parent_id, 0 AS depth FROM categories WHERE parent_id IS NULL UNION ALL SELECT c.id, c.name, c.parent_id, t.depth + 1 FROM categories c JOIN tree t ON c.parent_id = t.id)
SELECT id, name, depth FROM tree ORDER BY depth, name;
