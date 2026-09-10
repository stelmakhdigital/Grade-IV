-- Схема «Грейд» — PostgreSQL (prod). Метки времени — TEXT (RFC3339): единообразие с sqlite.
CREATE TABLE IF NOT EXISTS users (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  grade TEXT NOT NULL,
  stack TEXT NOT NULL,
  stage TEXT NOT NULL DEFAULT 'voice',
  status TEXT NOT NULL DEFAULT 'active',
  duration_limit_s INTEGER NOT NULL,
  active_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
  paused_at TEXT,
  started_at TEXT NOT NULL,
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS session_events (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  session_id BIGINT NOT NULL REFERENCES sessions(id),
  seq INTEGER NOT NULL,
  ts TEXT NOT NULL,
  kind TEXT NOT NULL,
  data TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_events_session_seq ON session_events(session_id, seq);

CREATE TABLE IF NOT EXISTS submissions (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  session_id BIGINT NOT NULL REFERENCES sessions(id),
  task_id TEXT,
  files TEXT NOT NULL,
  action TEXT NOT NULL DEFAULT 'test',
  exit_code INTEGER,
  stdout TEXT NOT NULL DEFAULT '',
  stderr TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER,
  tests TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_submissions_session ON submissions(session_id);

CREATE TABLE IF NOT EXISTS whiteboards (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  session_id BIGINT NOT NULL UNIQUE REFERENCES sessions(id),
  state TEXT NOT NULL,
  blocks TEXT,
  png_path TEXT,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS reports (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  session_id BIGINT NOT NULL UNIQUE REFERENCES sessions(id),
  overall DOUBLE PRECISION NOT NULL DEFAULT 0,
  grade_recommendation TEXT NOT NULL DEFAULT '',
  criteria TEXT NOT NULL DEFAULT '{}',
  strengths TEXT NOT NULL DEFAULT '[]',
  weaknesses TEXT NOT NULL DEFAULT '[]',
  recommendations TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS minutes_ledger (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  session_id BIGINT,
  delta_seconds INTEGER NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ledger_user ON minutes_ledger(user_id);
