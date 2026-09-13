// Package interviewer — движок ИИ-интервьюера (WP-5, ADR-005): персоны/промпты по стадиям
// и грейдам, контекст из транскрипта (session_events), ходы диалога через LLM-провайдер.
//
// Бессостойный к рестарту: контекст — всегда из БД (сессия + последние события),
// состояние не хранится в памяти.
package interviewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// Ошибки.
var (
	ErrSessionEnded = errors.New("сессия завершена")
	ErrWrongStage   = errors.New("ход не соответствует стадии")
)

// Option — параметр движка.
type Option func(*Interviewer)

// WithMaxHistory — сколько последних реплик класть в контекст (default 20).
func WithMaxHistory(n int) Option {
	return func(i *Interviewer) {
		if n > 0 {
			i.maxHistory = n
		}
	}
}

// Interviewer — ходы ИИ-интервьюера.
type Interviewer struct {
	llm        llm.Provider
	sessions   *db.SessionStore
	log        *slog.Logger
	maxHistory int
}

// New создаёт движок интервьюера.
func New(provider llm.Provider, sessions *db.SessionStore, log *slog.Logger, opts ...Option) *Interviewer {
	i := &Interviewer{llm: provider, sessions: sessions, log: log, maxHistory: 20}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// Provider — активный LLM-провайдер (health/тесты).
func (i *Interviewer) Provider() llm.Provider { return i.llm }

// OnUserUtterance — реплика кандидата → ответ ИИ. Событие ai_utterance пишется
// только при успешном ответе. Возвращает текст ответа (для ai_text по WS).
func (i *Interviewer) OnUserUtterance(ctx context.Context, sessionID int64, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("пустая реплика")
	}
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()

	sess, msgs, err := i.context(ctx, sessionID, text)
	if err != nil {
		return "", err
	}
	resp, err := i.llm.Chat(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.7,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
		"text": resp.Content, "stage": sess.Stage,
	})
	return resp.Content, nil
}

// OnStageChanged — вход на стадию: первый вопрос/представление (voice/livecode/design).
func (i *Interviewer) OnStageChanged(ctx context.Context, sessionID int64, stageNote string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()

	sess, msgs, err := i.context(ctx, sessionID,
		"Кандидат перешёл на эту стадию. Заведи её: представь формат и задай первый вопрос (или, для Live-Code, объяви задачу).")
	if err != nil {
		return "", err
	}
	if stageNote != "" {
		msgs[len(msgs)-1].Content += "\n" + stageNote
	}
	resp, err := i.llm.Chat(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.7,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
		"text": resp.Content, "stage": sess.Stage, "note": "stage_entry",
	})
	return resp.Content, nil
}

// OnCodeRun — результаты запуска кода (Live-Code) → ревью + follow-up.
func (i *Interviewer) OnCodeRun(ctx context.Context, sessionID int64, summary map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()

	sess, err := i.sessions.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if session.IsTerminalStatus(sess.Status) {
		return "", ErrSessionEnded
	}
	if sess.Stage != models.StageLiveCode {
		return "", ErrWrongStage
	}

	hint := ""
	if s, ok := summary["hint"].(string); ok {
		hint = s
	}
	raw, _ := json.Marshal(summary)
	msgs := i.buildMessages(sess, "Кандидат запустил код. Результаты (JSON): "+string(raw)+"\n"+hint)
	resp, err := i.llm.Chat(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.5,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
		"text": resp.Content, "stage": sess.Stage, "note": "code_review",
	})
	return resp.Content, nil
}

// Nudge — кандидат молчит больше порога: ИИ сам заполняет паузу (решение #7).
func (i *Interviewer) Nudge(ctx context.Context, sessionID int64, silentS int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()

	sess, msgs, err := i.context(ctx, sessionID,
		fmt.Sprintf("Кандидат молчит уже %d секунд. Мягко заполни паузу: уточни, всё ли понятно, "+
			"повтори последний вопрос или дай лёгкую подсказку (1–2 предложения).", silentS))
	if err != nil {
		return "", err
	}
	resp, err := i.llm.Chat(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.7,
		MaxTokens:   120,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_nudge", map[string]any{"text": resp.Content, "stage": sess.Stage})
	return resp.Content, nil
}

// CodeRunHint — текстовая сводка результатов запуска для LLM-контекста.
func CodeRunHint(passed bool, exitCode, durationMS, testsTotal, testsFailed int) string {
	return runReviewHint(passed, exitCode, durationMS, testsTotal, testsFailed)
}

// context — сессия + сообщения (system + транскрипт + текущий ход).
func (i *Interviewer) context(ctx context.Context, sessionID int64, userMsg string) (models.Session, []llm.Message, error) {
	sess, err := i.sessions.Get(ctx, sessionID)
	if err != nil {
		return models.Session{}, nil, err
	}
	if session.IsTerminalStatus(sess.Status) {
		return models.Session{}, nil, ErrSessionEnded
	}
	msgs := i.buildMessages(sess, userMsg)
	return sess, msgs, nil
}

// buildMessages — system-промпт + последние реплики + текущий ход.
func (i *Interviewer) buildMessages(sess models.Session, userMsg string) []llm.Message {
	msgs := []llm.Message{{Role: llm.RoleSystem, Content: SystemPrompt(sess.Grade, string(sess.Stack), sess.Stage)}}
	msgs = append(msgs, i.transcript(sess.ID, i.maxHistory)...)
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: userMsg})
	return msgs
}

// transcript — последние реплики (user_utterance/ai_utterance/ai_nudge) в хронологии.
func (i *Interviewer) transcript(sessionID int64, max int) []llm.Message {
	ctx := context.Background()
	events, err := i.sessions.ListEvents(ctx, sessionID)
	if err != nil {
		i.log.Debug("interviewer: события не прочитаны", "session", sessionID, "err", err)
		return nil
	}
	type line struct {
		who  string
		text string
		seq  int
	}
	var lines []line
	for _, ev := range events {
		var d struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(ev.Data, &d); err != nil || d.Text == "" {
			continue
		}
		switch ev.Kind {
		case "user_utterance":
			lines = append(lines, line{who: "candidate", text: d.Text, seq: ev.Seq})
		case "ai_utterance", "ai_nudge":
			lines = append(lines, line{who: "interviewer", text: d.Text, seq: ev.Seq})
		}
	}
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	msgs := make([]llm.Message, 0, len(lines))
	for _, l := range lines {
		role := llm.RoleUser
		if l.who == "interviewer" {
			role = llm.RoleAssistant
		}
		msgs = append(msgs, llm.Message{Role: role, Content: l.text})
	}
	return msgs
}

// saveEvent — событие сессии (ошибки — только в лог: ход уже сформирован).
func (i *Interviewer) saveEvent(ctx context.Context, sessionID int64, kind string, data map[string]any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	if _, err := i.sessions.AddEvent(ctx, sessionID, kind, raw); err != nil {
		i.log.Warn("interviewer: событие не записано", "session", sessionID, "kind", kind, "err", err)
	}
}
