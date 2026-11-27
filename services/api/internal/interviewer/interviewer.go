// Package interviewer — движок ИИ-интервьюера (WP-5, ADR-005): персоны/промпты по стадиям
// и грейдам, контекст из транскрипта (session_events), ходы диалога через LLM-провайдер.
//
// Бессостойный к рестарту: контекст — всегда из БД (сессия + последние события),
// состояние не хранится в памяти.
package interviewer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

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
	resp, err := i.chatGuarded(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.7,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
		"text": resp, "stage": sess.Stage,
	})
	return resp, nil
}

// OnUserUtteranceStream — реплика кандидата → ответ ИИ стримом (SSE LLM).
// Возвращает канал приращений content; полный текст пишется в БД
// (ai_utterance) ПО ЗАВЕРШЕНИИ стрима. Ошибки запроса/соединения — в error.
func (i *Interviewer) OnUserUtteranceStream(ctx context.Context, sessionID int64, text string) (<-chan string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("пустая реплика")
	}
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)

	sess, msgs, err := i.context(ctx, sessionID, text)
	if err != nil {
		cancel()
		return nil, err
	}
	ch, err := i.llm.ChatStream(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.7,
		MaxTokens:   300,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	// Tee: единый потребитель стрима провайдера — собирает полный текст для БД
	// и пересылает приращения наружу (потребителю — голосовой пайплайн).
	//
	// Проверка на цитату директивы идёт по фразам, до озвучки: TTS и так ждёт
	// конца фразы, пока буфер пуст — латентности это не добавляет. При цитате
	// остаток потока выбрасываем и берём ответ заново без стрима.
	//
	// shortcut: уже озвученное до момента цитаты не отозвать (аудио ушло),
	// апгрейд — tts_stop в протоколе по этому случаю.
	var sb strings.Builder
	out := make(chan string, 16)
	go func() {
		defer cancel()
		defer close(out)
		var head strings.Builder
		guarded := false
		for delta := range ch {
			if guarded {
				sb.WriteString(delta)
				out <- delta
				continue
			}
			head.WriteString(delta)
			if leaksDirective(head.String()) {
				go func() {
					for range ch {
					}
				}()
				content, err := i.chatGuarded(ctx, llm.Request{Messages: msgs, Temperature: 0.7, MaxTokens: 300})
				if content != "" {
					sb.WriteString(content)
					out <- content
				} else if err != nil {
					i.log.Error("interviewer: повторный запрос после цитаты директивы не удался", "err", err)
				}
				guarded = true
				continue
			}
			if !clauseReady(head.String()) {
				continue
			}
			guarded = true
			headStr := head.String()
			sb.WriteString(headStr)
			out <- headStr
		}
		if !guarded && head.Len() > 0 {
			// поток кончился на фразе короче лимита — что набрали, то и отдаём
			headStr := head.String()
			sb.WriteString(headStr)
			out <- headStr
		}
		content := strings.TrimSpace(sb.String())
		if content != "" {
			i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
				"text": content, "stage": sess.Stage,
			})
		}
	}()
	return out, nil
}

// OnStageChanged — вход на стадию: первый вопрос/представление (voice/livecode/design).
func (i *Interviewer) OnStageChanged(ctx context.Context, sessionID int64, stageNote string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()

	sess, msgs, err := i.context(ctx, sessionID, directiveStageEntry)
	if err != nil {
		return "", err
	}
	if stageNote != "" {
		msgs[len(msgs)-1].Content += "\n" + stageNote
	}
	// F2 (Итерация 10): голосовая стадия — случайное начало из НАСТРАИВАЕМОГО
	// набора (по грейду, '*' — любой; не повторяем последний для пользователя).
	// LLM адаптирует формулировку под живую речь (история диалога в контексте —
	// не задаст повторно уже решённое). Без набора (openings==nil) — по-старому.
	if op := i.openings.Load(); sess.Stage == models.StageVoice && op != nil {
		if opening := op.RandomFor(sess.Grade, sess.UserID); opening != "" {
			msgs[len(msgs)-1].Content += "\nНачни именно с этого вопроса (адаптируй формулировку под живую речь, не дословно): " + opening
		}
	}
	resp, err := i.chatGuarded(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.7,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
		"text": resp, "stage": sess.Stage, "note": "stage_entry",
	})
	return resp, nil
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
	resp, err := i.chatGuarded(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.5,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
		"text": resp, "stage": sess.Stage, "note": "code_review",
	})
	return resp, nil
}

// designReviewMessage — user-сообщение ревью схемы System Design (OnDesignSubmit).
// Формат-контракт (T-20261009152316): ревью будет ОЗВУЧЕНО TTS — 3–5 предложений
// живой речью, без списков/нумерации (без ограничения модель отдаёт ~150 слов
// со структурой — voice_fit=2 по A/B-прогону 2026-10-09).
func designReviewMessage(structureRaw string) string {
	return "Кандидат представил схему System Design. Структура (JSON: блоки и связи): " +
		structureRaw +
		"\nОцени по рубрике: (1) покрытие (клиент, балансировка, сервисы, БД/кэш, очереди, мониторинг), " +
		"(2) масштабируемость, (3) отказоустойчивость, (4) обоснование trade-offs. " +
		"Ревью — 3–5 предложений живой речью, без списков и нумерации (ответ будет озвучен TTS). " +
		"В конце задай один follow-up вопрос. Устно: "
}

// OnDesignSubmit — оценка схемы System Design (ADR-004, WP-10): структура
// блоков/связей (из холста) + PNG схемы (vision, если есть) + устный ответ
// стадии → ревью по рубрике (покрытие, масштабируемость, отказоустойчивость,
// trade-offs) + follow-up.
func (i *Interviewer) OnDesignSubmit(ctx context.Context, sessionID int64, structure map[string]any, png []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()

	sess, err := i.sessions.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if session.IsTerminalStatus(sess.Status) {
		return "", ErrSessionEnded
	}

	raw, _ := json.Marshal(structure)
	userMsg := designReviewMessage(string(raw))
	var imgs []string
	if len(png) > 0 {
		imgs = []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}
	}
	msgs := i.buildMessages(sess, userMsg, imgs...)
	resp, err := i.chatGuarded(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.5,
		MaxTokens:   400,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_utterance", map[string]any{
		"text": resp, "stage": sess.Stage, "note": "design_review",
	})
	return resp, nil
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
	resp, err := i.chatGuarded(ctx, llm.Request{
		Messages:    msgs,
		Temperature: 0.7,
		MaxTokens:   120,
	})
	if err != nil {
		return "", err
	}
	i.saveEvent(ctx, sessionID, "ai_nudge", map[string]any{"text": resp, "stage": sess.Stage})
	return resp, nil
}

// CodeRunHint — текстовая сводка результатов запуска для LLM-контекста.
func CodeRunHint(passed bool, exitCode, durationMS, testsTotal, testsFailed int) string {
	return runReviewHint(passed, exitCode, durationMS, testsTotal, testsFailed)
}
