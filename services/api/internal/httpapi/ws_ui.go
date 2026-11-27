// Диспетчер ui-событий WS-клиента (stage_action, finish, recording, utterance…)
// и валидация стадий. Отдельный файл: событийная логика клиента отделена от
// чтения кадров (ws.go).

package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// handleUIEvent — диспетчеризация ui-событий (ARCHITECTURE.md §4.2).
func (s *Server) handleUIEvent(id int64, msg *wsMessage, ws *wsSession) {
	ctx := context.Background()
	switch msg.Name {
	case "stage_action":
		var p struct {
			Stage models.Stage `json:"stage"`
		}
		if err := json.Unmarshal(msg.Payload, &p); err != nil || !p.Stage.Valid() {
			s.engine.SendTo(id, wsErr("invalid_request", "payload.stage: voice|livecode|design|report"))
			return
		}
		// Сообщение stage (и timer/stage report при финализации) отправляет движок.
		if _, err := s.engine.Transition(id, p.Stage); err != nil {
			code, msg := clientError(err)
			s.log.Warn("ws: переход отклонён", "session", id, "err", err)
			s.engine.SendTo(id, wsErr(code, msg))
			return
		}
		// Вход на стадии — первая реплика ИИ (WP-5): текст + озвучка (TTS).
		switch p.Stage {
		case models.StageLiveCode:
			s.enterLiveCode(ctx, id, ws)
		case models.StageDesign:
			// spec-18: текст входа — по треку (заголовок доски, кейс грейда).
			text, err := s.interviewer.OnStageChanged(ctx, id, s.designEntryNote(id))
			s.speak(ctx, id, ws, text, err) // озвучка формата стадии (TTS)
		}

	case "finish":
		if _, err := s.engine.Finish(id); err != nil {
			code, msg := clientError(err)
			s.log.Warn("ws: finish отклонён", "session", id, "err", err)
			s.engine.SendTo(id, wsErr(code, msg))
		} else {
			s.startReportGeneration(id) // WP-11: отчёт после завершения
		}

	case "code_run_requested", "submit_solution":
		// Фактическое выполнение кода — WP-6 (sandbox); здесь фиксируем событие (история).
		_, _ = s.eventData(ctx, id, "code_run", map[string]any{"ui": msg.Name, "payload": json.RawMessage(msg.Payload)})

	case "whiteboard_saved":
		// Сохранение холста (PUT /whiteboard) — WP-10; событие фиксируем.
		_, _ = s.eventData(ctx, id, "whiteboard_save", map[string]any{"payload": json.RawMessage(msg.Payload)})

	case "recording":
		// Режим записи (FR-S8, ADR-009): on/off от клиента. On: STT-сегменты
		// идут как stt_segment (AI-ход не стартует); off — до явной отправки
		// (utterance) — WS-порядок: off обрабатывается раньше utterance.
		var p struct {
			On bool `json:"on"`
		}
		// spec-14/A7: раньше ошибка Unmarshal подавлялась (`_ =`) — битый/пустой
		// payload молча декодировался как On=false: режим записи выключался,
		// recSpeechMS/recTailDiscardAt сбрасывались, диктуемый текст терялся.
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.engine.SendTo(id, wsErr("invalid_request", "payload.recording: {\"on\": true|false}"))
			return
		}
		ws.recording.Store(p.On)
		ws.recSpeechMS.Store(0)
		if p.On {
			// Новое окно записи — старый «хвост» (если был) неактуален.
			ws.recTailDiscardAt.Store(0)
		} else if ws.sttSpeechSince.Load() != 0 || ws.sttFinalPending.Load() ||
			ws.vad.InSpeech() || ws.sileroSpeech.Load() {
			// На момент выключения микрофона речь была в пути (BUG-20261012-018):
			// следующий final — хвост диктовки, уже в склеенном буфере клиента;
			// не отправляем его отдельным сообщением.
			ws.recTailDiscardAt.Store(time.Now().UnixNano())
		}
		s.log.Info("ws: режим записи", "session", id, "on", p.On)

	case "utterance":
		// Реплика кандидата (текстовый режим / FR-V8 / тесты; голосовой STT — WP-4).
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Payload, &p); err != nil || strings.TrimSpace(p.Text) == "" {
			s.engine.SendTo(id, wsErr("invalid_request", "payload.text обязателен"))
			return
		}
		text := strings.TrimSpace(p.Text)
		// Пауза/завершение: реплики не обрабатываются (FR-S7) — тихо, как при busy-гейте.
		if snap, err := s.engine.Snapshot(id); err != nil || snap.Status != models.StatusActive {
			return
		}
		// Конвейер хода (WP-5/ADR-002): событие + transcript → LLM → ai_text → TTS.
		// Асинхронно: ход занимает до ~3 с (LLM + TTS-pacing), readLoop должен
		// продолжать читать PCM (barge-in: микрофон слушается, пока ИИ говорит).
		// busy-гейт: один ход на соединение, повторные реплики отбрасываются.
		if !ws.busy.CompareAndSwap(false, true) {
			return // ход занят — реплика теряется (ходовой режим)
		}
		go func() {
			defer ws.busy.Store(false)
			s.runCandidateTurn(ws, text)
		}()

	default:
		s.engine.SendTo(id, wsErr("unknown_ui_event", "неизвестное событие: "+msg.Name))
	}
}
