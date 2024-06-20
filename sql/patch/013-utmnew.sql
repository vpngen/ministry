BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '013-utmnew', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes', '012-roles']);

ALTER TABLE :"schema_name".start_labels DROP CONSTRAINT start_labels_pkey;
ALTER TABLE :"schema_name".start_labels ADD COLUMN label_id UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE :"schema_name".start_labels ADD CONSTRAINT start_labels_pkey PRIMARY KEY (label_id);
ALTER TABLE :"schema_name".start_labels ADD COLUMN first_visit TIMESTAMP WITHOUT TIME ZONE DEFAULT NULL;

UPDATE :"schema_name".start_labels SET first_visit = created_at FROM :"schema_name".brigadiers WHERE start_labels.brigade_id = brigadiers.brigade_id;

ALTER TABLE :"schema_name".start_labels ALTER COLUMN first_visit SET NOT NULL;

CREATE INDEX start_labels_first_visit_idx ON :"schema_name".start_labels (first_visit);

COMMIT;
