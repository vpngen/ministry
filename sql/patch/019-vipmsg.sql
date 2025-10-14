BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '019-vipmsg', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2', '018-roles']);

CREATE TABLE IF NOT EXISTS :"schema_name".vip_messages (
        brigade_id                      uuid NOT NULL,
        mnemo                           text NOT NULL DEFAULT '',
        vpnconfig                       text NOT NULL DEFAULT '',
        finalizer                       boolean NOT NULL DEFAULT false,
        last_try                        timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        update_time                     timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (brigade_id)
);

DO $$
BEGIN
    CREATE TRIGGER vip_messages_update_time_trigger BEFORE INSERT OR UPDATE ON "head".vip_messages FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger vip_messages_update_time_trigger already exists. Ignoring...';
END$$;

GRANT 
        SELECT,UPDATE,INSERT,DELETE 
ON 
        :"schema_name".vip_messages
TO  
        :"head_vpnapi_dbuser";


COMMIT;
