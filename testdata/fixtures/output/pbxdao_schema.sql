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
	pbx_call_profile_id "uuid" PRIMARY KEY,
	desktop_call_as_msisdn "pg_catalog"."varchar",
	mobile_call_as_msisdn "pg_catalog"."varchar",
	mex_call_as_msisdn "pg_catalog"."varchar",
	mex_call_as "pg_catalog"."varchar",
	available_by_default "pg_catalog"."bool" NOT NULL DEFAULT true,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	deleted_at "pg_catalog"."timestamptz"
);

CREATE TABLE p_pbx_call_profile_mex_accept_call (
	pbx_call_profile_id "uuid" NOT NULL REFERENCES p_pbx_call_profile,
	subscription_id "uuid",
	accept_calls "pg_catalog"."bool" NOT NULL DEFAULT true,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	PRIMARY KEY (pbx_call_profile_id, subscription_id)
);

CREATE TABLE p_pbx_user (
	pbx_user_id "uuid" PRIMARY KEY,
	name "pg_catalog"."varchar",
	msisdn "pg_catalog"."varchar",
	reason "pg_catalog"."varchar",
	level "pg_catalog"."varchar",
	available_at "pg_catalog"."timestamptz",
	organisation_id "uuid" NOT NULL,
	call_profile_available_id "uuid" REFERENCES p_pbx_call_profile,
	call_profile_unavailable_id "uuid" REFERENCES p_pbx_call_profile,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	deleted_at "pg_catalog"."timestamptz"
);

CREATE UNIQUE INDEX ON p_pbx_user USING btree (msisdn, organisation_id) WHERE deleted_at IS NULL;

CREATE TABLE p_pbx_prompt (
	pbx_prompt_id "uuid" PRIMARY KEY,
	description "pg_catalog"."varchar",
	extension "pg_catalog"."varchar",
	organisation_id "uuid",
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	deleted_at "pg_catalog"."timestamptz"
);

CREATE TABLE p_pbx_voicemail (
	pbx_voicemail_id "uuid" PRIMARY KEY,
	name "pg_catalog"."varchar",
	pin "pg_catalog"."varchar",
	extension "pg_catalog"."varchar",
	msisdn "pg_catalog"."varchar",
	pbx_prompt_id "uuid" REFERENCES p_pbx_prompt,
	organisation_id "uuid" NOT NULL,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	deleted_at "pg_catalog"."timestamptz"
);

CREATE TABLE p_pbx_route (
	pbx_route_id "uuid" PRIMARY KEY,
	type "pg_catalog"."varchar" NOT NULL,
	extension "pg_catalog"."varchar",
	name "pg_catalog"."varchar",
	suffix "pg_catalog"."varchar",
	call_display "pg_catalog"."varchar",
	pbx_user_id "uuid" REFERENCES p_pbx_user,
	pbx_prompt_id "uuid" REFERENCES p_pbx_prompt,
	pbx_voicemail_id "uuid" REFERENCES p_pbx_voicemail,
	organisation_id "uuid",
	subscription_id "uuid",
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL
);

CREATE INDEX ON p_pbx_route USING btree (organisation_id);

ALTER TABLE p_pbx_route
	ADD COLUMN next "uuid" REFERENCES p_pbx_route (pbx_route_id);

CREATE TABLE p_pbx_call_profile_route_accept_call (
	pbx_call_profile_id "uuid" NOT NULL REFERENCES p_pbx_call_profile,
	pbx_route_id "uuid" NOT NULL REFERENCES p_pbx_route,
	accept_calls "pg_catalog"."bool" NOT NULL,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	PRIMARY KEY (pbx_call_profile_id, pbx_route_id)
);

CREATE TABLE p_pbx_route_schedule (
	pbx_schedule_id "uuid" PRIMARY KEY,
	pbx_route_id "uuid" NOT NULL REFERENCES p_pbx_route,
	name "pg_catalog"."varchar",
	type "pg_catalog"."varchar" NOT NULL,
	index "pg_catalog"."int4" NOT NULL,
	recurrence_mon "bool",
	recurrence_tue "bool",
	recurrence_wed "bool",
	recurrence_thu "bool",
	recurrence_fri "bool",
	recurrence_sat "bool",
	recurrence_sun "bool",
	next "uuid" REFERENCES p_pbx_route (pbx_route_id),
	start_time "pg_catalog"."timetz",
	start_date "date",
	end_time "pg_catalog"."timetz",
	end_date "date",
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL
);

CREATE INDEX ON p_pbx_route_schedule USING btree (pbx_route_id);

CREATE TABLE p_pbx_route_queue (
	pbx_route_id "uuid" PRIMARY KEY REFERENCES p_pbx_route,
	max_waiting_callers "pg_catalog"."int4" NOT NULL,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL
);

