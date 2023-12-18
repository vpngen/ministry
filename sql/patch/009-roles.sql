BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '009-roles', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm']);

ALTER DEFAULT PRIVILEGES IN SCHEMA "head" GRANT SELECT ON TABLES TO :"head_admin_dbuser";
ALTER DEFAULT PRIVILEGES IN SCHEMA "head" GRANT SELECT ON TABLES TO :"head_vpnapi_dbuser";
ALTER DEFAULT PRIVILEGES IN SCHEMA "head" GRANT SELECT ON TABLES TO :"head_stats_dbuser";

GRANT 
        SELECT,UPDATE,INSERT,DELETE 
ON 
        :"schema_name".brigadier_realms,
        :"schema_name".brigadier_realms_actions,
        :"schema_name".brigadier_partners,
        :"schema_name".brigadier_partners_actions,
        :"schema_name".start_labels
TO  
        :"head_vpnapi_dbuser";

COMMIT;
