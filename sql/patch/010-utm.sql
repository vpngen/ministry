BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '008-utm', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles']);

ALTER TABLE :"schema_name".start_labels ADD COLUMN update_time timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC');

DO $$
BEGIN
    CREATE TRIGGER start_labels_update_time_trigger BEFORE INSERT OR UPDATE ON "head".start_labels FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger start_labels_update_time_trigger already exists. Ignoring...';
END$$;

COMMIT;
