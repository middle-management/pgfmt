BEGIN;

DROP TABLE IF EXISTS p_pbx_call_profile_mex_accept_call;

DROP TABLE IF EXISTS p_pbx_call_profile_route_accept_call;

DROP TABLE IF EXISTS p_pbx_route_user;

DROP TABLE IF EXISTS p_pbx_route_schedule;

DROP TABLE IF EXISTS p_pbx_route_queue;

DROP TABLE IF EXISTS p_pbx_route_menu;

DROP TABLE IF EXISTS p_pbx_voicemail_user_recording_status;

DROP TABLE IF EXISTS p_pbx_voicemail_recording_alternative;

DROP TABLE IF EXISTS p_pbx_voicemail_recording;

DROP TABLE IF EXISTS p_pbx_voicemail_user;

DROP TABLE IF EXISTS p_pbx_route;

DROP TABLE IF EXISTS p_pbx_prompt_recording_alternative;

DROP TABLE IF EXISTS p_pbx_prompt_callback;

DROP TABLE IF EXISTS p_pbx_prompt_recording;

DROP TABLE IF EXISTS p_pbx_voicemail;

DROP TABLE IF EXISTS p_pbx_prompt;

DROP TABLE IF EXISTS p_pbx_customer;

DROP TABLE IF EXISTS p_pbx_user;

DROP TABLE IF EXISTS p_pbx_call_profile;

DROP INDEX IF EXISTS idx_pbx_call_transfer_list;

CREATE TABLE p_pbx_call_profile (
	pbx_call_profile_id uuid PRIMARY KEY,
	desktop_call_as_msisdn varchar,
	mobile_call_as_msisdn varchar,
	mex_call_as_msisdn varchar,
	mex_call_as varchar,
	available_by_default boolean NOT NULL DEFAULT true,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	deleted_at timestamp with time zone
);

CREATE TABLE p_pbx_call_profile_mex_accept_call (
	pbx_call_profile_id uuid NOT NULL REFERENCES p_pbx_call_profile ON DELETE CASCADE,
	subscription_id uuid,
	accept_calls boolean NOT NULL DEFAULT true,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	PRIMARY KEY (pbx_call_profile_id, subscription_id)
);

CREATE TABLE p_pbx_user (
	pbx_user_id uuid PRIMARY KEY,
	name varchar,
	msisdn varchar,
	reason varchar,
	level varchar,
	available_at timestamp with time zone,
	organisation_id uuid NOT NULL,
	call_profile_available_id uuid REFERENCES p_pbx_call_profile ON DELETE SET NULL,
	call_profile_unavailable_id uuid REFERENCES p_pbx_call_profile ON DELETE SET NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	deleted_at timestamp with time zone
);

CREATE UNIQUE INDEX ON p_pbx_user USING btree (msisdn, organisation_id) WHERE deleted_at IS NULL;

CREATE TABLE p_pbx_prompt (
	pbx_prompt_id uuid PRIMARY KEY,
	description varchar,
	extension varchar,
	organisation_id uuid,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	deleted_at timestamp with time zone
);

CREATE TABLE p_pbx_voicemail (
	pbx_voicemail_id uuid PRIMARY KEY,
	name varchar,
	pin varchar,
	extension varchar,
	msisdn varchar,
	pbx_prompt_id uuid REFERENCES p_pbx_prompt ON DELETE SET NULL,
	organisation_id uuid NOT NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	deleted_at timestamp with time zone
);

CREATE TABLE p_pbx_route (
	pbx_route_id uuid PRIMARY KEY,
	type varchar NOT NULL, -- ivr, prompt, schedule, group, queue, voicemail, forwarding
	extension varchar, -- external if it starts with a + (pattern: /^\+?[0-9]+$/)
	name varchar,
	suffix varchar,
	call_display varchar, -- "next" is added below in an ALTER TABLE because circular references
	pbx_user_id uuid REFERENCES p_pbx_user ON DELETE SET NULL,
	pbx_prompt_id uuid REFERENCES p_pbx_prompt ON DELETE SET NULL,
	pbx_voicemail_id uuid REFERENCES p_pbx_voicemail ON DELETE SET NULL,
	organisation_id uuid,
	subscription_id uuid,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL
);

CREATE INDEX ON p_pbx_route USING btree (organisation_id);

