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

CREATE TABLE p_pbx_call_profile
(
    "pbx_call_profile_id"  UUID PRIMARY KEY,
    "desktop_call_as_msisdn" VARCHAR,
    "mobile_call_as_msisdn"  VARCHAR,
    "mex_call_as_msisdn"     VARCHAR,
    "mex_call_as"            VARCHAR,
    "available_by_default"   BOOLEAN NOT NULL DEFAULT TRUE,
    "created_at"             TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"             TIMESTAMP WITH TIME ZONE NOT NULL,
    "deleted_at"             TIMESTAMP WITH TIME ZONE
);

CREATE TABLE p_pbx_call_profile_mex_accept_call
(
    "pbx_call_profile_id" UUID                     NOT NULL REFERENCES p_pbx_call_profile ON DELETE CASCADE,
    "subscription_id"     UUID,
    "accept_calls"        BOOLEAN NOT NULL DEFAULT TRUE,
    "created_at"          TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"          TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY ("pbx_call_profile_id", "subscription_id")
);

CREATE TABLE p_pbx_user
(
    "pbx_user_id"                 UUID PRIMARY KEY,
    "name"                        VARCHAR,
    "msisdn"                      VARCHAR,
    "reason"                      VARCHAR,
    "level"                       VARCHAR,
    "available_at"                TIMESTAMP WITH TIME ZONE,
    "organisation_id"             UUID                     NOT NULL,
    "call_profile_available_id"   UUID                     REFERENCES p_pbx_call_profile ON DELETE SET NULL,
    "call_profile_unavailable_id" UUID                     REFERENCES p_pbx_call_profile ON DELETE SET NULL,
    "created_at"                  TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"                  TIMESTAMP WITH TIME ZONE NOT NULL,
    "deleted_at"                  TIMESTAMP WITH TIME ZONE
);
CREATE UNIQUE INDEX ON p_pbx_user (msisdn, organisation_id) WHERE deleted_at IS NULL;

CREATE TABLE p_pbx_prompt
(
    "pbx_prompt_id"   UUID PRIMARY KEY,
    "description"     VARCHAR,
    "extension"       VARCHAR,
    "organisation_id" UUID,
    "created_at"      TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"      TIMESTAMP WITH TIME ZONE NOT NULL,
    "deleted_at"      TIMESTAMP WITH TIME ZONE
);

CREATE TABLE p_pbx_voicemail
(
    "pbx_voicemail_id" UUID PRIMARY KEY,
    "name"             VARCHAR,
    "pin"              VARCHAR,
    "extension"        VARCHAR,
    "msisdn"           VARCHAR,
    "pbx_prompt_id"    UUID                     REFERENCES p_pbx_prompt ON DELETE SET NULL,
    "organisation_id"  UUID                     NOT NULL,
    "created_at"       TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"       TIMESTAMP WITH TIME ZONE NOT NULL,
    "deleted_at"       TIMESTAMP WITH TIME ZONE
);
 
