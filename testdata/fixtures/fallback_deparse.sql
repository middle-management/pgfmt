-- Statements that fall through to deparse fallback
DISCARD ALL;
LOCK TABLE users IN ACCESS EXCLUSIVE MODE;
REFRESH MATERIALIZED VIEW mv_stats;