-- must be added after unique index or we get
-- errors about it missing
ALTER TABLE p_pbx_route
	ADD COLUMN next uuid REFERENCES p_pbx_route (pbx_route_id) ON DELETE SET NULL;

CREATE TABLE p_pbx_call_profile_route_accept_call (
	pbx_call_profile_id uuid NOT NULL REFERENCES p_pbx_call_profile ON DELETE CASCADE,
	pbx_route_id uuid NOT NULL REFERENCES p_pbx_route ON DELETE CASCADE,
	accept_calls boolean NOT NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	PRIMARY KEY (pbx_call_profile_id, pbx_route_id)
);

CREATE TABLE p_pbx_route_schedule (
	pbx_schedule_id uuid PRIMARY KEY,
	pbx_route_id uuid NOT NULL REFERENCES p_pbx_route ON DELETE CASCADE,
	name varchar,
	type varchar NOT NULL, -- weekly, daily, monthly, yearly, other
	index int NOT NULL, -- sequential number per route (0 = "default", highest is "first")
	recurrence_mon bool,
	recurrence_tue bool,
	recurrence_wed bool,
	recurrence_thu bool,
	recurrence_fri bool,
	recurrence_sat bool,
	recurrence_sun bool,
	next uuid REFERENCES p_pbx_route (pbx_route_id) ON DELETE SET NULL,
	start_time time with time zone,
	start_date date,
	end_time time with time zone,
	end_date date,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL
);

CREATE INDEX ON p_pbx_route_schedule USING btree (pbx_route_id);

CREATE TABLE p_pbx_route_queue (
	pbx_route_id uuid PRIMARY KEY REFERENCES p_pbx_route ON DELETE CASCADE,
	max_waiting_callers int NOT NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL
);

CREATE TABLE p_pbx_route_menu (
	pbx_route_id uuid NOT NULL REFERENCES p_pbx_route ON DELETE CASCADE,
	type varchar NOT NULL, -- 1,2,3,4,5,6,7,8,9,0,#,*
	next uuid REFERENCES p_pbx_route (pbx_route_id) ON DELETE SET NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	PRIMARY KEY (pbx_route_id, type)
);

CREATE INDEX ON p_pbx_route_menu USING btree (pbx_route_id);

CREATE TABLE p_pbx_route_user (
	pbx_user_id uuid NOT NULL REFERENCES p_pbx_user ON DELETE CASCADE,
	pbx_route_id uuid NOT NULL REFERENCES p_pbx_route ON DELETE CASCADE,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	connected boolean NOT NULL,
	PRIMARY KEY (pbx_user_id, pbx_route_id)
);

CREATE INDEX ON p_pbx_route_user USING btree (pbx_route_id);

CREATE TABLE p_pbx_voicemail_user (
	pbx_voicemail_id uuid NOT NULL REFERENCES p_pbx_voicemail ON DELETE CASCADE,
	pbx_user_id uuid NOT NULL REFERENCES p_pbx_user ON DELETE CASCADE,
	notify_sms boolean NOT NULL DEFAULT false,
	notify_email boolean NOT NULL DEFAULT false,
	email varchar,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	PRIMARY KEY (pbx_user_id, pbx_voicemail_id)
);

CREATE TABLE p_pbx_prompt_recording (
	pbx_prompt_recording_id uuid PRIMARY KEY,
	pbx_prompt_id uuid NOT NULL REFERENCES p_pbx_prompt ON DELETE CASCADE,
	url varchar NOT NULL,
	content_type varchar,
	language varchar,
	duration bigint,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL
);

CREATE TABLE p_pbx_prompt_recording_alternative (
	pbx_prompt_recording_id uuid REFERENCES p_pbx_prompt_recording ON DELETE CASCADE,
	url varchar NOT NULL,
	content_type varchar NOT NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	PRIMARY KEY (pbx_prompt_recording_id, content_type)
);

CREATE TABLE p_pbx_prompt_callback (
	pbx_prompt_callback_id uuid PRIMARY KEY,
	pbx_prompt_id uuid NOT NULL REFERENCES p_pbx_prompt ON DELETE CASCADE,
	status varchar NOT NULL,
	language varchar NOT NULL,
	msisdn varchar NOT NULL,
	recording_id varchar,
	service_id varchar,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL
);

