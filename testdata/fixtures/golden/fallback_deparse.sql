-- Statements that fall through to deparse fallback
DISCARD ALL;

LOCK TABLE users;

REFRESH MATERIALIZED VIEW mv_stats;

