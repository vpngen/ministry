BEGIN;

ALTER TABLE :"schema_name".brigadiers RENAME COLUMN create_at TO created_at;

COMMIT;
