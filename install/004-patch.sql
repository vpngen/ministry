BEGIN;

CREATE TABLE library.brigadiers_ids (
  brigade_id uuid NOT NULL PRIMARY KEY,
  realm_id uuid NOT NULL,
  partner_id uuid NOT NULL,
  reason text NOT NULL DEFAULT '',
  created_at timestamp without time zone NOT NULL,
  deleted_at timestamp without time zone DEFAULT NULL,
  purged_at timestamp without time zone DEFAULT NULL,
  update_time timestamp without time zone NOT NULL DEFAULT NOW()
);

CREATE  FUNCTION update_brigadiers_ids_update_time()
RETURNS TRIGGER AS $$
BEGIN
    NEW.update_time = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_brigadiers_ids_update_time_trigger
    BEFORE UPDATE
    ON
        library.brigadiers_ids
    FOR EACH ROW
EXECUTE PROCEDURE update_brigadiers_ids_update_time();

CREATE OR REPLACE FUNCTION update_brigadiers_ids_purged_at()
RETURNS TRIGGER AS $$
BEGIN
  UPDATE library.brigadiers_ids SET purged_at = NOW() WHERE brigade_id = OLD.brigade_id;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_brigadiers_ids_purged_at_trigger
AFTER DELETE ON library.brigadiers
FOR EACH ROW
EXECUTE FUNCTION update_brigadiers_ids_purged_at();


CREATE OR REPLACE FUNCTION update_deleted_at_on_deleted_insert()
RETURNS TRIGGER AS $$
BEGIN
  UPDATE library.brigadiers_ids SET deleted_at = NEW.deleted_at, reason = NEW.reason WHERE brigade_id = NEW.brigade_id;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_deleted_at_on_deleted_insert_trigger
AFTER INSERT ON library.deleted_brigadiers
FOR EACH ROW
EXECUTE FUNCTION update_deleted_at_on_deleted_insert();

CREATE OR REPLACE FUNCTION add_brigade_id_to_ids()
RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO library.brigadiers_ids (brigade_id,realm_id,partner_id,created_at)
  VALUES (NEW.brigade_id,NEW.realm_id,NEW.partner_id,NEW.created_at);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER add_brigade_id_to_ids_trigger
AFTER INSERT ON library.brigadiers
FOR EACH ROW
EXECUTE FUNCTION add_brigade_id_to_ids();


CREATE OR REPLACE FUNCTION create_brigadier(brigadier text, person json, realm_id uuid, partner_id uuid)
RETURNS void AS $$
DECLARE
  new_id uuid;
  created_at timestamp without time zone;
BEGIN
  new_id := gen_random_uuid();
  created_at := now();

  WHILE (SELECT count(*) FROM library.brigadiers_ids WHERE brigade_id = new_id) > 0 LOOP
    new_id := gen_random_uuid();
  END LOOP;

  INSERT INTO library.brigadiers (brigade_id, brigadier, person, realm_id,partner_id,created_at)
  VALUES (new_id, brigadier, person, realm_id,partner_id,created_at);
END;
$$ LANGUAGE plpgsql;

COMMIT;
