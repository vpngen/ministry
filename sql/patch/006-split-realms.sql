BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch( '006-split-realms' , ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates']);

-- Realms reference table.
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_realms (
        brigade_id          uuid NOT NULL,
        realm_id            uuid NOT NULL,
        draft               bool NOT NULL,
        featured            bool NOT NULL,
        update_time         timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id) REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        FOREIGN KEY (realm_id) REFERENCES :"schema_name".realms (realm_id),
        PRIMARY KEY (brigade_id, realm_id),
        CHECK (NOT (draft AND featured))
);

CREATE UNIQUE INDEX idx_unique_brigade_id_featured_true ON :"schema_name".brigadier_realms (brigade_id) WHERE featured=True;

DO $$
BEGIN
    CREATE TRIGGER brigadier_realms_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_realms FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_realms_update_time_trigger already exists. Ignoring...';
END$$;

-- Realms actions reference table.
-- tracks:
--    order -> remove -> remove
--    order -> remove -> modify -> remove
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_realms_actions (
        brigade_id          uuid NOT NULL,
        realm_id            uuid NOT NULL,
        event_time          timestamp without time zone NOT NULL,
        event_name          text NOT NULL, -- 'order', 'compose', 'modify', 'remove'
        event_info          text NOT NULL, -- modify: 'promote', 'retire'
        update_time         timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id) REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        FOREIGN KEY (realm_id) REFERENCES :"schema_name".realms (realm_id),
        PRIMARY KEY (brigade_id, realm_id, event_time)
);

DO $$
BEGIN
    CREATE TRIGGER brigadier_realms_actions_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_realms_actions FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_realms_actions_update_time_trigger already exists. Ignoring...';
END$$;

-- Step 1: Migrate existing realm_id data to the new brigadier_realms table.
INSERT INTO :"schema_name".brigadier_realms (brigade_id, realm_id, featured, draft)
SELECT
        brigade_id, realm_id, True, False
FROM
        :"schema_name".brigadiers_ids
WHERE
        deleted_at IS NULL
AND 
        purged_at IS NULL
ON CONFLICT DO NOTHING;

-- Step 2: Migrate existing realm_id data to the new brigadier_realms_actions table.

-- All current brigadiers are assigned to the realm with fake "order -> compose -> modify" track.
INSERT INTO :"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_time, event_name, event_info)
SELECT
        brigade_id, realm_id, created_at, 'order', ''
FROM
        :"schema_name".brigadiers_ids
ON CONFLICT DO NOTHING;

INSERT INTO :"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_time, event_name, event_info)
SELECT
        brigade_id, realm_id, created_at, 'compose', ''
FROM
        :"schema_name".brigadiers_ids
ON CONFLICT DO NOTHING;

INSERT INTO :"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_time, event_name, event_info)
SELECT
        brigade_id, realm_id, created_at, 'modify', 'promote'
FROM
        :"schema_name".brigadiers_ids
ON CONFLICT DO NOTHING;

-- All deleted brigadiers are assigned to the realm with fake "remove" track.
INSERT INTO :"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_time, event_name, event_info)
SELECT
        brigade_id, realm_id, deleted_at, 'remove', ''
FROM
        :"schema_name".brigadiers_ids
WHERE
        deleted_at IS NOT NULL
ON CONFLICT DO NOTHING;

-- All purged brigadiers without delete track are assigned to the realm with fake "remove" track.
INSERT INTO :"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_time, event_name, event_info)
SELECT
        brigade_id, realm_id, purged_at, 'remove', ''
FROM
        :"schema_name".brigadiers_ids
WHERE
        purged_at IS NOT NULL
AND
        deleted_at IS NULL
ON CONFLICT DO NOTHING;


-- Step 3: Add the realm_id column to the delete_brigadiers table.
ALTER TABLE :"schema_name".deleted_brigadiers ADD COLUMN IF NOT EXISTS realm_id uuid DEFAULT NULL;

UPDATE :"schema_name".deleted_brigadiers 
SET 
        realm_id = b.realm_id 
FROM 
        :"schema_name".brigadiers_ids b 
WHERE 
        b.brigade_id = deleted_brigadiers.brigade_id;

ALTER TABLE :"schema_name".deleted_brigadiers ALTER COLUMN realm_id DROP DEFAULT;
ALTER TABLE :"schema_name".deleted_brigadiers ALTER COLUMN realm_id SET NOT NULL;


-- Step 5: Optionally drop the realm_id column from the brigadiers_ids table.
ALTER TABLE :"schema_name".brigadiers_ids DROP COLUMN realm_id;
ALTER TABLE :"schema_name".brigadiers DROP COLUMN realm_id;

COMMIT;
