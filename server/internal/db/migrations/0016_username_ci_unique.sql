-- 0016_username_ci_unique.sql — panel usernames are unique regardless of case.
--
-- admin_user.username has a case-sensitive UNIQUE constraint, so "Admin" and
-- "admin" could coexist; the API has refused that on create since 0015, but only
-- in application code. This makes it a schema guarantee.
--
-- A live database may already hold names that differ only by case. Creating the
-- index there would fail and stop the server from starting, so the index is
-- created only when no such pair exists. When it is skipped, the server repeats
-- the check at every start (auth.EnsureUsernameIndex), names the clashing
-- accounts in its log, and creates the index once they are resolved.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM admin_user GROUP BY lower(username) HAVING count(*) > 1) THEN
    RAISE WARNING 'admin_user holds usernames that differ only by case; admin_user_username_lower_idx was not created';
  ELSE
    CREATE UNIQUE INDEX IF NOT EXISTS admin_user_username_lower_idx ON admin_user (lower(username));
  END IF;
END
$$;
