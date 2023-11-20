BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch( '007-split-partners' , ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms']);

-- Realms reference table.
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_partners (
        brigade_id                      uuid NOT NULL,
        partner_id                      uuid NOT NULL,
        update_time                     timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        FOREIGN KEY (partner_id)        REFERENCES :"schema_name".partners (partner_id),
        PRIMARY KEY (brigade_id)
);


DO $$
BEGIN
    CREATE TRIGGER brigadier_partners_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_partners FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_partners_update_time_trigger already exists. Ignoring...';
END$$;

-- Partners actions reference table.
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_partners_actions (
        brigade_id          uuid NOT NULL,
        partner_id          uuid NOT NULL,
        event_time          timestamp without time zone NOT NULL,
        event_name          text NOT NULL, -- 'assign', 'remove'
        event_info          text NOT NULL,
        update_time         timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id) REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        FOREIGN KEY (partner_id) REFERENCES :"schema_name".partners (partner_id),
        PRIMARY KEY (brigade_id, partner_id, event_time)
);

DO $$
BEGIN
    CREATE TRIGGER brigadier_partners_actions_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_partners_actions FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_partners_actions_update_time_trigger already exists. Ignoring...';
END$$;


-- Step 1: Migrate existing partner_id data to the new brigadier_partners table.
INSERT INTO :"schema_name".brigadier_partners (brigade_id, partner_id)
SELECT
    brigade_id, partner_id
FROM
    :"schema_name".brigadiers_ids
ON CONFLICT DO NOTHING;

-- Step 2: Migrate existing partner_id data to the new brigadier_partners_actions table.
INSERT INTO :"schema_name".brigadier_partners_actions (brigade_id, partner_id, event_time, event_name, event_info)
SELECT
        brigade_id, partner_id, created_at, 'assign', ''
FROM
        :"schema_name".brigadiers_ids
ON CONFLICT DO NOTHING;

-- Step 3: Optionally drop the partner_id column from the brigadiers_ids table.
ALTER TABLE :"schema_name".brigadiers_ids DROP COLUMN partner_id;
ALTER TABLE :"schema_name".brigadiers DROP COLUMN partner_id;

COMMIT;
