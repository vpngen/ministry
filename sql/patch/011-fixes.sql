BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '011-fixes', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm']);

GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"head_admin_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"head_vpnapi_dbuser";
GRANT SELECT ON ALL TABLES IN SCHEMA :"schema_name" TO :"head_stats_dbuser";

COMMIT;
