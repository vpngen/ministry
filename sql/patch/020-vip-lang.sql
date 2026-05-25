BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch('020-vip-lang', ARRAY['001-init', '002-roles', '003-patch', '004-realms','005-updates', '006-split-realms', '007-split-partners', '008-utm','009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2', '018-roles', '019-vipmsg']);

ALTER TABLE :"schema_name".vip_telegram_ids
    ADD COLUMN IF NOT EXISTS lang text NOT NULL DEFAULT 'ru';

COMMIT;
