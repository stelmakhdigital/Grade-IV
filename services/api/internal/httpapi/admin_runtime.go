// Единый контур настроек: экран «Настройки» читает и пишет конфигурацию стенда.
//
// GET /api/v1/settings — всё, что можно настроить, с двумя честными подписями:
// откуда значение (env | file | default) и что с ним будет после сохранения
// (applied_now | needs_restart | shadowed_by_env). Раньше «сохранили и ничего не
// поменялось» было обычным делом: значение оказывалось закреплено переменной
// окружения, а файл — просто файлом.
//
// PUT /api/v1/settings — одна точка для LLM, адресов voice/sandbox, порогов
// тишины/VAD и рамок сессии; голоса (STT/TTS) проксируются в voice.
//
// Маршруты /api/v1/admin/runtime и /api/v1/admin/models оставлены алиасами:
// админка и тесты ссылаются на них, дублировать логику незачем.
//
// Секреты не отдаются и не принимаются: LLM_API_KEY, JWT_SECRET, пароли БД —
// только окружение. Наружу — факт наличия.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
)

// settingSource — эффективное значение + откуда оно взялось.
type settingSource struct {
	Value  any    `json:"value"`
	Source string `json:"source"`
	Env    string `json:"env"`
}

// sessionLimitShown — лимит сессии для показа и сравнения. В коде «не задано»
// живёт как -1 (0 означало бы «ноль секунд»), но в интерфейсе это читается как
// испорченное значение: показываем 0 и подписываем «по грейду».
func (s *Server) sessionLimitShown() int {
	if s.cfg.SessionLimitS <= 0 {
		return 0
	}
	return s.cfg.SessionLimitS
}

func strSetting(envVar, fileVal, loaded string) settingSource {
	src := "default"
	switch {
	case os.Getenv(envVar) != "":
		src = "env"
	case fileVal != "":
		src = "file"
	}
	return settingSource{Value: loaded, Source: src, Env: envVar}
}

func boolSetting(envVar string, fileVal *bool, loaded bool) settingSource {
	src := "default"
	switch {
	case os.Getenv(envVar) != "":
		src = "env"
	case fileVal != nil:
		src = "file"
	}
	return settingSource{Value: loaded, Source: src, Env: envVar}
}

func intSetting(envVar string, fileVal *int, loaded int) settingSource {
	src := "default"
	switch {
	case os.Getenv(envVar) != "":
		src = "env"
	case fileVal != nil:
		src = "file"
	}
	return settingSource{Value: loaded, Source: src, Env: envVar}
}

// settingsView — ответ GET.
type settingsView struct {
	AppEnv      string            `json:"app_env"`
	Applies     string            `json:"applies"`
	AppliesNote string            `json:"applies_note"`
	Paths       map[string]string `json:"paths"`
	LLM         map[string]any    `json:"llm"`
	Voice       map[string]any    `json:"voice"`
	Sandbox     map[string]any    `json:"sandbox"`
	Timing      map[string]any    `json:"timing"`
	Install     map[string]any    `json:"install"`
	// Pending — сохранено в файл, но процесс этого ещё не видит (нужен
	// перезапуск) или переменная окружения перекрывает файл (правка мертва).
	Pending  []map[string]any `json:"pending"`
	Writable bool             `json:"writable"`
}

// valueState — есть ли значение в файле и совпадает ли оно с тем, что процесс
// уже применил. typed-nil в any под `case nil` не попадает: проверять надо
// явно, иначе отсутствие значения выглядит как «ждёт перезапуска».
func valueState(fileVal, loaded any) (present, same bool) {
	switch v := fileVal.(type) {
	case string:
		return v != "", v == loaded
	case *bool:
		if v == nil {
			return false, true
		}
		l, ok := loaded.(bool)
		return true, ok && *v == l
	case *int:
		if v == nil {
			return false, true
		}
		l, ok := loaded.(int)
		return true, ok && *v == l
	}
	return false, true
}

