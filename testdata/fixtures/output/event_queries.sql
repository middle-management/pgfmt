-- name: CreateIndexUnique :exec
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_event_index_unique ON event USING btree (index);

-- name: CreateIndexType :exec
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_index_type ON event USING btree (index ASC, type);

-- name: CreateIndexCreated :exec
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_created_at ON event USING btree (created_at ASC);

-- name: CreateGinIndexData :exec
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_data_trgm ON event USING gin ((data::text) ext.gin_trgm_ops);

-- name: CreateGinIndexType :exec
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_type_trgm ON event USING gin (type ext.gin_trgm_ops);

-- name: CreateGinIndexCreatedBy :exec
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_created_by_trgm ON event USING gin ((created_by::text) ext.gin_trgm_ops);

-- name: CreateIndexReactor :exec
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_reactor_reactor ON event_reactor USING btree (reactor);

-- name: TakeTransactionEventLock :exec
WITH 
i AS (
	SELECT
		max(index) AS idx
	FROM
		event
)
SELECT
	pg_advisory_xact_lock_shared(COALESCE(idx, 0))
FROM
	i;

-- name: InsertEvent :exec
INSERT INTO
	event (id, type, data, created_at, created_by)
VALUES (pggen.arg('id'), pggen.arg('type'), pggen.arg('data'), pggen.arg('createdAt'), pggen.arg('createdBy'));

-- name: LoadReactorEvents :many
WITH 
new_event AS (
	INSERT INTO
		event_reactor (event_id, reactor)
	SELECT
		id,
		pggen.arg('reactor')::text
	FROM
		event AS e
	WHERE
		(NOT e.id IN (
			SELECT
				event_id
			FROM
				event_reactor AS r
			WHERE
				r.reactor = pggen.arg('reactor')::text
		))
		AND (e.index >= lower(pggen.arg('range')::int8range))
		AND (
			upper(pggen.arg('range')) IS NULL
			OR (e.index < upper(pggen.arg('range')))
		)
		AND (
			(e.type = ANY (pggen.arg('types')))
			OR pggen.arg('types') IS NULL
		)
	ORDER BY
		index
	ON CONFLICT (event_id, reactor) DO NOTHING
	RETURNING
		event_id
)
SELECT
	*
FROM
	event AS evt
	JOIN new_event ON id = event_id
ORDER BY
	evt.index;

-- name: LoadEvents :many
SELECT
	*
FROM
	event AS evt
WHERE
	(evt.index >= lower(pggen.arg('range')::int8range))
	AND (
		upper(pggen.arg('range')) IS NULL
		OR (evt.index < upper(pggen.arg('range')))
	)
	AND (
		(evt.type = ANY (pggen.arg('types')))
		OR pggen.arg('types') IS NULL
	)
ORDER BY
	evt.index;

-- name: SelectMinTransactionEventLock :one
SELECT
	min(objid)
FROM
	pg_locks
WHERE
	locktype = 'advisory'
GROUP BY
	locktype;

