// Контур настроек (фидбек 2026-11-28): admin-панель «Система».
//
// GET /api/v1/admin/runtime — что подключено: LLM (эндпоинт, модель, thinking,
// «ключ задан»), voice (провайдеры, модели, загружено ли, что лежит на диске,
// чем закреплено), sandbox (адрес, есть ли токен управления). Раньше это
// выяснилось только чтением .env и логов: «voice молчит» из-за отсутствующих
// весов выглядело как «интервьюер не отвечает».
//
// PUT /api/v1/admin/models — выбор моделей. Секреты сюда не приезжают и не
// уезжают: LLM_API_KEY остаётся переменной окружения.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
)

// settingSource — значение вместе с источником (env | file | default) и именем
// переменной: «сохранено, а эффекта нет» обычно значит «значение закреплено env».
type settingSource struct {
	Value  any    `json:"value"`
	Source string `json:"source"`
	Env    string `json:"env"`
}

func strSource(envVar, fileVal string) settingSource {
	if v := os.Getenv(envVar); v != "" {
		return settingSource{Value: v, Source: "env", Env: envVar}
	}
	if fileVal != "" {
		return settingSource{Value: fileVal, Source: "file", Env: envVar}
	}
	return settingSource{Source: "default", Env: envVar}
}

func boolSource(envVar string, value bool, fileVal *bool) settingSource {
	if v := os.Getenv(envVar); v != "" {
		return settingSource{Value: v == "true", Source: "env", Env: envVar}
	}
	if fileVal != nil {
		return settingSource{Value: *fileVal, Source: "file", Env: envVar}
	}
	return settingSource{Value: value, Source: "default", Env: envVar}
}

// runtimeLLM — LLM-блок панели. Ключ показан фактом наличия: длина, префикс и
// тем более сам ключ в админском JSON — приманка для скриншотов и логов.
func (s *Server) runtimeLLM() map[string]any {
	path := config.LLMConfigPath()
	file, err := config.ReadLLMFile(path)
	if err != nil {
		slog.Warn("админ: не читается файл LLM", "path", path, "err", err)
	}
	base := strSource("LLM_BASE_URL", file.BaseURL)
	if base.Source == "default" {
		base.Value = s.cfg.LLMBaseURL
	}
	model := strSource("LLM_MODEL", file.Model)
	if model.Source == "default" && s.cfg.LLMModel != "" {
		model.Value = s.cfg.LLMModel
	}
	return map[string]any{
		"base_url":        base,
		"model":           model,
		"enable_thinking": boolSource("LLM_ENABLE_THINKING", s.cfg.LLMEnableThinking, file.EnableThinking),
		"mock":            s.cfg.LLMMock,
		"api_key_set":     s.cfg.LLMAPIKey != "",
		"config_path":     path,
	}
}

// runtimeSandbox — адрес, наличие токена и пороги пополнения. Сам sandbox —
// отдельный сервис со своим /healthz: в панели он либо доступен (список задач
// отдаётся), либо нет; дублировать его диагностику здесь незачем.
func (s *Server) runtimeSandbox() map[string]any {
	return map[string]any{
		"url":             s.cfg.SandboxURL,
		"admin_token_set": s.cfg.SandboxAdminToken != "",
		"tasks_refill_s":  s.cfg.SandboxTasksIntervalS,
		"tasks_min":       s.cfg.SandboxTasksMin,
	}
}

// adminRuntime — GET /api/v1/admin/runtime (доступ — через s.requireAdmin в маршруте).
func (s *Server) adminRuntime(w http.ResponseWriter, r *http.Request) {
	view := map[string]any{
		"app_env": s.cfg.Env,
		"llm":     s.runtimeLLM(),
		"sandbox": s.runtimeSandbox(),
		"applies": "restart",
		"applies_note": "значения читаются при старте сервисов — сохранённое " +
			"применится после перезапуска",
	}
	// voice отвечает сам за свой блок (провайдеры, каталог весов, источники).
	// Недоступен — не 502 на всю панель: админ должен видеть, ЧЕГО не хватает.
	voice, err := s.voice.ConfigModels(r.Context())
	if err != nil {
		view["voice"] = map[string]any{"error": err.Error()}
	} else {
		view["voice"] = voice
	}
	writeJSON(w, http.StatusOK, view)
}

// modelsPut — тело PUT /api/v1/admin/models. Поля опциональны: панель шлёт
// только изменённый блок (LLM отдельно от голосов).
type modelsPut struct {
	LLM *struct {
		BaseURL        *string `json:"base_url"`
		Model          *string `json:"model"`
		EnableThinking *bool   `json:"enable_thinking"`
	} `json:"llm"`
	STT *struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"stt"`
	TTS *struct {
		Provider string `json:"provider"`
		Speaker  string `json:"speaker"`
	} `json:"tts"`
}

// adminModelsPut — PUT /api/v1/admin/models.
//
// LLM пишется в config/llm.json (env старше файла — см. config/llmfile.go),
// голос проксируется в voice с токеном VOICE_ADMIN_TOKEN. Ничего не применяется
// на ходу: модель сидит в памяти процесса, «сменили прямо сейчас» было бы
// враньём — отвечаем applies=restart.
func (s *Server) adminModelsPut(w http.ResponseWriter, r *http.Request) {
	var in modelsPut
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "тело запроса не разбирается: "+err.Error())
		return
	}
	applied := map[string]any{}

	if in.LLM != nil {
		path := config.LLMConfigPath()
		file, err := config.ReadLLMFile(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable",
				"файл настроек LLM не читается: "+err.Error())
			return
		}
		if in.LLM.BaseURL != nil {
			file.BaseURL = *in.LLM.BaseURL
		}
		if in.LLM.Model != nil {
			file.Model = *in.LLM.Model
		}
		if in.LLM.EnableThinking != nil {
			file.EnableThinking = in.LLM.EnableThinking
		}
		if err := config.WriteLLMFile(path, file); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed",
				"не удалось записать "+path+": "+err.Error())
			return
		}
		applied["llm"] = map[string]any{"saved": true, "path": path, "applies": "restart",
			"note": "переменная окружения перебивает файл; ключ LLM_API_KEY — только окружение"}
	}

	if in.STT != nil || in.TTS != nil {
		if s.cfg.VoiceAdminToken == "" {
			applied["voice"] = map[string]any{"saved": false,
				"error": "у api не задан VOICE_ADMIN_TOKEN — права на запись настроек voice не выданы"}
		} else {
			status, body, err := s.voice.PutConfigModels(r.Context(), in, s.cfg.VoiceAdminToken)
			switch {
			case err != nil:
				applied["voice"] = map[string]any{"saved": false, "error": err.Error()}
			default:
				applied["voice"] = map[string]any{"status": status, "body": body,
					"saved": status == http.StatusOK, "applies": "restart"}
			}
			slog.Info("админ: настройки voice", "status", status, "err", err)
		}
	}
	if len(applied) == 0 {
		writeError(w, http.StatusBadRequest, "validation", "пустой запрос: укажите llm, stt или tts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied, "applies": "restart"})
}
