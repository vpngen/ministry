BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '018-roles', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2']);

GRANT 
        SELECT,UPDATE,INSERT,DELETE 
ON 
        :"schema_name".brigadier_vip,
        :"schema_name".brigadier_vip_actions,
TO  
        :"head_vpnapi_dbuser";


COMMIT;
