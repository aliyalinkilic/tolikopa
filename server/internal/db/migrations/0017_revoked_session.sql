-- 0017_revoked_session.sql — sign-out invalidates the session token itself.
--
-- Session JWTs are stateless, so clearing the cookie on logout used to leave a
-- copied token valid until it expired (12 h). Logout now records the token's id
-- (jti) here and authentication refuses any token listed. Rows are needed only
-- until the token would have expired anyway; logout deletes expired rows.
CREATE TABLE IF NOT EXISTS revoked_session (
  jti        text        PRIMARY KEY,
  expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS revoked_session_expires_idx ON revoked_session (expires_at);
