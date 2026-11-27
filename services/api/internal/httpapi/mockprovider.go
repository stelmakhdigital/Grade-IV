// LLM-мок (LLM_MOCK=1): сборка провайдера-заглушки и заготовленные ответы.
// Отдельный файл: к сборке Server отношения не имеет, живёт рядом с New.

package httpapi

import (
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

// newMockProvider — LLM-мок (LLM_MOCK=1): эхо по умолчанию; детерминированный
// замер (T-20261009001516) — LLM_MOCK_RESPONSE (фиксированный ответ) +
// LLM_MOCK_TOKENS_PER_S (скорость стриминга токенов, эмуляция реального узла).
func newMockProvider(cfg *config.Config) llm.Provider {
	m := llm.NewMockProvider()
	if cfg.LLMMockResponse != "" {
		resp := cfg.LLMMockResponse
		m.SetResponder(func(req llm.Request) (string, error) {
			// Приветствие (system-промпт «поприветствуй») — короткий ответ:
			// ускоряет замер (дренирование greeting), на измеряемый ход не влияет.
			for _, msg := range req.Messages {
				if msg.Role == llm.RoleSystem && strings.Contains(msg.Content, "поприветствуй") {
					return "Привет. Давайте начнём.", nil
				}
			}
			// spec-22: генератор ролей — детерминированный черновик (валидный
			// JSON по схеме), чтобы E2E с LLM_MOCK проверял весь путь.
			for _, msg := range req.Messages {
				if msg.Role == llm.RoleSystem && strings.Contains(msg.Content, "конфигурируешь роль") {
					return trackMockDraft, nil
				}
			}
			return resp, nil
		})
	}
	if cfg.LLMMockTokensPerS > 0 {
		m.SetTokenDelay(time.Duration(float64(time.Second) / cfg.LLMMockTokensPerS))
	}
	return m
}

// trackMockDraft — ответ мока генератора ролей (spec-22): валидный черновик,
// админ правит id/название перед сохранением.
const trackMockDraft = `{
  "title": "Data-инженер",
  "description": "Строит пайплайны данных: сбор, трансформация, загрузка в хранилище. Проверяем моделирование данных и надёжность пайплайнов.",
  "practice": {"runtime": "python"},
  "grades": ["junior", "middle"],
  "board": {
    "title": "Схема пайплайна данных",
    "review_prompt": "Оцени: источники и их контракт, трансформации, хранилище, мониторинг качества данных, идемпотентность и ретраи. Сверяй схему с объяснением.",
    "groups": [
      {"title": "Источники", "names": ["API", "Вебхуки", "Логи", "БД-источник"]},
      {"title": "Обработка", "names": ["Ingest", "Трансформация", "Валидация", "Очередь"]},
      {"title": "Хранилище", "names": ["Data Lake", "SQL-хранилище", "Кэш", "Дашборд"]}
    ],
    "cases": [
      {"grade": "middle", "title": "Ежедневная загрузка заказов", "statement": "Спроектируй ежедневную загрузку заказов из API партнёра в хранилище: источники, трансформации, контроль качества. Объясни, что делать при сбое загрузки.", "checklist": ["контракт источника", "идемпотентная загрузка", "валидация и карантин", "ретраи и алертинг"]}
    ]
  },
  "criteria": [
    {"name": "Моделирование данных", "weight": 0.25, "hint": "структура хранилища и контракты"},
    {"name": "Надёжность пайплайна", "weight": 0.25, "hint": "ретраи, идемпотентность, карантин"},
    {"name": "Практика (качество кода)", "weight": 0.20, "hint": "по запускам кода"},
    {"name": "Коммуникация (ясность, структура)", "weight": 0.15, "hint": "ясность речи и структуры ответов"},
    {"name": "Эксплуатация (мониторинг, алерты)", "weight": 0.15, "hint": "как следит за качеством данных"}
  ]
}`
