BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '014-utmnew2', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew']);

ALTER TABLE :"schema_name".start_labels ADD COLUMN created_at TIMESTAMP WITHOUT TIME ZONE NULL;
UPDATE :"schema_name".start_labels SET created_at = brigadiers_ids.created_at FROM :"schema_name".brigadiers_ids WHERE start_labels.brigade_id = brigadiers_ids.brigade_id;

CREATE INDEX start_labels_created_at_idx ON :"schema_name".start_labels (created_at);

COMMIT;
