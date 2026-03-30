-- Header comment describing the file
SELECT 1;

-- Section: User queries
-- These queries handle user management
SELECT id, name FROM users WHERE active = true;

/* Block comment before a statement */
SELECT count(*) FROM orders;

-- Multiple single-line comments
-- grouped together
-- before a statement
INSERT INTO logs (message) VALUES ('hello');

SELECT a, b FROM t;

-- Trailing comment at end of file
