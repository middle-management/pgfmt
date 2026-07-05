-- Transaction modes, negative casts, char types, and operator precedence
SET TRANSACTION ISOLATION LEVEL SERIALIZABLE, READ ONLY, DEFERRABLE;

SET SESSION CHARACTERISTICS AS TRANSACTION READ WRITE;

START TRANSACTION READ ONLY;

BEGIN ISOLATION LEVEL REPEATABLE READ;

COMMIT AND CHAIN;

PREPARE TRANSACTION 'tx1';

COMMIT PREPARED 'tx1';

ROLLBACK PREPARED 'tx2';

SELECT
	(-9223372036854775808)::int8 * (-1)::int8;

SELECT
	'a'::char(1),
	'abc'::char(3),
	'x'::"char";

CREATE TABLE char_types (
	a char(1),
	b char(3),
	c "char"
);

SELECT
	'foo' SIMILAR TO 'f%';

SELECT
	'foo' SIMILAR TO 'f#%' ESCAPE '#';

SELECT
	('v' = ANY (proargmodes)) IS DISTINCT FROM (provariadic <> 0)
FROM
	pg_proc;

SELECT
	(a = b) <> (c IS NOT NULL)
FROM
	t;

CREATE FUNCTION low_level()
RETURNS int
LANGUAGE internal
STRICT
IMMUTABLE
AS $$
int8in
$$;

