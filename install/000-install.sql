BEGIN;

CREATE SCHEMA :"schema_name";

-- Realms.
CREATE TABLE :"schema_name".realms (
    realm_id            uuid PRIMARY KEY NOT NULL,
    control_ip          inet UNIQUE NOT NULL,
    is_active           bool NOT NULL
);

CREATE TABLE :"schema_name".brigadiers (
    brigade_id          uuid PRIMARY KEY NOT NULL,
    realm_id            uuid NOT NULL,
    create_at           timestamp without time zone NOT NULL DEFAULT NOW(),
    brigadier           text UNIQUE NOT NULL,
    person              json NOT NULL,
    FOREIGN KEY (realm_id) REFERENCES :"schema_name".realms (realm_id)
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

CREATE ROLE :"realms_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"realms_dbuser";
GRANT SELECT,INSERT,UPDATE,DELETE ON :"schema_name".realms TO :"realms_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"realms_dbuser";
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA :"schema_name"  TO :"realms_dbuser";

--CREATE ROLE :"brigadiers_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"brigadiers_dbuser";
GRANT SELECT ON :"schema_name".realms TO :"brigadiers_dbuser";
GRANT SELECT,UPDATE,INSERT,DELETE ON :"schema_name".brigadiers, :"schema_name".brigadier_salts, :"schema_name".brigadier_keys TO :"brigadiers_dbuser";
GRANT USAGE,SELECT,UPDATE ON SEQUENCE :"schema_name".brigadiers_queue_queue_id_seq TO :"brigadiers_dbuser";

COMMIT;
