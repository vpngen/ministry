BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch('026-pro-single-tier', ARRAY['001-init', '002-roles', '003-patch', '004-realms', '005-updates', '006-split-realms', '007-split-partners', '008-utm', '009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2', '018-roles', '019-vipmsg', '020-vip-lang', '021-free-telegram-ids', '022-push-messages', '023-vip-upgrade-notify', '024-key-tier', '025-identify-lockout']);

-- PRO collapses from a two-tier split (pro_basic/pro_unlim) to a single
-- 'pro' tier for this PoC - key-differentiated PRO tiers are a real future
-- feature, not built yet. Migrate existing data before tightening the
-- CHECK constraints so no row is left violating the new, stricter check.
UPDATE :"schema_name".brigadier_key_tier SET tier = 'pro' WHERE tier IN ('pro_basic', 'pro_unlim');
UPDATE :"schema_name".brigadier_key_upgrade_requests SET to_tier = 'pro' WHERE to_tier IN ('pro_basic', 'pro_unlim');
UPDATE :"schema_name".brigadier_key_upgrade_requests SET from_tier = 'pro' WHERE from_tier IN ('pro_basic', 'pro_unlim');

ALTER TABLE :"schema_name".brigadier_key_tier DROP CONSTRAINT brigadier_key_tier_tier_check;
ALTER TABLE :"schema_name".brigadier_key_tier ADD CONSTRAINT brigadier_key_tier_tier_check CHECK (tier IN ('free', 'pro'));

ALTER TABLE :"schema_name".brigadier_key_upgrade_requests DROP CONSTRAINT brigadier_key_upgrade_requests_to_tier_check;
ALTER TABLE :"schema_name".brigadier_key_upgrade_requests ADD CONSTRAINT brigadier_key_upgrade_requests_to_tier_check CHECK (to_tier = 'pro');

COMMIT;
