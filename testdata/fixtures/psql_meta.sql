--
-- PostgreSQL database dump
--

\restrict 8ZAeqrccOO62hdeNzGJzMqxWDtDbSPXT5CN36pPBxgJNe1DgROFI6MtVY9X6LwB

SET statement_timeout = 0;
SET client_encoding = 'UTF8';

\connect postgres

CREATE TABLE public.users (
    id integer NOT NULL,
    name text
);

-- a backslash line inside a string literal is not a meta-command
SELECT 'line one
\restrict not_a_meta_command
line two';

-- nor inside a dollar-quoted body
SELECT $body$
\restrict still_not_a_meta_command
$body$;

/*
\restrict inside a block comment is not a meta-command either
*/
SELECT 1;

\unrestrict 8ZAeqrccOO62hdeNzGJzMqxWDtDbSPXT5CN36pPBxgJNe1DgROFI6MtVY9X6LwB

--
-- PostgreSQL database dump complete
--
