-- CASE WHEN expression
SELECT id, CASE WHEN status = 'active' THEN 'A' WHEN status = 'inactive' THEN 'I' ELSE 'U' END AS code FROM users;

-- Simple CASE expression
SELECT id, CASE status WHEN 'active' THEN 1 WHEN 'inactive' THEN 0 ELSE -1 END AS status_code FROM users;

-- GREATEST
SELECT GREATEST(a, b, c) AS max_val FROM numbers;

-- LEAST
SELECT LEAST(a, b, c) AS min_val FROM numbers;

-- CURRENT_TIMESTAMP and friends (SqlvalueFunction)
SELECT CURRENT_TIMESTAMP, CURRENT_DATE, CURRENT_USER, LOCALTIME, LOCALTIMESTAMP;

-- GROUPING function
SELECT department, GROUPING(department) AS grp FROM sales GROUP BY ROLLUP(department);

-- DEFAULT as value in INSERT
INSERT INTO config (key, value) VALUES ('k', DEFAULT);

-- DEFAULT as value in UPDATE
UPDATE config SET value = DEFAULT WHERE key = 'k';

-- SELECT DISTINCT (without ON)
SELECT DISTINCT status FROM users;
