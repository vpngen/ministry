BEGIN;

SELECT _v.assert_user_is_superuser();
SELECT _v.register_patch('024-key-tier', ARRAY['001-init', '002-roles', '003-patch', '004-realms', '005-updates', '006-split-realms', '007-split-partners', '008-utm', '009-roles', '010-utm', '011-fixes', '012-roles', '013-utmnew', '014-utmnew2', '015-utmnew3', '016-vip', '017-vip2', '018-roles', '019-vipmsg', '020-vip-lang', '021-free-telegram-ids', '022-push-messages', '023-vip-upgrade-notify']);

-- Per-brigade key tier state (Free / Pro Basic / Pro Unlim). Same granularity
-- as brigadier_vip: there is no per-key concept in the schema today, so a
-- brigade's "Free key" upgrade maps to one row per brigade_id.
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_key_tier (
        brigade_id                      uuid NOT NULL,
        tier                             text NOT NULL DEFAULT 'free',
        socket_key_id                    text DEFAULT NULL,
        pending_deactivation             boolean NOT NULL DEFAULT false,
        deactivate_at                    timestamp without time zone DEFAULT NULL,
        update_time                      timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (brigade_id),
        CHECK (tier IN ('free', 'pro_basic', 'pro_unlim'))
);

DO $$
BEGIN
    CREATE TRIGGER brigadier_key_tier_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_key_tier FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_key_tier_update_time_trigger already exists. Ignoring...';
END$$;

-- Supports the future 3-day-deactivation cron scan without a full table scan.
CREATE INDEX IF NOT EXISTS brigadier_key_tier_pending_deactivation_idx ON :"schema_name".brigadier_key_tier (deactivate_at) WHERE pending_deactivation = true;

GRANT
        SELECT,UPDATE,INSERT,DELETE
ON
        :"schema_name".brigadier_key_tier
TO
        :"head_vpnapi_dbuser";

-- Upgrade-in-progress state. The two partial unique indexes below are the
-- idempotency mechanism: a redelivered billing webhook (same billing_event_id)
-- can never insert a second row, and a brigade can never have two concurrent
-- non-terminal upgrades (also guards against Mini App double-tap today).
CREATE TABLE IF NOT EXISTS :"schema_name".brigadier_key_upgrade_requests (
        id                               uuid NOT NULL DEFAULT gen_random_uuid(),
        brigade_id                      uuid NOT NULL,
        from_tier                       text NOT NULL,
        to_tier                          text NOT NULL,
        status                           text NOT NULL DEFAULT 'requested',
        billing_event_id                 text DEFAULT NULL,
        socket_key_id                    text DEFAULT NULL,
        created_time                     timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        update_time                      timestamp without time zone NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
        FOREIGN KEY (brigade_id)        REFERENCES :"schema_name".brigadiers_ids (brigade_id),
        PRIMARY KEY (id),
        CHECK (to_tier IN ('pro_basic', 'pro_unlim')),
        CHECK (status IN ('requested', 'quoted', 'awaiting_payment', 'payment_confirmed', 'key_issued', 'completed', 'failed', 'cancelled'))
);

DO $$
BEGIN
    CREATE TRIGGER brigadier_key_upgrade_requests_update_time_trigger BEFORE INSERT OR UPDATE ON "head".brigadier_key_upgrade_requests FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Trigger brigadier_key_upgrade_requests_update_time_trigger already exists. Ignoring...';
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS brigadier_key_upgrade_requests_billing_event_idx ON :"schema_name".brigadier_key_upgrade_requests (billing_event_id) WHERE billing_event_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS brigadier_key_upgrade_requests_open_idx ON :"schema_name".brigadier_key_upgrade_requests (brigade_id) WHERE status NOT IN ('completed', 'failed', 'cancelled');

GRANT
        SELECT,UPDATE,INSERT,DELETE
ON
        :"schema_name".brigadier_key_upgrade_requests
TO
        :"head_vpnapi_dbuser";

-- Every existing caller looks free_telegram_ids up by brigade_id; Mini App
-- auth needs the reverse direction (telegram_id -> brigade_id) and has no
-- index for it today.
CREATE INDEX IF NOT EXISTS free_telegram_ids_telegram_id_idx ON :"schema_name".free_telegram_ids (telegram_id);

COMMIT;
