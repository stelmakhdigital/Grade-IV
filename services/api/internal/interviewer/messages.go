// Сборка контекста хода: сессия + system-промпт (профиль, вакансия, резюме) +
// транскрипт последних реплик + событие сессии по итогам хода.
package interviewer

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

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

// profilePrompt — tone/difficulty сессии по профилю (0/ошибка — default).
// Профиль влияет только на стиль, не на оценку.
func (i *Interviewer) profilePrompt(sess models.Session) (tone, difficulty string) {
	tone, difficulty = "balanced", "standard"
	if i.profiles == nil || sess.ProfileID <= 0 {
		return tone, difficulty
	}
	if p, err := i.profiles.Get(context.Background(), sess.ProfileID); err == nil {
		return p.Tone, p.Difficulty
	}
	return tone, difficulty
}

// buildMessages — system-промпт (с профилем) + последние реплики + текущий ход.
func (i *Interviewer) buildMessages(sess models.Session, userMsg string, images ...string) []llm.Message {
	tone, difficulty := i.profilePrompt(sess)
	// F7: род речи по выбранному голосу (speaker → gender); пусто/неизвестно → male.
	gender := ""
	if i.genderResolver != nil {
		gender = i.genderResolver(sess.TTSSpeaker)
	}
	// F6: блок вакансии кандидата (из профиля); пусто → без блока.
	vacancy := ""
	if i.vacancyResolver != nil {
		vacancy = i.vacancyResolver(context.Background(), sess.UserID)
	}
	// Резюме: блок профиля + уровень интервью. В system-промпт каждого голоса хода,
	// поэтому срезается коротко — иначе деградирует SLO голосового контура.
	resume := ""
	if i.resumeResolver != nil {
		resume = clampRunes(i.resumeResolver(context.Background(), sess.UserID), resumePromptRunes)
	}
	// Заявленные языки — отдельной строкой НАД резюме (после обрезки, чтобы её
	// не отрезало). Без неё модель читает резюме и спрашивает по одному языку:
	// кандидату с go+python доставался только один (замер 2026-11-26).
	if line := declaredStacksLine(sess.ResumeStacks); line != "" {
		resume = line + "\n" + resume
	}
	// Программа — из сессии (шаблон, итерация A); пустая — дефолтная gradeProgram.
	// spec-18: контекст трека (роль, доска) в промптах стадии.
	sys := SystemPromptWithProfileProgram(sess.Grade, string(sess.Stack), sess.Stage,
		tone, difficulty, sess.Program, gender, vacancy, resume, sess.BaseGrade, i.trackFor(sess))
	msgs := []llm.Message{{Role: llm.RoleSystem, Content: sys}}
	msgs = append(msgs, i.transcript(sess.ID, i.maxHistory)...)
	msg := llm.Message{Role: llm.RoleUser, Content: userMsg}
	if len(images) > 0 {
		msg.Images = images
	}
	msgs = append(msgs, msg)
	return msgs
}

// declaredStacksLine — строка директивы о языках, заявленных в резюме.
// Пусто — строки нет (интервью не по резюме).
func declaredStacksLine(st []models.Stack) string {
	if len(st) == 0 {
		return ""
	}
	names := make([]string, len(st))
	for i, s := range st {
		names[i] = string(s)
	}
	return "Языки кандидата по резюме: " + strings.Join(names, ", ") +
		". Ставь вопросы по каждому заявленному, а не только по первому."
}

// transcript — последние реплики (user_utterance/ai_utterance/ai_nudge) в хронологии.
// spec-14/A6: ListEventsTail вместо ListEvents — на каждый LLM-ход не читаем
// всю историю событий (длинные интервью — сотни событий).
func (i *Interviewer) transcript(sessionID int64, max int) []llm.Message {
	ctx := context.Background()
	events, err := i.sessions.ListEventsTail(ctx, sessionID, max*3)
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