CREATE TABLE p_pbx_route_menu (
	pbx_route_id "uuid" NOT NULL REFERENCES p_pbx_route,
	type "pg_catalog"."varchar" NOT NULL,
	next "uuid" REFERENCES p_pbx_route (pbx_route_id),
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	PRIMARY KEY (pbx_route_id, type)
);

CREATE INDEX ON p_pbx_route_menu USING btree (pbx_route_id);

CREATE TABLE p_pbx_route_user (
	pbx_user_id "uuid" NOT NULL REFERENCES p_pbx_user,
	pbx_route_id "uuid" NOT NULL REFERENCES p_pbx_route,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	connected "pg_catalog"."bool" NOT NULL,
	PRIMARY KEY (pbx_user_id, pbx_route_id)
);

CREATE INDEX ON p_pbx_route_user USING btree (pbx_route_id);

CREATE TABLE p_pbx_voicemail_user (
	pbx_voicemail_id "uuid" NOT NULL REFERENCES p_pbx_voicemail,
	pbx_user_id "uuid" NOT NULL REFERENCES p_pbx_user,
	notify_sms "pg_catalog"."bool" NOT NULL DEFAULT false,
	notify_email "pg_catalog"."bool" NOT NULL DEFAULT false,
	email "pg_catalog"."varchar",
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	PRIMARY KEY (pbx_user_id, pbx_voicemail_id)
);

CREATE TABLE p_pbx_prompt_recording (
	pbx_prompt_recording_id "uuid" PRIMARY KEY,
	pbx_prompt_id "uuid" NOT NULL REFERENCES p_pbx_prompt,
	url "pg_catalog"."varchar" NOT NULL,
	content_type "pg_catalog"."varchar",
	language "pg_catalog"."varchar",
	duration "pg_catalog"."int8",
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL
);

CREATE TABLE p_pbx_prompt_recording_alternative (
	pbx_prompt_recording_id "uuid" REFERENCES p_pbx_prompt_recording,
	url "pg_catalog"."varchar" NOT NULL,
	content_type "pg_catalog"."varchar" NOT NULL,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	PRIMARY KEY (pbx_prompt_recording_id, content_type)
);

CREATE TABLE p_pbx_prompt_callback (
	pbx_prompt_callback_id "uuid" PRIMARY KEY,
	pbx_prompt_id "uuid" NOT NULL REFERENCES p_pbx_prompt,
	status "pg_catalog"."varchar" NOT NULL,
	language "pg_catalog"."varchar" NOT NULL,
	msisdn "pg_catalog"."varchar" NOT NULL,
	recording_id "pg_catalog"."varchar",
	service_id "pg_catalog"."varchar",
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL
);

CREATE TABLE p_pbx_voicemail_recording (
	pbx_voicemail_recording_id "uuid" PRIMARY KEY,
	pbx_voicemail_id "uuid" NOT NULL REFERENCES p_pbx_voicemail,
	url "pg_catalog"."varchar" NOT NULL,
	content_type "pg_catalog"."varchar",
	msisdn "pg_catalog"."varchar",
	duration "pg_catalog"."int8",
	label "pg_catalog"."varchar",
	recorded_at "pg_catalog"."timestamptz" NOT NULL,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL
);

CREATE TABLE p_pbx_voicemail_recording_alternative (
	pbx_voicemail_recording_id "uuid" REFERENCES p_pbx_voicemail_recording,
	url "pg_catalog"."varchar" NOT NULL,
	content_type "pg_catalog"."varchar" NOT NULL,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	PRIMARY KEY (pbx_voicemail_recording_id, content_type)
);

CREATE TABLE p_pbx_voicemail_user_recording_status (
	pbx_voicemail_recording_id "uuid" NOT NULL REFERENCES p_pbx_voicemail_recording,
	pbx_user_id "uuid" NOT NULL REFERENCES p_pbx_user,
	read "pg_catalog"."bool" NOT NULL,
	created_at "pg_catalog"."timestamptz" NOT NULL,
	updated_at "pg_catalog"."timestamptz" NOT NULL,
	PRIMARY KEY (pbx_voicemail_recording_id, pbx_user_id)
);

CREATE TABLE p_pbx_customer (
	customer_id "uuid" PRIMARY KEY,
	organisation_id "uuid",
	subscription_id "uuid",
	created_at "timestamptz" NOT NULL,
	updated_at "timestamptz" NOT NULL
);

CREATE UNIQUE INDEX ON p_pbx_customer USING btree (organisation_id);

;

;

;

;

;

COMMIT;

DROP FUNCTION IF EXISTS ;

;

