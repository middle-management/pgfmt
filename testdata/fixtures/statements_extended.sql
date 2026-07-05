-- SAVEPOINT
SAVEPOINT my_savepoint;

-- RELEASE SAVEPOINT
RELEASE SAVEPOINT my_savepoint;

-- ROLLBACK TO SAVEPOINT
ROLLBACK TO SAVEPOINT my_savepoint;

-- DROP TYPE
DROP TYPE IF EXISTS mood;

-- DROP TRIGGER
DROP TRIGGER IF EXISTS trg_audit ON users;

-- DROP TRIGGER on schema-qualified table
DROP TRIGGER IF EXISTS trigger_apply_resource_event ON studio.resource_event;

-- Operator subquery: > ALL
SELECT name FROM products WHERE price > ALL (SELECT price FROM products WHERE category = 'budget');

-- Operator subquery: = ANY
SELECT name FROM users WHERE id = ANY (SELECT user_id FROM admins);
