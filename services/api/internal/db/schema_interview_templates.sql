-- Таблица шаблонов интервью (настраиваемые планы под грейды).
-- Создана в 2026-10-09 (решение: настраиваемые review-планы без HR-кабинета).
-- Блоки — JSON-массив: [{title, focus, question_count}].
CREATE TABLE IF NOT EXISTS interview_templates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  grade TEXT NOT NULL,
  stack TEXT NOT NULL,
  track TEXT NOT NULL DEFAULT 'backend',
  duration_s INTEGER NOT NULL,
  blocks TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_templates_grade_stack ON interview_templates(grade, stack);
CREATE INDEX IF NOT EXISTS idx_templates_track_grade ON interview_templates(track, grade);

-- Сессия ссылается на шаблон (NULL — дефолтный для грейда, до миграции).
ALTER TABLE sessions ADD COLUMN template_id INTEGER REFERENCES interview_templates(id);

ALTER TABLE sessions ADD COLUMN program TEXT NOT NULL DEFAULT '';

-- spec-18: трек собеседования для шаблона (backend — дефолт совместимости).
ALTER TABLE interview_templates ADD COLUMN track TEXT NOT NULL DEFAULT 'backend';
