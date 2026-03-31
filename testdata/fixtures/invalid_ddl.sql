-- CREATE VIEW
CREATE VIEW active_users AS SELECT id, name FROM users WHERE active = true;

-- CREATE OR REPLACE VIEW
CREATE OR REPLACE VIEW user_summary AS SELECT u.id, u.name, count(o.id) AS order_count FROM users u LEFT JOIN orders o ON o.user_id = u.id GROUP BY u.id, u.name;

-- CREATE MATERIALIZED VIEW
CREATE MATERIALIZED VIEW monthly_stats AS SELECT date_trunc('month', created_at) AS month, count(*) AS total FROM events GROUP BY 1;

-- CREATE SCHEMA
CREATE SCHEMA IF NOT EXISTS reporting;

-- CREATE SEQUENCE
CREATE SEQUENCE order_id_seq START WITH 1 INCREMENT BY 1;

-- CREATE EXTENSION
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- GRANT
GRANT SELECT, INSERT ON users TO readonly_role;

-- REVOKE
REVOKE DELETE ON users FROM readonly_role;

-- COMMENT ON
COMMENT ON TABLE users IS 'Main user accounts table';
COMMENT ON COLUMN users.email IS 'Primary email address';

-- TRUNCATE
TRUNCATE TABLE logs;
TRUNCATE TABLE sessions, tokens RESTART IDENTITY CASCADE;

-- EXPLAIN
EXPLAIN SELECT * FROM users WHERE id = 1;
EXPLAIN ANALYZE SELECT * FROM users WHERE active = true;

-- COPY
COPY users (id, name, email) TO STDOUT WITH (FORMAT csv, HEADER true);

-- LISTEN / NOTIFY
LISTEN channel_updates;
NOTIFY channel_updates, 'payload data';

-- SET / SHOW / RESET
SET search_path TO public, staging;
SHOW search_path;
RESET search_path;

-- PREPARE / EXECUTE / DEALLOCATE
PREPARE user_lookup (integer) AS SELECT id, name FROM users WHERE id = $1;
EXECUTE user_lookup(42);
DEALLOCATE user_lookup;

-- VACUUM / ANALYZE
VACUUM users;
ANALYZE users;
