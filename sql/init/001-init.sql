BEGIN;

SELECT _v.register_patch( '001-init' );
SELECT _v.assert_user_is_superuser();

CREATE SCHEMA :"schema_name";

-- Realms.
CREATE TABLE :"schema_name".realms (
    realm_id            uuid PRIMARY KEY NOT NULL,
    realm_name          text UNIQUE NOT NULL,
    control_ip          inet UNIQUE NOT NULL,
    is_active           bool NOT NULL,
    free_slots          int NOT NULL DEFAULT 0,
    update_time         timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE TABLE :"schema_name".partners (
    partner_id          uuid PRIMARY KEY NOT NULL,
    partner             text UNIQUE NOT NULL,
    created_at          timestamp without time zone DEFAULT now(),
    is_active           bool NOT NULL,
    update_time         timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE TABLE :"schema_name".brigadiers_ids (
  brigade_id uuid NOT NULL PRIMARY KEY,
  realm_id uuid NOT NULL,
  partner_id uuid NOT NULL,
  reason text NOT NULL DEFAULT '',
  created_at timestamp without time zone NOT NULL,
  deleted_at timestamp without time zone DEFAULT NULL,
  purged_at timestamp without time zone DEFAULT NULL,
  update_time timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE TABLE :"schema_name".brigadiers (
    brigade_id          uuid PRIMARY KEY NOT NULL,
    realm_id            uuid NOT NULL,
    partner_id          uuid NOT NULL,
    created_at          timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    brigadier           text UNIQUE NOT NULL,
    person              json NOT NULL,
    FOREIGN KEY (realm_id) REFERENCES :"schema_name".realms (realm_id),
    FOREIGN KEY (partner_id) REFERENCES :"schema_name".partners (partner_id)
);

CREATE TABLE :"schema_name".brigadier_salts (
    brigade_id          uuid NOT NULL REFERENCES :"schema_name".brigadiers (brigade_id),
    salt                bytea
);

CREATE TABLE :"schema_name".brigadier_keys (
    brigade_id          uuid NOT NULL REFERENCES :"schema_name".brigadiers (brigade_id),
    key                 bytea
);

CREATE TABLE :"schema_name".brigadiers_queue (
    queue_id serial PRIMARY KEY,
    payload json NOT NULL,
    error json
);

CREATE TABLE :"schema_name".deleted_brigadiers (
    brigade_id          uuid UNIQUE NOT NULL REFERENCES :"schema_name".brigadiers (brigade_id),
    deleted_at timestamp without time zone DEFAULT now(),
    reason text NOT NULL
);

CREATE TABLE :"schema_name".brigades_actions (
    brigade_id          uuid NOT NULL REFERENCES :"schema_name".brigadiers_ids (brigade_id),
    event_time          timestamp without time zone NOT NULL,
    event_name          text NOT NULL,
    event_info          text NOT NULL,
    update_time         timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    PRIMARY KEY (brigade_id, event_time)
);

CREATE DOMAIN partner_token AS bytea CHECK (octet_length(value) = 32);

CREATE TABLE :"schema_name".partners_tokens (
    partner_id          uuid NOT NULL REFERENCES :"schema_name".partners (partner_id),
    token               partner_token NOT NULL,
    name                text NOT NULL,
    created_at          timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    UNIQUE (partner_id, token)
);

CREATE TABLE :"schema_name".partners_realms (
    partner_id          uuid NOT NULL REFERENCES :"schema_name".partners (partner_id),
    realm_id            uuid NOT NULL REFERENCES :"schema_name".realms (realm_id)
);

COMMIT;