// statusOf — что будет со значением после сохранения: живёт сейчас, ждёт
// перезапуска, или его перекрывает переменная окружения.
func statusOf(envVar string, fileVal, loaded any) string {
	if os.Getenv(envVar) != "" {
		return "shadowed_by_env"
	}
	present, same := valueState(fileVal, loaded)
	if !present || same {
		return "applied_now"
	}
	return "needs_restart"
}

// collectPending — список ключей, которые файл уже просит, а сервис ещё не
// применил. UI показывает это как «N изменений ждут перезапуска».
func (s *Server) collectPending(f config.AppFile) []map[string]any {
	out := make([]map[string]any, 0, 4)
	add := func(key, envVar string, fileVal, loaded any) {
		st := statusOf(envVar, fileVal, loaded)
		if st == "applied_now" {
			return
		}
		item := map[string]any{"key": key, "status": st, "env": envVar}
		if st == "needs_restart" {
			item["note"] = "применится после перезапуска сервиса"
		} else {
			item["note"] = "переменная " + envVar + " перекрывает файл — правка файла ни на что не влияет"
		}
		out = append(out, item)
	}
	add("llm.base_url", "LLM_BASE_URL", f.LLM.BaseURL, s.cfg.LLMBaseURL)
	add("llm.model", "LLM_MODEL", f.LLM.Model, s.cfg.LLMModel)
	add("llm.enable_thinking", "LLM_ENABLE_THINKING", f.LLM.EnableThinking, s.cfg.LLMEnableThinking)
	add("voice_url", "VOICE_URL", f.VoiceURL, s.cfg.VoiceURL)
	add("sandbox_url", "SANDBOX_URL", f.SandboxURL, s.cfg.SandboxURL)
	add("timing.silence_nudge_s", "SILENCE_NUDGE_S", f.Timing.SilenceNudgeS, s.cfg.SilenceNudgeS)
	add("timing.vad_end_silence_ms", "VAD_END_SILENCE_MS", f.Timing.VADEndSilenceMS, s.cfg.VADEndSilenceMS)
	add("timing.vad_rms_threshold", "VAD_RMS_THRESHOLD", f.Timing.VADRMS, s.cfg.VADRMSThreshold)
	add("timing.vad_prestt_silence_ms", "VAD_PRESTT_SILENCE_MS", f.Timing.VADPreSilenceMS, s.cfg.VADPreSilenceMS)
	add("timing.session_pause_timeout_s", "SESSION_PAUSE_TIMEOUT_S", f.Timing.PauseTimeoutS, s.cfg.PauseTimeoutS)
	add("timing.session_limit_s", "SESSION_LIMIT_S", f.Timing.SessionLimitS, s.sessionLimitShown())
	return out
}

