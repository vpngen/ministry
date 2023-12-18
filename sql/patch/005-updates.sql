BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch( '005-updates' , ARRAY['001-init', '002-roles', '003-patch', '004-realms']);

DROP TRIGGER IF EXISTS update_brigadiers_ids_update_time_trigger ON :"schema_name".brigadiers_ids CASCADE;
DROP FUNCTION IF EXISTS update_brigadiers_ids_update_time() CASCADE;

DROP TRIGGER IF EXISTS update_brigadiers_ids_purged_at_trigger ON :"schema_name".brigadiers CASCADE;
DROP FUNCTION IF EXISTS update_brigadiers_ids_purged_at() CASCADE;

DROP TRIGGER IF EXISTS update_deleted_at_on_deleted_insert_trigger ON :"schema_name".deleted_brigadiers CASCADE;
DROP FUNCTION IF EXISTS update_deleted_at_on_deleted_insert() CASCADE;

DROP TRIGGER IF EXISTS add_brigade_id_to_ids_trigger ON :"schema_name".brigadiers CASCADE;
DROP FUNCTION IF EXISTS add_brigade_id_to_ids() CASCADE;

DROP FUNCTION IF EXISTS create_brigadier() CASCADE;

-- If you need to add a foreign key constraint to an existing table
ALTER TABLE :"schema_name".brigadiers
DROP CONSTRAINT IF EXISTS brigadiers_brigade_id_fkey,
ADD CONSTRAINT brigadiers_brigade_id_fkey FOREIGN KEY (brigade_id)
REFERENCES :"schema_name".brigadiers_ids (brigade_id);

-- If you need to update the 'brigadier_salts' table's foreign key
ALTER TABLE :"schema_name".brigadier_salts
DROP CONSTRAINT IF EXISTS brigadier_salts_brigade_id_fkey,
ADD CONSTRAINT brigadier_salts_brigade_id_fkey FOREIGN KEY (brigade_id)
REFERENCES :"schema_name".brigadiers (brigade_id);

-- If you need to update the 'brigadier_keys' table's foreign key
ALTER TABLE :"schema_name".brigadier_keys
DROP CONSTRAINT IF EXISTS brigadier_keys_brigade_id_fkey,
ADD CONSTRAINT brigadier_keys_brigade_id_fkey FOREIGN KEY (brigade_id)
REFERENCES :"schema_name".brigadiers (brigade_id);

-- Update trigger for update_time column.
CREATE OR REPLACE FUNCTION update_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.update_time = NOW() AT TIME ZONE 'UTC';
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    CREATE TRIGGER brigadiers_ids_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadiers_ids FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadiers_ids_update_time_trigger already exists. Ignoring...';
END$$;

DO $$
BEGIN
    CREATE TRIGGER brigades_actions_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigades_actions FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigades_actions_update_time_trigger already exists. Ignoring...';
END$$;

DO $$
BEGIN
    CREATE TRIGGER realms_update_time_trigger BEFORE INSERT OR UPDATE ON "head".realms FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger realms_update_time_trigger already exists. Ignoring...';
END$$;

DO $$
BEGIN
    CREATE TRIGGER partners_update_time_trigger BEFORE INSERT OR UPDATE ON "head".partners FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger partners_update_time_trigger already exists. Ignoring...';
END$$;

COMMIT;
