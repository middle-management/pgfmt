CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_event_index_unique ON event  USING btree (index ) ;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_index_type ON event  USING btree (index ASC , type ) ;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_created_at ON event  USING btree (created_at ASC ) ;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_data_trgm ON event  USING gin (ext.gin_trgm_ops ) ;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_type_trgm ON event  USING gin (type ext.gin_trgm_ops ) ;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_created_by_trgm ON event  USING gin (ext.gin_trgm_ops ) ;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_event_reactor_reactor ON event_reactor  USING btree (reactor ) ;

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
	i  ;

;

WITH 
new_event AS (

)
SELECT
	* 
FROM
	 ORDER BY evtindex  ;

SELECT
	* 
FROM
	event AS evt 
WHERE
	((evtindex)>=(lower(pggen.arg(range)::"int8range"))) AND (() OR ((evtindex)<(upper(pggen.arg(range))))) AND (() OR ()) ORDER BY evtindex  ;

SELECT
	min(objid) 
FROM
	pg_locks  
WHERE
	(locktype)=(advisory) 
GROUP BY
	locktype ;

