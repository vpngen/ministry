BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch('023-vip-upgrade-notify', ARRAY['001-init', '002-roles', '003-patch', '004-realms', '005-updates', '006-split-realms', '007-split-partners', '008-utm', '009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2', '018-roles', '019-vipmsg', '020-vip-lang', '021-free-telegram-ids', '022-push-messages']);

-- Marks a vip_messages row as a plain "your brigade is now VIP" notification
-- (existing brigade upgraded in place via viparize), as opposed to a full
-- grant message carrying a freshly generated vpnconfig.
ALTER TABLE :"schema_name".vip_messages ADD COLUMN vip_upgrade_notify boolean NOT NULL DEFAULT false;

COMMIT;
