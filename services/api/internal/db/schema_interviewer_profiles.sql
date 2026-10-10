-- Профили интервьюера (tone × difficulty, Итерация B) — SQLite (dev).
-- Postgres-вариант (BIGINT, BOOLEAN) — в db.go (schemaPostgresProfiles).
CREATE TABLE IF NOT EXISTS interviewer_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  tone TEXT NOT NULL,        -- strict | balanced | supportive | playful | socratic
  difficulty TEXT NOT NULL,  -- minus | standard | plus
  is_preset INTEGER NOT NULL DEFAULT 0,
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_profiles_preset ON interviewer_profiles(is_preset);