// buildSettings — сборка ответа (общая для GET и для ответа на PUT).
func (s *Server) buildSettings(r *http.Request, f config.AppFile) settingsView {
	path := config.AppConfigPath()
	view := settingsView{
		AppEnv:      s.cfg.Env,
		Applies:     "restart",
		AppliesNote: "значения читаются при старте сервисов; сохранённое применяется после перезапуска",
		Paths: map[string]string{
			"app_file": path,
			"env":      ".env (переменные окружения процесса)",
		},
		Writable: true,
		LLM: map[string]any{
			"base_url":        strSetting("LLM_BASE_URL", f.LLM.BaseURL, s.cfg.LLMBaseURL),
			"model":           strSetting("LLM_MODEL", f.LLM.Model, s.cfg.LLMModel),
			"enable_thinking": boolSetting("LLM_ENABLE_THINKING", f.LLM.EnableThinking, s.cfg.LLMEnableThinking),
			"mock":            s.cfg.LLMMock,
			"api_key_set":     s.cfg.LLMAPIKey != "",
			// Куда именно пишется LLM: путь показывает, что файл — не выдумка
			// интерфейса (админка рисует это как подпись под полем).
			"config_path": path,
		},
		Sandbox: map[string]any{
			"url":             strSetting("SANDBOX_URL", f.SandboxURL, s.cfg.SandboxURL),
			"admin_token_set": s.cfg.SandboxAdminToken != "",
			"tasks_refill_s":  s.cfg.SandboxTasksIntervalS,
			"tasks_min":       s.cfg.SandboxTasksMin,
		},
		Timing: map[string]any{
			"silence_nudge_s":         intSetting("SILENCE_NUDGE_S", f.Timing.SilenceNudgeS, s.cfg.SilenceNudgeS),
			"vad_end_silence_ms":      intSetting("VAD_END_SILENCE_MS", f.Timing.VADEndSilenceMS, s.cfg.VADEndSilenceMS),
			"vad_rms_threshold":       intSetting("VAD_RMS_THRESHOLD", f.Timing.VADRMS, s.cfg.VADRMSThreshold),
			"vad_prestt_silence_ms":   intSetting("VAD_PRESTT_SILENCE_MS", f.Timing.VADPreSilenceMS, s.cfg.VADPreSilenceMS),
			"session_pause_timeout_s": intSetting("SESSION_PAUSE_TIMEOUT_S", f.Timing.PauseTimeoutS, s.cfg.PauseTimeoutS),
			"session_limit_s":         intSetting("SESSION_LIMIT_S", f.Timing.SessionLimitS, s.sessionLimitShown()),
		},
		// install — то, что настраивается снаружи. Показываем эффективные
		// значения и факты наличия секретов: человек видит, где что лежит, и не
		// пытается «починить» адрес БД полем в форме.
		Install: map[string]any{
			"database_url":         orUnset(os.Getenv("DATABASE_URL"), s.cfg.DatabaseURL),
			"addr":                 s.cfg.Addr,
			"log_level":            s.cfg.LogLevel,
			"resume_dir":           s.cfg.ResumeDir,
			"jwt_secret_set":       s.cfg.JWTSecret != "",
			"metrics_password_set": s.cfg.MetricsPassword != "",
			"editable":             false,
			"note":                 "адрес БД, порты и секреты — переменные окружения: их правят в .env и перезапускают сервис",
		},
	}
	// Голос отвечает сам за себя: провайдеры, модели, что лежит на диске.
	// Недоступен — строка ошибки в своём блоке, а не 502 на весь экран.
	voice, err := s.voice.ConfigModels(r.Context())
	addr := strSetting("VOICE_URL", f.VoiceURL, s.cfg.VoiceURL)
	if err != nil {
		// view.Voice ещё nil: присваивание в nil-map — паника, а не 502.
		view.Voice = map[string]any{"error": err.Error(), "address": addr}
	} else {
		view.Voice = voice
		voice["address"] = addr
		if p, ok := voice["path"].(string); ok {
			view.Paths["voice_file"] = p
		}
		view.Writable = view.Writable && s.cfg.VoiceAdminToken != ""
	}
	view.Pending = s.collectPending(f)
	return view
}

func orUnset(a, b string) string {
	if a != "" {
		return a
	}
	if b != "" {
		return b
	}
	return "не задан"
}

// adminSettings — GET /api/v1/settings (и алиас /api/v1/admin/runtime).
func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	f, err := config.ReadAppFile(config.AppConfigPath())
	if err != nil {
		slog.Warn("настройки: файл не читается", "err", err)
		f = config.AppFile{}
	}
	writeJSON(w, http.StatusOK, s.buildSettings(r, f))
}

