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

-- SqlvalueFunction with precision
SELECT CURRENT_TIMESTAMP(3), CURRENT_TIME(0), LOCALTIME(6), LOCALTIMESTAMP(2);

-- GROUPING function
SELECT department, GROUPING(department) AS grp FROM sales GROUP BY ROLLUP(department);

-- DEFAULT as value in INSERT
INSERT INTO config (key, value) VALUES ('k', DEFAULT);

-- DEFAULT as value in UPDATE
UPDATE config SET value = DEFAULT WHERE key = 'k';

-- SELECT DISTINCT (without ON)
SELECT DISTINCT status FROM users;

-- IN with literal list
SELECT 1 WHERE x IN (1, 2, 3);

-- NOT IN with literal list
SELECT 1 WHERE x NOT IN (1, 2, 3);

-- NOT BETWEEN
SELECT 1 WHERE x NOT BETWEEN 1 AND 10;

-- NULLIF
SELECT NULLIF(a, b);

-- IS DISTINCT FROM
SELECT 1 WHERE a IS DISTINCT FROM b;

-- ARRAY expression
SELECT ARRAY[1, 2, 3];

-- Row field indirection
SELECT (my_row).field_name;

-- Array subscript
SELECT arr[1];

-- ROW expression
SELECT ROW(1, 2, 3);
