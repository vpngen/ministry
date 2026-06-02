BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch('021-free-telegram-ids', ARRAY['001-init', '002-roles', '003-patch', '004-realms', '005-updates', '006-split-realms', '007-split-partners', '008-utm', '009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2', '018-roles', '019-vipmsg', '020-vip-lang']);

CREATE TABLE IF NOT EXISTS :"schema_name".free_telegram_ids (
        brigade_id                      uuid NOT NULL,
        telegram_id                     bigint NOT NULL,
        lang                            text NOT NULL DEFAULT 'ru',
        update_time                     timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (brigade_id)
);

DO $$
BEGIN
    CREATE TRIGGER free_telegram_ids_update_time_trigger BEFORE INSERT OR UPDATE ON "head".free_telegram_ids FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger free_telegram_ids_update_time_trigger already exists. Ignoring...';
END$$;

GRANT
        SELECT,UPDATE,INSERT,DELETE
ON
        :"schema_name".free_telegram_ids
TO
        :"head_vpnapi_dbuser";


COMMIT;