CREATE TABLE p_pbx_voicemail_recording (
	pbx_voicemail_recording_id uuid PRIMARY KEY,
	pbx_voicemail_id uuid NOT NULL REFERENCES p_pbx_voicemail ON DELETE CASCADE,
	url varchar NOT NULL, -- to .wav on s3
	content_type varchar,
	msisdn varchar, -- called phone number
	duration bigint,
	label varchar,
	recorded_at timestamp with time zone NOT NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL
);

CREATE TABLE p_pbx_voicemail_recording_alternative (
	pbx_voicemail_recording_id uuid REFERENCES p_pbx_voicemail_recording ON DELETE CASCADE,
	url varchar NOT NULL,
	content_type varchar NOT NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	PRIMARY KEY (pbx_voicemail_recording_id, content_type)
);

CREATE TABLE p_pbx_voicemail_user_recording_status (
	pbx_voicemail_recording_id uuid NOT NULL REFERENCES p_pbx_voicemail_recording ON DELETE CASCADE,
	pbx_user_id uuid NOT NULL REFERENCES p_pbx_user ON DELETE CASCADE,
	read boolean NOT NULL,
	created_at timestamp with time zone NOT NULL,
	updated_at timestamp with time zone NOT NULL,
	PRIMARY KEY (pbx_voicemail_recording_id, pbx_user_id)
);

