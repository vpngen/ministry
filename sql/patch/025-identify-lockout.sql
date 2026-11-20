BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch('025-identify-lockout', ARRAY['001-init', '002-roles', '003-patch', '004-realms', '005-updates', '006-split-realms', '007-split-partners', '008-utm', '009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2', '018-roles', '019-vipmsg', '020-vip-lang', '021-free-telegram-ids', '022-push-messages', '023-vip-upgrade-notify', '024-key-tier']);

-- IP-keyed equivalent of embassy-tgbot's Badger-based, chatID-keyed VIP
-- identify lockout (vip_identify_lockout.go), for ministry-api's stateless
-- POST /identify - progressive lockout (1h / 12h / 24h) applied by the
-- handler based on fail_count, mirroring vipIdentifyLockDurationForCount.
CREATE TABLE IF NOT EXISTS :"schema_name".identify_attempts (
        client_ip                       inet NOT NULL,
        fail_count                       integer NOT NULL DEFAULT 0,
        locked_until                     timestamp without time zone DEFAULT NULL,
        update_time                      timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        PRIMARY KEY (client_ip)
);

DO $$
BEGIN
    CREATE TRIGGER identify_attempts_update_time_trigger BEFORE INSERT OR UPDATE ON "head".identify_attempts FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger identify_attempts_update_time_trigger already exists. Ignoring...';
END$$;

GRANT
        SELECT,UPDATE,INSERT,DELETE
ON
        :"schema_name".identify_attempts
TO
        :"head_vpnapi_dbuser";

-- Maps a signed browser-session JWT's jti (POST /session/browser) to the
-- brigade_id it was issued for. KeydeskTokenClaims (github.com/vpngen/keydesk
-- pkg/jwt) has no free-form subject field for this, so the binding lives
-- here instead of in the token itself; expires_at mirrors the token's own
-- exp so an expired row is simply a lookup miss.
CREATE TABLE IF NOT EXISTS :"schema_name".browser_sessions (
        jti                              uuid NOT NULL,
        brigade_id                       uuid NOT NULL,
        expires_at                       timestamp without time zone NOT NULL,
        update_time                      timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (jti)
);

DO $$
BEGIN
    CREATE TRIGGER browser_sessions_update_time_trigger BEFORE INSERT OR UPDATE ON "head".browser_sessions FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger browser_sessions_update_time_trigger already exists. Ignoring...';
END$$;

GRANT
        SELECT,UPDATE,INSERT,DELETE
ON
        :"schema_name".browser_sessions
TO
        :"head_vpnapi_dbuser";

COMMIT;
