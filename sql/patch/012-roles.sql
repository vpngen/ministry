BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '012-roles', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes']);


CREATE ROLE :"head_migration_dbuser" WITH LOGIN;
GRANT USAGE ON SCHEMA :"schema_name" TO :"head_migration_dbuser";
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA :"schema_name"  TO :"head_migration_dbuser";

GRANT 
        SELECT,UPDATE,INSERT,DELETE 
ON
        :"schema_name".brigadier_realms,
        :"schema_name".brigadier_realms_actions
TO  
        :"head_migration_dbuser";

COMMIT;
