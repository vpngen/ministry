BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch(  '015-utmnew3', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2']);

ALTER TABLE head.start_labels ALTER COLUMN brigade_id DROP NOT NULL;
ALTER TABLE :"schema_name".start_labels ADD COLUMN partner_id UUID DEFAULT NULL REFERENCES :"schema_name".partners(partner_id);

WITH last_partner_actions AS (
        SELECT 
                bpa.brigade_id, MAX(bpa.event_time) AS max_event_time
        FROM 
                :"schema_name".brigadier_partners_actions bpa
        JOIN 
                :"schema_name".start_labels l ON bpa.brigade_id = l.brigade_id
        WHERE
                bpa.event_name = 'assign'
        GROUP BY
                bpa.brigade_id
), partners_ref AS (
        SELECT 
                bpa.brigade_id, p.partner_id
        FROM 
                :"schema_name".brigadier_partners_actions bpa
        JOIN 
                last_partner_actions lpa ON bpa.brigade_id = lpa.brigade_id AND bpa.event_time = lpa.max_event_time
        JOIN
                :"schema_name".partners p ON bpa.partner_id = p.partner_id
)
UPDATE 
        :"schema_name".start_labels 
SET
        partner_id = partners_ref.partner_id
FROM
        partners_ref
WHERE
        start_labels.brigade_id = partners_ref.brigade_id;

ALTER TABLE :"schema_name".start_labels ALTER COLUMN partner_id SET NOT NULL;

ALTER TABLE :"schema_name".start_labels DROP CONSTRAINT start_labels_pkey;
ALTER TABLE :"schema_name".start_labels ADD CONSTRAINT start_labels_pkey PRIMARY KEY (label_id, partner_id, first_visit);

COMMIT;