CREATE TABLE p_pbx_customer (
	customer_id uuid PRIMARY KEY,
	organisation_id uuid,
	subscription_id uuid,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX ON p_pbx_customer USING btree (organisation_id);

-- pbx_route_select selects all fields that we want for a pbx route
CREATE OR REPLACE FUNCTION pbx_route_select(route_id uuid[])
RETURNS TABLE (
	pbx_route_id uuid,
	type varchar,
	name varchar,
	next uuid,
	extension varchar,
	pbx_user_id uuid,
	pbx_prompt_id uuid,
	pbx_voicemail_id uuid,
	users uuid[],
	connectedusers uuid[],
	disconnectedusers uuid[],
	menu pg_catalog.json,
	schedule pg_catalog.json,
	queue pg_catalog.json,
	suffix varchar,
	call_display varchar,
	subscription_id uuid,
	organisation_id uuid,
	created_at timestamptz,
	updated_at timestamptz
)
AS $$
	SELECT
		r.pbx_route_id,
		r.type,
		COALESCE(r.name, '') AS name,
		r.next,
		r.extension,
		r.pbx_user_id,
		r.pbx_prompt_id,
		r.pbx_voicemail_id,
		array_agg(u.pbx_user_id) FILTER (WHERE u.pbx_route_id IS NOT NULL) AS users,
		array_agg(u.pbx_user_id) FILTER (WHERE u.pbx_route_id IS NOT NULL
		AND (u.connected = true)) AS connectedusers,
		array_agg(u.pbx_user_id) FILTER (WHERE u.pbx_route_id IS NOT NULL
		AND (u.connected = false)) AS disconnectedusers,
		json_object_agg(m.type, m.next) FILTER (WHERE m.pbx_route_id IS NOT NULL) AS menu,
		json_agg(row_to_json(i) ORDER BY i.index) FILTER (WHERE i.pbx_route_id IS NOT NULL) AS schedule,
		(
			SELECT
				row_to_json(q)
			FROM
				p_pbx_route_queue AS q
			WHERE
				r.pbx_route_id = q.pbx_route_id
		) AS queue,
		COALESCE(r.suffix, '') AS suffix,
		COALESCE(r.call_display, 'a') AS call_display,
		r.subscription_id,
		r.organisation_id,
		r.created_at,
		r.updated_at
	FROM
		p_pbx_route AS r
		LEFT JOIN p_pbx_route_user AS u USING (pbx_route_id)
		LEFT JOIN p_pbx_route_menu AS m USING (pbx_route_id)
		LEFT JOIN p_pbx_route_schedule AS i USING (pbx_route_id)
	WHERE
		r.pbx_route_id = ANY (route_id)
	GROUP BY
		r.pbx_route_id
$$
LANGUAGE sql;

CREATE OR REPLACE FUNCTION pbx_prompt_select(prompt_ids uuid[], contenttype varchar)
RETURNS TABLE (
	pbx_prompt_id uuid,
	description varchar,
	extension varchar,
	recording jsonb,
	modifiable bool,
	organisation_id uuid,
	created_at timestamptz,
	updated_at timestamptz
)
AS $$
	SELECT
		p.pbx_prompt_id,
		COALESCE(p.description, ''),
		COALESCE(p.extension, ''),
		jsonb_agg(row_to_json(r)) FILTER (WHERE r.pbx_prompt_id IS NOT NULL) AS recordings,
		p.organisation_id IS NOT NULL,
		p.organisation_id,
		p.created_at,
		p.updated_at
	FROM
		p_pbx_prompt AS p
		LEFT JOIN LATERAL (
			SELECT
				r.pbx_prompt_recording_id,
				r.pbx_prompt_id,
				COALESCE(a.content_type, r.content_type) AS content_type,
				COALESCE(a.url, r.url) AS url,
				r.duration,
				r.language,
				r.created_at,
				r.updated_at
			FROM
				p_pbx_prompt_recording AS r
				LEFT JOIN p_pbx_prompt_recording_alternative AS a ON (a.pbx_prompt_recording_id = r.pbx_prompt_recording_id)
			AND (a.content_type = contenttype)
			WHERE
				(
					contenttype IS NULL
					OR (r.content_type = contenttype)
					OR (a.content_type = contenttype)
				)
				AND (r.pbx_prompt_id = p.pbx_prompt_id)
		) r ON true
	WHERE
		p.pbx_prompt_id = ANY (prompt_ids)
	GROUP BY
		p.pbx_prompt_id
$$
LANGUAGE sql;

CREATE OR REPLACE FUNCTION pbx_voicemail_recording_select(
	voicemail_recording_ids uuid[],
	contenttype varchar
)
RETURNS TABLE (
	pbx_voicemail_recording_id uuid,
	url varchar,
	content_type varchar,
	msisdn varchar,
	duration bigint,
	read bool,
	pbx_voicemail_id uuid,
	recorded_at timestamptz,
	created_at timestamptz,
	updated_at timestamptz
)
AS $$
	SELECT
		r.pbx_voicemail_recording_id,
		COALESCE(a.url, r.url, '') AS url,
		COALESCE(a.content_type, r.content_type, 'audio/wav') AS content_type,
		COALESCE(r.msisdn, '') AS msisdn,
		r.duration,
		false AS read,
		r.pbx_voicemail_id,
		r.recorded_at,
		r.created_at,
		r.updated_at
	FROM
		p_pbx_voicemail_recording AS r
		LEFT JOIN p_pbx_voicemail_recording_alternative AS a ON (r.pbx_voicemail_recording_id = a.pbx_voicemail_recording_id)
	AND (a.content_type = contenttype)
	WHERE
		(r.pbx_voicemail_recording_id = ANY (voicemail_recording_ids))
		AND (
			contenttype IS NULL
			OR (r.content_type = contenttype)
			OR (a.content_type = contenttype)
		)
$$
LANGUAGE sql;

CREATE OR REPLACE FUNCTION pbx_voicemail_recording_with_read_select(
	voicemail_recording_ids uuid[],
	contenttype varchar,
	usermsisdn varchar
)
RETURNS TABLE (
	pbx_voicemail_recording_id uuid,
	url varchar,
	content_type varchar,
	msisdn varchar,
	duration bigint,
	read bool,
	pbx_voicemail_id uuid,
	recorded_at timestamptz,
	created_at timestamptz,
	updated_at timestamptz
)
AS $$
	SELECT
		r.pbx_voicemail_recording_id,
		COALESCE(a.url, r.url, '') AS url,
		COALESCE(a.content_type, r.content_type, '') AS content_type,
		COALESCE(r.msisdn, '') AS msisdn,
		r.duration,
		COALESCE(s.read, false) AS read,
		r.pbx_voicemail_id,
		r.recorded_at,
		r.created_at,
		r.updated_at
	FROM
		p_pbx_voicemail_recording AS r
		JOIN p_pbx_voicemail_user AS vu USING (pbx_voicemail_id)
		JOIN p_pbx_user AS u USING (pbx_user_id)
		LEFT JOIN p_pbx_voicemail_user_recording_status AS s USING (pbx_voicemail_recording_id, pbx_user_id)
		LEFT JOIN p_pbx_voicemail_recording_alternative AS a ON (r.pbx_voicemail_recording_id = a.pbx_voicemail_recording_id)
	AND (a.content_type = contenttype)
	WHERE
		(r.pbx_voicemail_recording_id = ANY (voicemail_recording_ids))
		AND (u.msisdn = usermsisdn)
		AND (
			contenttype::text IS NULL
			OR (r.content_type = contenttype)
			OR (a.content_type = contenttype)
		)
$$
LANGUAGE sql;

CREATE OR REPLACE FUNCTION pbx_voicemail_select(voicemail_ids uuid[])
RETURNS TABLE (
	pbx_voicemail_id uuid,
	name varchar,
	pin varchar,
	msisdn varchar,
	pbx_prompt_id uuid,
	organisation_id uuid,
	users jsonb,
	created_at timestamptz,
	updated_at timestamptz
)
AS $$
	SELECT
		v.pbx_voicemail_id,
		COALESCE(v.name, ''),
		COALESCE(v.pin, ''),
		COALESCE(v.msisdn, ''),
		v.pbx_prompt_id,
		v.organisation_id,
		jsonb_agg(row_to_json(u)) FILTER (WHERE u.pbx_voicemail_id IS NOT NULL) AS users,
		v.created_at,
		v.updated_at
	FROM
		p_pbx_voicemail AS v
		LEFT JOIN p_pbx_voicemail_user AS u USING (pbx_voicemail_id)
	WHERE
		v.pbx_voicemail_id = ANY (voicemail_ids)
	GROUP BY
		v.pbx_voicemail_id
$$
LANGUAGE sql;

COMMIT;

DROP FUNCTION IF EXISTS pbx_callprofile_select(uuid[]);

-- if return type is changed
CREATE OR REPLACE FUNCTION pbx_callprofile_select(call_profile_ids uuid[]) -- old user per route settings connected/disconnected
RETURNS TABLE (
	pbx_call_profile_id uuid,
	pbx_user_id uuid,
	desktop_call_as_msisdn varchar,
	mobile_call_as_msisdn varchar,
	mex_call_as_msisdn varchar,
	mex_call_as varchar,
	available_by_default bool,
	mex_accept_call pg_catalog.json,
	route_accept_call pg_catalog.json,
	route_user pg_catalog.json,
	created_at timestamptz,
	updated_at timestamptz,
	deleted_at timestamptz
)
AS $$
	SELECT
		cp.pbx_call_profile_id,
		u.pbx_user_id,
		desktop_call_as_msisdn,
		mobile_call_as_msisdn,
		mex_call_as_msisdn,
		mex_call_as,
		available_by_default,
		json_agg(row_to_json(ma) ORDER BY ma.subscription_id) FILTER (WHERE ma.subscription_id IS NOT NULL) AS mex_accept_call,
		json_agg(row_to_json(ra) ORDER BY ra.pbx_route_id) FILTER (WHERE ra.pbx_route_id IS NOT NULL) AS route_accept_call,
		json_agg(row_to_json(ru) ORDER BY ru.pbx_route_id) FILTER (WHERE ru.pbx_user_id IS NOT NULL) AS route_user,
		cp.created_at,
		cp.updated_at,
		cp.deleted_at
	FROM
		p_pbx_call_profile AS cp
		LEFT JOIN p_pbx_call_profile_mex_accept_call AS ma USING (pbx_call_profile_id)
		LEFT JOIN p_pbx_call_profile_route_accept_call AS ra USING (pbx_call_profile_id)
		LEFT JOIN p_pbx_user AS u ON (u.call_profile_available_id = cp.pbx_call_profile_id)
	OR (u.call_profile_unavailable_id = cp.pbx_call_profile_id)
		LEFT JOIN p_pbx_route_user AS ru ON u.pbx_user_id = ru.pbx_user_id
		LEFT JOIN unnest(call_profile_ids) WITH ORDINALITY so(pbx_call_profile_id, sort_order) ON cp.pbx_call_profile_id = so.pbx_call_profile_id
	WHERE
		cp.pbx_call_profile_id = ANY (call_profile_ids)
	GROUP BY
		cp.pbx_call_profile_id, u.pbx_user_id, so.sort_order
	ORDER BY
		so.sort_order ASC
$$
LANGUAGE sql;

