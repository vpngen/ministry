BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '016-vip', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3']);


-- Realms reference table.
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_vip (
        brigade_id                      uuid NOT NULL,
        vip_expire                      timestamp without time zone DEFAULT NULL,
        finalizer                       boolean NOT NULL DEFAULT false,
        update_time                     timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (brigade_id)
);

DO $$
BEGIN
    CREATE TRIGGER brigadier_partners_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_vip FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_vip_update_time_trigger already exists. Ignoring...';
END$$;

CREATE INDEX brigadier_vip_variant_idx ON :"schema_name".brigadier_vip (vip_variant);

-- Partners actions reference table.
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_vip_actions (
        brigade_id          uuid NOT NULL,
        event_time          timestamp without time zone NOT NULL,
        event_name          text NOT NULL, -- 'begin', 'end'
        event_info          text NOT NULL,
        update_time         timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id) REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (brigade_id, event_time)
);

DO $$
BEGIN
    CREATE TRIGGER brigadier_partners_actions_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_vip_actions FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_vip_actions_update_time_trigger already exists. Ignoring...';
END$$;

COMMIT;
