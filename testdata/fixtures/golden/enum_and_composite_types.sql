CREATE TYPE mood AS ENUM (
	'happy',
	'sad',
	'angry',
	'neutral'
);

CREATE TYPE inventory_item AS (
	name text,
	supplier_id int,
	price numeric
);

CREATE TYPE bug_status AS ENUM (
	'open',
	'closed',
	'in_progress',
	'wont_fix',
	'duplicate'
);

CREATE TYPE address AS (
	street text,
	city text,
	state text,
	zip_code text,
	country text
);

CREATE TYPE color AS ENUM (
	'red',
	'green',
	'blue'
);

