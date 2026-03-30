-- Header comment describing the file
SELECT
	1;

-- Section: User queries
-- These queries handle user management
SELECT
	id,
	name
FROM
	users
WHERE
	active = true;

/* Block comment before a statement */
SELECT
	count(*)
FROM
	orders;

-- Multiple single-line comments
-- grouped together
-- before a statement
INSERT INTO
	logs (message)
VALUES ('hello');

SELECT
	a,
	b
FROM
	t;

-- Inline comments in SELECT list
SELECT
	id, -- primary key
	name, -- user's display name
	email, -- contact address
	created_at
FROM
	users;

-- Multiple inline comments between items
SELECT
	a, -- group B starts here
	-- important columns
	b,
	c
FROM
	t;

-- Block comment inline
SELECT /* all columns */
	*
FROM
	t;

-- Trailing comment at end of file
