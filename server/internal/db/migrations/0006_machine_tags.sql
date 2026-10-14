-- 0006_machine_tags.sql — department/type tags on fleet machines (VM vs real PC, etc.).
ALTER TABLE machine ADD COLUMN IF NOT EXISTS tags text[] NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS machine_tags_idx ON machine USING gin (tags);
