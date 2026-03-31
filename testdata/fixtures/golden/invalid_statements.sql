-- SAVEPOINT
SAVEPOINT ;

-- RELEASE SAVEPOINT
RELEASE SAVEPOINT ;

-- ROLLBACK TO SAVEPOINT
ROLLBACK TO SAVEPOINT ;

-- DROP TYPE
DROP TYPE IF EXISTS ;

-- DROP TRIGGER
DROP TRIGGER IF EXISTS users.trg_audit;

-- Operator subquery: > ALL
SELECT
	name
FROM
	products
WHERE
	price string:{sval:">"} ALL (
		SELECT
			price
		FROM
			products
		WHERE
			category = 'budget'
	);

-- Operator subquery: = ANY
SELECT
	name
FROM
	users
WHERE
	id string:{sval:"="} ANY (
		SELECT
			user_id
		FROM
			admins
	);