// settingsPut — тело PUT /api/v1/settings. Все секции опциональны: экран шлёт
// только изменённую (LLM отдельно от порогов, голоса отдельно).
type settingsPut struct {
	LLM *struct {
		BaseURL        *string `json:"base_url"`
		Model          *string `json:"model"`
		EnableThinking *bool   `json:"enable_thinking"`
	} `json:"llm"`
	VoiceURL   *string `json:"voice_url"`
	SandboxURL *string `json:"sandbox_url"`
	Timing     *struct {
		SilenceNudgeS   *int `json:"silence_nudge_s"`
		VADEndSilenceMS *int `json:"vad_end_silence_ms"`
		VADRMS          *int `json:"vad_rms_threshold"`
		VADPreSilenceMS *int `json:"vad_prestt_silence_ms"`
		PauseTimeoutS   *int `json:"session_pause_timeout_s"`
		SessionLimitS   *int `json:"session_limit_s"`
	} `json:"timing"`
	STT *struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"stt"`
	TTS *struct {
		Provider string `json:"provider"`
		Speaker  string `json:"speaker"`
	} `json:"tts"`
}

// adminSettingsPut — PUT /api/v1/settings (и алиас /admin/models).
//
// Свои значения пишем в config/app.json, голоса проксируем в voice с токеном
// VOICE_ADMIN_TOKEN. На ходу не меняется ничего: модель держится в памяти
// процесса, а адреса читаются при сборке клиентов — отвечаем needs_restart.
func (s *Server) adminSettingsPut(w http.ResponseWriter, r *http.Request) {
	var in settingsPut
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "тело запроса не разбирается: "+err.Error())
		return
	}
	applied := map[string]any{}

	if in.LLM != nil || in.Timing != nil || in.VoiceURL != nil || in.SandboxURL != nil {
		path := config.AppConfigPath()
		f, err := config.ReadAppFile(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable",
				"файл настроек не читается: "+err.Error())
			return
		}
		if in.LLM != nil {
			if in.LLM.BaseURL != nil {
				f.LLM.BaseURL = *in.LLM.BaseURL
			}
			if in.LLM.Model != nil {
				f.LLM.Model = *in.LLM.Model
			}
			if in.LLM.EnableThinking != nil {
				f.LLM.EnableThinking = in.LLM.EnableThinking
			}
		}
		if in.VoiceURL != nil {
			f.VoiceURL = *in.VoiceURL
		}
		if in.SandboxURL != nil {
			f.SandboxURL = *in.SandboxURL
		}
		if in.Timing != nil {
			if in.Timing.SilenceNudgeS != nil {
				f.Timing.SilenceNudgeS = in.Timing.SilenceNudgeS
			}
			if in.Timing.VADEndSilenceMS != nil {
				f.Timing.VADEndSilenceMS = in.Timing.VADEndSilenceMS
			}
			if in.Timing.VADRMS != nil {
				f.Timing.VADRMS = in.Timing.VADRMS
			}
			if in.Timing.VADPreSilenceMS != nil {
				f.Timing.VADPreSilenceMS = in.Timing.VADPreSilenceMS
			}
			if in.Timing.PauseTimeoutS != nil {
				f.Timing.PauseTimeoutS = in.Timing.PauseTimeoutS
			}
			if in.Timing.SessionLimitS != nil {
				f.Timing.SessionLimitS = in.Timing.SessionLimitS
			}
		}
		if err := config.WriteAppFile(path, f); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed",
				"не удалось записать "+path+": "+err.Error())
			return
		}
		applied["app"] = map[string]any{"saved": true, "path": path, "applies": "restart",
			"note": "переменная окружения перебивает файл; секреты и адрес БД в файл не пишутся"}
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
			slog.Info("настройки: voice", "status", status, "err", err)
		}
	}

	if len(applied) == 0 {
		writeError(w, http.StatusBadRequest, "validation",
			"пустой запрос: укажите llm, timing, voice_url, sandbox_url, stt или tts")
		return
	}
	f, _ := config.ReadAppFile(config.AppConfigPath())
	writeJSON(w, http.StatusOK, map[string]any{
		"applied": applied, "applies": "restart", "pending": s.collectPending(f),
	})
}
