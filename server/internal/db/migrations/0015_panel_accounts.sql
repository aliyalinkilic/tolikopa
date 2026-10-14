-- 0015_panel_accounts.sql — panel account self-service, two-factor, session
-- revocation and instance-wide settings.
--
-- admin_user is the PANEL account table (who may sign in here). It has nothing to
-- do with the monitored domain users, whose column is logon_user everywhere.

-- Profile.
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS display_name text NOT NULL DEFAULT '';
-- Per-user UI preferences: {"theme": "dark"|"light"|"system", "lang": "tr"|"en"}.
-- A missing key means "use the default" (theme: system, lang: instance default).
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS prefs jsonb NOT NULL DEFAULT '{}';
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS last_login_at timestamptz;

-- Session revocation. The value is embedded in every session JWT; a token whose
-- epoch differs from the row is rejected. Incremented on password change/reset,
-- on disable and on 2FA enrolment, which signs the user out everywhere at once.
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS session_epoch int NOT NULL DEFAULT 0;

-- Two-factor (TOTP, RFC 6238). Secrets are AES-256-GCM ciphertext
-- (nonce || ciphertext) under a key derived from JWT_SECRET — never plaintext.
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS totp_secret_enc  bytea;   -- active secret
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS totp_pending_enc bytea;   -- enrolment in progress, not yet verified
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS totp_enabled     boolean NOT NULL DEFAULT false;
-- Last accepted time step (unix seconds / 30). A code for a step <= this value is
-- refused, so a code cannot be replayed inside its validity window.
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS totp_last_step   bigint NOT NULL DEFAULT 0;
-- Unused recovery codes as hex HMAC-SHA256 digests (keyed, never the code
-- itself). A code is removed from the array when it is used.
ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS recovery_codes   text[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN admin_user.session_epoch IS
  'Embedded in session JWTs; bump to invalidate every session of the user.';
COMMENT ON COLUMN admin_user.totp_secret_enc IS
  'AES-256-GCM(nonce||ciphertext) of the TOTP secret, key derived from JWT_SECRET. Rotating JWT_SECRET voids it.';
COMMENT ON COLUMN admin_user.recovery_codes IS
  'Hex HMAC-SHA256 digests of the unused recovery codes. Single use: consumed entries are removed.';

-- Instance-wide settings, one row per key. Small and read rarely; not a place
-- for secrets (GET /api/settings/public serves some keys without authentication).
CREATE TABLE IF NOT EXISTS app_setting (
  key        text PRIMARY KEY,
  value      jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO app_setting (key, value) VALUES
  ('org_name',     '""'::jsonb),
  ('default_lang', '"tr"'::jsonb),
  ('require_2fa',  'false'::jsonb)
ON CONFLICT (key) DO NOTHING;
