BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.try_register_patch( '002-roles' , ARRAY['001-init']);

CREATE ROLE :"head_admin_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"head_admin_dbuser";
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA :"schema_name"  TO :"head_admin_dbuser";
GRANT SELECT,INSERT,UPDATE,DELETE ON :"schema_name".realms TO :"head_admin_dbuser";
GRANT SELECT,INSERT,UPDATE,DELETE ON :"schema_name".partners, :"schema_name".partners_realms, :"schema_name".partners_tokens TO :"head_admin_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"head_admin_dbuser";

CREATE ROLE :"head_vpnapi_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"head_vpnapi_dbuser";
GRANT USAGE,SELECT,UPDATE ON ALL SEQUENCES IN SCHEMA :"schema_name" TO :"head_vpnapi_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"head_vpnapi_dbuser";
GRANT 
        SELECT,UPDATE,INSERT,DELETE 
ON 
        :"schema_name".brigadiers, 
        :"schema_name".brigadier_salts, 
        :"schema_name".brigadier_keys, 
        :"schema_name".deleted_brigadiers, 
        :"schema_name".brigades_actions 
TO 
        :"head_vpnapi_dbuser";
GRANT SELECT ON :"schema_name".partners, :"schema_name".partners_realms TO :"head_vpnapi_dbuser";

CREATE ROLE :"partners_admin_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"partners_admin_dbuser";
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA :"schema_name" TO :"partners_admin_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"partners_admin_dbuser";
GRANT SELECT,INSERT,UPDATE,DELETE ON :"schema_name".partners, :"schema_name".partners_tokens, :"schema_name".partners_realms TO :"partners_admin_dbuser";

CREATE ROLE :"head_stats_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"head_stats_dbuser";
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA :"schema_name" TO :"head_stats_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"head_stats_dbuser";

COMMIT;
