-- Aggregate ORDER BY
SELECT string_agg(name, ', ' ORDER BY name ASC) AS names FROM users;

-- Aggregate FILTER
SELECT count(*) FILTER (WHERE status = 'active') AS active_count, count(*) FILTER (WHERE status = 'inactive') AS inactive_count FROM users;

-- WITHIN GROUP (ordered-set aggregate)
SELECT department, percentile_cont(0.5) WITHIN GROUP (ORDER BY salary DESC) AS median_salary FROM employees GROUP BY department;

-- Window function with OVER
SELECT id, name, salary, rank() OVER (PARTITION BY department ORDER BY salary DESC) AS dept_rank FROM employees;

-- Named window reference
SELECT id, sum(amount) OVER w, avg(amount) OVER w FROM orders WINDOW w AS (PARTITION BY customer_id ORDER BY created_at);

-- Window with frame clause (ROWS BETWEEN)
SELECT id, amount, sum(amount) OVER (ORDER BY id ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS rolling_sum FROM orders;

-- Window with RANGE and EXCLUDE
SELECT id, ts, count(*) OVER (ORDER BY ts RANGE BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW EXCLUDE TIES) AS running_count FROM events;

-- GROUP BY DISTINCT
SELECT x, count(*) FROM t GROUP BY DISTINCT x;

-- FOR UPDATE
SELECT id, name FROM users WHERE id = $1 FOR UPDATE;

-- FOR SHARE with table and NOWAIT
SELECT u.id, u.name FROM users u JOIN accounts a ON u.id = a.user_id FOR SHARE OF u NOWAIT;

-- FOR UPDATE SKIP LOCKED (queue pattern)
SELECT id, payload FROM job_queue WHERE status = 'pending' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED;

-- FOR KEY SHARE
SELECT id FROM parent_table WHERE id = $1 FOR KEY SHARE;

-- Combined: aggregate with ORDER BY, FILTER, and window OVER
SELECT department, json_agg(row_to_json(e) ORDER BY e.name) FILTER (WHERE e.active = true) AS active_employees, sum(e.salary) OVER (PARTITION BY department) AS dept_total FROM employees e GROUP BY department, e.salary;
