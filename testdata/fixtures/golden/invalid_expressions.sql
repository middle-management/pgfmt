-- CASE WHEN expression
SELECT
	id,
	 AS code
FROM
	users;

-- Simple CASE expression
SELECT
	id,
	 AS status_code
FROM
	users;

-- GREATEST
SELECT
	 AS max_val
FROM
	numbers;

-- LEAST
SELECT
	 AS min_val
FROM
	numbers;

-- CURRENT_TIMESTAMP and friends (SqlvalueFunction)
SELECT
	,
	,
	,
	,
	;

-- GROUPING function
SELECT
	department,
	 AS grp
FROM
	sales
GROUP BY
	;

-- DEFAULT as value in INSERT
INSERT INTO
	config (key, value)
VALUES ('k', );

-- DEFAULT as value in UPDATE
UPDATE config
SET
	value = 
WHERE
	key = 'k';

-- SELECT DISTINCT (without ON)
SELECT DISTINCT ON ()
	status
FROM
	users;