CREATE TABLE p_pbx_route
(
    "pbx_route_id"     UUID PRIMARY KEY,
    "type"             VARCHAR                  NOT NULL, -- ivr, prompt, schedule, group, queue, voicemail, forwarding
    "extension"        VARCHAR,                           -- external if it starts with a + (pattern: /^\+?[0-9]+$/)
    "name"             VARCHAR,
    "suffix"           VARCHAR,
    "call_display"     VARCHAR,
    -- "next" is added below in an ALTER TABLE because circular references
    "pbx_user_id"      UUID                     REFERENCES p_pbx_user ON DELETE SET NULL,
    "pbx_prompt_id"    UUID                     REFERENCES p_pbx_prompt ON DELETE SET NULL,
    "pbx_voicemail_id" UUID                     REFERENCES p_pbx_voicemail ON DELETE SET NULL,
    "organisation_id"  UUID,
    "subscription_id"  UUID,
    "created_at"       TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"       TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE INDEX ON p_pbx_route (organisation_id);

-- must be added after unique index or we get
-- errors about it missing
ALTER TABLE p_pbx_route
    ADD COLUMN "next" UUID
        REFERENCES p_pbx_route ("pbx_route_id")
            ON DELETE SET NULL;

CREATE TABLE p_pbx_call_profile_route_accept_call
(
    "pbx_call_profile_id" UUID                     NOT NULL REFERENCES p_pbx_call_profile ON DELETE CASCADE,
    "pbx_route_id"        UUID                     NOT NULL REFERENCES p_pbx_route ON DELETE CASCADE,
    "accept_calls"        BOOLEAN NOT NULL,
    "created_at"          TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"          TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY ("pbx_call_profile_id", "pbx_route_id")
);

CREATE TABLE p_pbx_route_schedule
(
    "pbx_schedule_id" UUID PRIMARY KEY,
    "pbx_route_id"   UUID                     NOT NULL
        REFERENCES p_pbx_route
            ON DELETE CASCADE,
    "name"           VARCHAR,
    "type"           VARCHAR                  NOT NULL, -- weekly, daily, monthly, yearly, other
    "index"          INTEGER                  NOT NULL, -- sequential number per route (0 = "default", highest is "first")
    "recurrence_mon" BOOL,
    "recurrence_tue" BOOL,
    "recurrence_wed" BOOL,
    "recurrence_thu" BOOL,
    "recurrence_fri" BOOL,
    "recurrence_sat" BOOL,
    "recurrence_sun" BOOL,
    "next"           UUID                     REFERENCES p_pbx_route ("pbx_route_id") ON DELETE SET NULL,
    "start_time"     TIME WITH TIME ZONE,
    "start_date"     DATE,
    "end_time"       TIME WITH TIME ZONE,
    "end_date"       DATE,
    "created_at"     TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"     TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE INDEX ON p_pbx_route_schedule (pbx_route_id);

CREATE TABLE p_pbx_route_queue
(
    "pbx_route_id"        UUID PRIMARY KEY
        REFERENCES p_pbx_route
            ON DELETE CASCADE,
    "max_waiting_callers" INTEGER                  NOT NULL,
    "created_at"          TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"          TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE TABLE p_pbx_route_menu
(
    "pbx_route_id" UUID                     NOT NULL
        REFERENCES p_pbx_route
            ON DELETE CASCADE,
    "type"         VARCHAR                  NOT NULL, -- 1,2,3,4,5,6,7,8,9,0,#,*
    "next"         UUID                     REFERENCES p_pbx_route ("pbx_route_id") ON DELETE SET NULL,
    "created_at"   TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"   TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY ("pbx_route_id", "type")
);

CREATE INDEX ON p_pbx_route_menu (pbx_route_id);

CREATE TABLE p_pbx_route_user
(
    "pbx_user_id"  UUID                     NOT NULL REFERENCES p_pbx_user ON DELETE CASCADE,
    "pbx_route_id" UUID                     NOT NULL REFERENCES p_pbx_route ON DELETE CASCADE,
    "created_at"   TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"   TIMESTAMP WITH TIME ZONE NOT NULL,
    "connected"    BOOLEAN                  NOT NULL,
    PRIMARY KEY (pbx_user_id, pbx_route_id)
);

CREATE INDEX ON p_pbx_route_user (pbx_route_id);

CREATE TABLE p_pbx_voicemail_user
(
    "pbx_voicemail_id" UUID                     NOT NULL REFERENCES p_pbx_voicemail ON DELETE CASCADE,
    "pbx_user_id"      UUID                     NOT NULL REFERENCES p_pbx_user ON DELETE CASCADE,
    "notify_sms"       BOOLEAN                  NOT NULL DEFAULT FALSE,
    "notify_email"     BOOLEAN                  NOT NULL DEFAULT FALSE,
    "email"            VARCHAR,
    "created_at"       TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"       TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (pbx_user_id, pbx_voicemail_id)
);

CREATE TABLE p_pbx_prompt_recording
(
    "pbx_prompt_recording_id" UUID PRIMARY KEY,
    "pbx_prompt_id"           UUID                     NOT NULL REFERENCES p_pbx_prompt ON DELETE CASCADE,
    "url"                     VARCHAR                  NOT NULL,
    "content_type"            VARCHAR,
    "language"                VARCHAR,
    "duration"                BIGINT,
    "created_at"              TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"              TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE TABLE p_pbx_prompt_recording_alternative
(
    "pbx_prompt_recording_id" UUID REFERENCES p_pbx_prompt_recording ON DELETE CASCADE,
    "url"                     VARCHAR                  NOT NULL,
    "content_type"            VARCHAR                  NOT NULL,
    "created_at"              TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"              TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (pbx_prompt_recording_id, content_type)
);

CREATE TABLE p_pbx_prompt_callback
(
    "pbx_prompt_callback_id" UUID PRIMARY KEY,
    "pbx_prompt_id"          UUID                     NOT NULL REFERENCES p_pbx_prompt ON DELETE CASCADE,
    "status"                 VARCHAR                  NOT NULL,
    "language"               VARCHAR                  NOT NULL,
    "msisdn"                 VARCHAR                  NOT NULL,
    "recording_id"           VARCHAR,
    "service_id"             VARCHAR,
    "created_at"             TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"             TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE TABLE p_pbx_voicemail_recording
(
    "pbx_voicemail_recording_id" UUID PRIMARY KEY,
    "pbx_voicemail_id"           UUID                     NOT NULL REFERENCES p_pbx_voicemail ON DELETE CASCADE,
    "url"                        VARCHAR                  NOT NULL, -- to .wav on s3
    "content_type"               VARCHAR,
    "msisdn"                     VARCHAR,                           -- called phone number
    "duration"                   BIGINT,
    "label"                      VARCHAR,
    "recorded_at"                TIMESTAMP WITH TIME ZONE NOT NULL,
    "created_at"                 TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"                 TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE TABLE p_pbx_voicemail_recording_alternative
(
    "pbx_voicemail_recording_id" UUID REFERENCES p_pbx_voicemail_recording ON DELETE CASCADE,
    "url"                        VARCHAR                  NOT NULL,
    "content_type"               VARCHAR                  NOT NULL,
    "created_at"                 TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"                 TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (pbx_voicemail_recording_id, content_type)
);

CREATE TABLE p_pbx_voicemail_user_recording_status
(
    "pbx_voicemail_recording_id" UUID                     NOT NULL REFERENCES p_pbx_voicemail_recording ON DELETE CASCADE,
    "pbx_user_id"                UUID                     NOT NULL REFERENCES p_pbx_user ON DELETE CASCADE,
    "read"                       BOOLEAN                  NOT NULL,
    "created_at"                 TIMESTAMP WITH TIME ZONE NOT NULL,
    "updated_at"                 TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (pbx_voicemail_recording_id, pbx_user_id)
);

CREATE TABLE p_pbx_customer
(
    customer_id     UUID PRIMARY KEY,
    organisation_id UUID,
    subscription_id UUID,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX ON p_pbx_customer (organisation_id);

-- pbx_route_select selects all fields that we want for a pbx route
CREATE OR REPLACE FUNCTION pbx_route_select(route_id UUID[])
    RETURNS TABLE
            (
                pbx_route_id      UUID,
                type              VARCHAR,
                name              VARCHAR,
                next              UUID,
                extension         VARCHAR,
                pbx_user_id       UUID,
                pbx_prompt_id     UUID,
                pbx_voicemail_id  UUID,
                users             UUID[],
                connectedUsers    UUID[],
                disconnectedUsers UUID[],
                menu              JSON,
                schedule          JSON,
                queue             JSON,
                suffix            VARCHAR,
                call_display      VARCHAR,
                subscription_id   UUID,
                organisation_id   UUID,
                created_at        TIMESTAMPTZ,
                updated_at        TIMESTAMPTZ
            )
AS
$$
SELECT r.pbx_route_id,
       r.type,
       coalesce(r.name, '')                                                                   as name,
       r.next,
       r.extension,
       r.pbx_user_id,
       r.pbx_prompt_id,
       r.pbx_voicemail_id,
       array_agg(u.pbx_user_id) FILTER (WHERE u.pbx_route_id IS NOT NULL)                     as users,
       array_agg(u.pbx_user_id)
       FILTER (WHERE u.pbx_route_id IS NOT NULL AND u.connected = true)                       as connectedUsers,
       array_agg(u.pbx_user_id)
       FILTER (WHERE u.pbx_route_id IS NOT NULL AND u.connected = false)                      as disconnectedUsers,
       json_object_agg(m.type, m.next) FILTER (WHERE m.pbx_route_id IS NOT NULL)              as menu,
       json_agg(row_to_json(i) ORDER BY i.index) FILTER (WHERE i.pbx_route_id IS NOT NULL)    as schedule,
       (SELECT row_to_json(q) FROM p_pbx_route_queue q WHERE r.pbx_route_id = q.pbx_route_id) as queue,
       COALESCE(r.suffix, '')                                                                 as suffix,
       COALESCE(r.call_display, 'a')                                                          as call_display,
       r.subscription_id,
       r.organisation_id,
       r.created_at,
       r.updated_at
FROM p_pbx_route r
         LEFT JOIN
     p_pbx_route_user u USING (pbx_route_id)
         LEFT JOIN
     p_pbx_route_menu m USING (pbx_route_id)
         LEFT JOIN
     p_pbx_route_schedule i USING (pbx_route_id)
WHERE r.pbx_route_id = ANY (route_id)
GROUP BY r.pbx_route_id
$$
    LANGUAGE SQL;

CREATE OR REPLACE FUNCTION pbx_prompt_select(prompt_ids UUID[], contentType VARCHAR)
    RETURNS TABLE
            (
                pbx_prompt_id   UUID,
                description     VARCHAR,
                extension       VARCHAR,
                recording       JSONB,
                modifiable      BOOL,
                organisation_id UUID,
                created_at      TIMESTAMPTZ,
                updated_at      TIMESTAMPTZ
            )
AS
$$
SELECT p.pbx_prompt_id,
       coalesce(p.description, ''),
       coalesce(p.extension, ''),
       jsonb_agg(row_to_json(r)) FILTER (WHERE r.pbx_prompt_id IS NOT NULL) as recordings,
       p.organisation_id IS NOT NULL,
       p.organisation_id,
       p.created_at,
       p.updated_at
FROM p_pbx_prompt p
         LEFT JOIN LATERAL (
    SELECT r.pbx_prompt_recording_id,
           r.pbx_prompt_id,
           COALESCE(a.content_type, r.content_type) as content_type,
           COALESCE(a.url, r.url)                   as url,
           r.duration,
           r.language,
           r.created_at,
           r.updated_at
    FROM p_pbx_prompt_recording r
             LEFT JOIN
         p_pbx_prompt_recording_alternative a
         ON (a.pbx_prompt_recording_id = r.pbx_prompt_recording_id AND a.content_type = contentType)
    WHERE (contentType IS NULL
        OR r.content_type = contentType
        OR a.content_type = contentType
        )
      AND r.pbx_prompt_id = p.pbx_prompt_id
    ) r ON TRUE
WHERE p.pbx_prompt_id = ANY (prompt_ids)
GROUP BY p.pbx_prompt_id
$$
    LANGUAGE SQL;

CREATE OR REPLACE FUNCTION pbx_voicemail_recording_select(voicemail_recording_ids UUID[], contentType VARCHAR)
    RETURNS TABLE
            (
                pbx_voicemail_recording_id UUID,
                url                        VARCHAR,
                content_type               VARCHAR,
                msisdn                     VARCHAR,
                duration                   BIGINT,
                read                       BOOL,
                pbx_voicemail_id           UUID,
                recorded_at                TIMESTAMPTZ,
                created_at                 TIMESTAMPTZ,
                updated_at                 TIMESTAMPTZ
            )
AS
$$
SELECT r.pbx_voicemail_recording_id,
       coalesce(a.url, r.url, '')                            as url,
       coalesce(a.content_type, r.content_type, 'audio/wav') as content_type,
       coalesce(r.msisdn, '')                                as msisdn,
       r.duration,
       FALSE                                                 as read, -- user "read" status not available in this select
       r.pbx_voicemail_id,
       r.recorded_at,
       r.created_at,
       r.updated_at
FROM p_pbx_voicemail_recording r
         LEFT JOIN
     p_pbx_voicemail_recording_alternative a
     ON (r.pbx_voicemail_recording_id = a.pbx_voicemail_recording_id AND a.content_type = contentType)
WHERE r.pbx_voicemail_recording_id = ANY (voicemail_recording_ids)
  AND (
        contentType IS NULL
        OR
        r.content_type = contentType
        OR
        a.content_type = contentType
    )
$$
    LANGUAGE SQL;

CREATE OR REPLACE FUNCTION pbx_voicemail_recording_with_read_select(
    voicemail_recording_ids UUID[], contentType VARCHAR, userMsisdn VARCHAR)
    RETURNS TABLE
            (
                pbx_voicemail_recording_id UUID,
                url                        VARCHAR,
                content_type               VARCHAR,
                msisdn                     VARCHAR,
                duration                   BIGINT,
                read                       BOOL,
                pbx_voicemail_id           UUID,
                recorded_at                TIMESTAMPTZ,
                created_at                 TIMESTAMPTZ,
                updated_at                 TIMESTAMPTZ
            )
AS
$$
SELECT r.pbx_voicemail_recording_id,
       coalesce(a.url, r.url, '')                   as url,
       coalesce(a.content_type, r.content_type, '') as content_type,
       coalesce(r.msisdn, '')                       as msisdn,
       r.duration,
       COALESCE(s.read, FALSE)                      as read,
       r.pbx_voicemail_id,
       r.recorded_at,
       r.created_at,
       r.updated_at
FROM p_pbx_voicemail_recording r
         JOIN
     p_pbx_voicemail_user vu USING (pbx_voicemail_id)
         JOIN
     p_pbx_user u USING (pbx_user_id)
         LEFT JOIN
     p_pbx_voicemail_user_recording_status s USING (pbx_voicemail_recording_id, pbx_user_id)
         LEFT JOIN
     p_pbx_voicemail_recording_alternative a
     ON (r.pbx_voicemail_recording_id = a.pbx_voicemail_recording_id AND a.content_type = contentType)
WHERE r.pbx_voicemail_recording_id = ANY (voicemail_recording_ids)
  AND u.msisdn = userMsisdn
  AND (
        contentType::TEXT IS NULL
        OR
        r.content_type = contentType
        OR
        a.content_type = contentType
    )
$$
    LANGUAGE SQL;

CREATE OR REPLACE FUNCTION pbx_voicemail_select(voicemail_ids UUID[])
    RETURNS TABLE
            (
                pbx_voicemail_id UUID,
                name             VARCHAR,
                pin              VARCHAR,
                msisdn           VARCHAR,
                pbx_prompt_id    UUID,
                organisation_id  UUID,
                users            JSONB,
                created_at       TIMESTAMPTZ,
                updated_at       TIMESTAMPTZ
            )
AS
$$
SELECT v.pbx_voicemail_id,
       coalesce(v.name, ''),
       coalesce(v.pin, ''),
       coalesce(v.msisdn, ''),
       v.pbx_prompt_id,
       v.organisation_id,
       jsonb_agg(row_to_json(u)) FILTER (WHERE u.pbx_voicemail_id IS NOT NULL) as users,
       v.created_at,
       v.updated_at
FROM p_pbx_voicemail v
         LEFT JOIN
     p_pbx_voicemail_user u USING (pbx_voicemail_id)
WHERE v.pbx_voicemail_id = ANY (voicemail_ids)
GROUP BY v.pbx_voicemail_id
$$
    LANGUAGE SQL;

COMMIT;

DROP FUNCTION IF EXISTS pbx_callprofile_select(uuid[]); -- if return type is changed
CREATE OR REPLACE FUNCTION pbx_callprofile_select(call_profile_ids UUID[])
    RETURNS TABLE
    (
        pbx_call_profile_id         UUID,
        pbx_user_id                 UUID,
        desktop_call_as_msisdn      VARCHAR,
        mobile_call_as_msisdn       VARCHAR,
        mex_call_as_msisdn          VARCHAR,
        mex_call_as                 VARCHAR,
        available_by_default        BOOL,
        mex_accept_call             JSON,
        route_accept_call           JSON,
        route_user                  JSON, -- old user per route settings connected/disconnected
        created_at                  TIMESTAMPTZ,
        updated_at                  TIMESTAMPTZ,
        deleted_at                  TIMESTAMPTZ
    )
AS
$$
SELECT 
    cp.pbx_call_profile_id,
    u.pbx_user_id,
    desktop_call_as_msisdn,
    mobile_call_as_msisdn,
    mex_call_as_msisdn,
    mex_call_as,
    available_by_default,
    json_agg(row_to_json(ma) ORDER BY ma.subscription_id) FILTER (WHERE ma.subscription_id IS NOT NULL) as mex_accept_call,
    json_agg(row_to_json(ra) ORDER BY ra.pbx_route_id) FILTER (WHERE ra.pbx_route_id IS NOT NULL) as route_accept_call,
    json_agg(row_to_json(ru) ORDER BY ru.pbx_route_id) FILTER (WHERE ru.pbx_user_id IS NOT NULL) as route_user,
    cp.created_at,
    cp.updated_at,
    cp.deleted_at
FROM p_pbx_call_profile cp
LEFT JOIN p_pbx_call_profile_mex_accept_call ma USING (pbx_call_profile_id)
LEFT JOIN p_pbx_call_profile_route_accept_call ra USING (pbx_call_profile_id)
LEFT JOIN p_pbx_user u ON (u.call_profile_available_id = cp.pbx_call_profile_id OR u.call_profile_unavailable_id = cp.pbx_call_profile_id) -- get pbx_user_id
LEFT JOIN p_pbx_route_user ru ON (u.pbx_user_id = ru.pbx_user_id) -- get route connections settings
LEFT JOIN unnest(call_profile_ids) WITH ORDINALITY so (pbx_call_profile_id, sort_order) on (cp.pbx_call_profile_id = so.pbx_call_profile_id)
WHERE cp.pbx_call_profile_id = ANY(call_profile_ids)
GROUP BY cp.pbx_call_profile_id, u.pbx_user_id, so.sort_order
ORDER BY
  so.sort_order ASC;
$$
    LANGUAGE SQL;