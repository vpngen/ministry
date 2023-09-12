BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch( '003-patch' , ARRAY['001-init', '002-roles']);

CREATE  FUNCTION update_brigadiers_ids_update_time()
RETURNS TRIGGER AS $$
BEGIN
    NEW.update_time = NOW() AT TIME ZONE 'UTC';
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_brigadiers_ids_update_time_trigger
    BEFORE UPDATE
    ON
        :"schema_name".brigadiers_ids
    FOR EACH ROW
EXECUTE PROCEDURE update_brigadiers_ids_update_time();

CREATE OR REPLACE FUNCTION update_brigadiers_ids_purged_at()
RETURNS TRIGGER AS $$
BEGIN
  UPDATE head.brigadiers_ids SET purged_at = NOW() AT TIME ZONE 'UTC' WHERE brigade_id = OLD.brigade_id;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_brigadiers_ids_purged_at_trigger
AFTER DELETE ON :"schema_name".brigadiers
FOR EACH ROW
EXECUTE FUNCTION update_brigadiers_ids_purged_at();


CREATE OR REPLACE FUNCTION update_deleted_at_on_deleted_insert()
RETURNS TRIGGER AS $$
BEGIN
  UPDATE head.brigadiers_ids SET deleted_at = NEW.deleted_at, reason = NEW.reason WHERE brigade_id = NEW.brigade_id;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_deleted_at_on_deleted_insert_trigger
AFTER INSERT ON :"schema_name".deleted_brigadiers
FOR EACH ROW
EXECUTE FUNCTION update_deleted_at_on_deleted_insert();

CREATE OR REPLACE FUNCTION add_brigade_id_to_ids()
RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO head.brigadiers_ids (brigade_id,realm_id,partner_id,created_at)
  VALUES (NEW.brigade_id,NEW.realm_id,NEW.partner_id,NEW.created_at);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER add_brigade_id_to_ids_trigger
AFTER INSERT ON :"schema_name".brigadiers
FOR EACH ROW
EXECUTE FUNCTION add_brigade_id_to_ids();


CREATE OR REPLACE FUNCTION create_brigadier(brigadier text, person json, realm_id uuid, partner_id uuid)
RETURNS void AS $$
DECLARE
  new_id uuid;
  created_at timestamp without time zone;
BEGIN
  new_id := gen_random_uuid();
  created_at := now() AT TIME ZONE 'UTC';

  WHILE (SELECT count(*) FROM head.brigadiers_ids WHERE brigade_id = new_id) > 0 LOOP
    new_id := gen_random_uuid();
  END LOOP;

  INSERT INTO head.brigadiers (brigade_id, brigadier, person, realm_id,partner_id,created_at)
  VALUES (new_id, brigadier, person, realm_id,partner_id,created_at);
END;
$$ LANGUAGE plpgsql;

COMMIT;
