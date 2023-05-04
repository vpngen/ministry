BEGIN;

-- Partners.

ALTER TABLE :"schema_name".brigadiers ADD COLUMN partner_id uuid NOT NULL DEFAULT '1372bb2a-8027-4191-868a-68e898b1116c' REFERENCES :"schema_name".partners (partner_id);
ALTER TABLE :"schema_name".brigadiers ALTER COLUMN partner_id DROP DEFAULT;

COMMIT;