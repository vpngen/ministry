BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '008-utm', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners']);

-- Realms reference table.
CREATE TABLE IF NOT EXISTS :"schema_name".start_labels (
        brigade_id                      uuid NOT NULL,
        label                           varchar(64) NOT NULL,
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (brigade_id)
);

COMMIT;
