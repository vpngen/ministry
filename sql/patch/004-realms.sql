BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch( '004-realms' , ARRAY['001-init', '002-roles', '003-patch']);

ALTER TABLE :"schema_name".realms ADD COLUMN open_for_regs bool NOT NULL DEFAULT true;
ALTER TABLE :"schema_name".partners ADD COLUMN open_for_regs bool NOT NULL DEFAULT true;

ALTER TABLE :"schema_name".realms ALTER COLUMN open_for_regs DROP DEFAULT;
ALTER TABLE :"schema_name".partners ALTER COLUMN open_for_regs DROP DEFAULT;

COMMIT;
