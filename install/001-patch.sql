BEGIN;

-- Partners.

CREATE DOMAIN partner_token AS bytea CHECK (octet_length(value) = 32);

CREATE TABLE :"schema_name".partners (
    partner_id          uuid PRIMARY KEY NOT NULL,
    partner             text UNIQUE NOT NULL,
    created_at          timestamp without time zone DEFAULT now(),
    is_active           bool NOT NULL
);

CREATE TABLE :"schema_name".partners_tokens (
    partner_id          uuid NOT NULL REFERENCES :"schema_name".partners (partner_id),
    token               partner_token NOT NULL,
    name                text NOT NULL,
    created_at          timestamp without time zone DEFAULT now(),
    UNIQUE (partner_id, token)
);


CREATE TABLE :"schema_name".partners_realms (
    partner_id          uuid NOT NULL REFERENCES :"schema_name".partners (partner_id),
    realm_id            uuid NOT NULL REFERENCES :"schema_name".realms (realm_id)
);

-- Grants.

--CREATE ROLE :"partnerss_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"partners_dbuser";
GRANT SELECT,INSERT,UPDATE,DELETE ON :"schema_name".partners, :"schema_name".partners_tokens, :"schema_name".partners_realms TO :"partners_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"partners_dbuser";
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA :"schema_name"  TO :"partners_dbuser";

GRANT SELECT ON :"schema_name".partners TO :"brigadiers_dbuser";
GRANT SELECT ON :"schema_name".partners_tokens TO :"brigadiers_dbuser";

COMMIT;