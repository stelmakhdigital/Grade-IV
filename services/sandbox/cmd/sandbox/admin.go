package main

// spec-24: регистрация сгенерированных задач Live-Code.
//
// Эти маршруты нарочно не похожи на /runs: принять файлы градатора от клиента
// означает позволить кандидату прислать свой check.sh и получить pass (issue #8,
// issue #9). Поэтому — отдельный токен (SANDBOX_ADMIN_TOKEN) и обязательный
// pre-flight gate: задача не попадает в банк, пока её тесты не покажут, что
// эталон проходит, а заведомо плохие решения падают.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/taskgen"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// gateTimeout — потолок на весь gate. Для go это холодная сборка на эталон плюс
// на каждого мутанта, минуты вместо десятков секунд.
const gateTimeout = 15 * time.Minute

// generateRequest — POST /admin/tasks/generate: собрать задачу семьи.
type generateRequest struct {
	Family string `json:"family"`
	Seed   int64  `json:"seed"`
	Grade  string `json:"grade"`
	// Statement/Title — «кожа» (spec-24 B): прозу переписывает LLM под домен
	// роли, файлы и проверки остаются из семьи. Проза не исполняется, поэтому
	// её пускаем сюда; файлы по-прежнему только через /admin/tasks и gate.
	Statement string `json:"statement"`
	// Generator — кто сделал задачу (для meta и отчёта), например
	// "family:shell-stats+skin:qwen3".
	Generator string `json:"generator"`
}

// registerRequest — POST /admin/tasks: готовое содержимое задачи (уровни B и C:
// LLM переписал условие семьи или сгенерировал задачу целиком).
type registerRequest struct {
	ID        string              `json:"id"`
	Stack     string              `json:"stack"`
	Grades    []string            `json:"grades"`
	Title     string              `json:"title"`
	Statement string              `json:"statement"`
	Files     map[string]string   `json:"files"`
	Reference map[string]string   `json:"reference"`
	Mutants   []map[string]string `json:"mutants"`
	Family    string              `json:"family"`
	Seed      int64               `json:"seed"`
	Generator string              `json:"generator"`
}

// registerResponse — что ушло вызывающему (пул хранит id и hash, вердикт — в лог).
type registerResponse struct {
	ID      string           `json:"id"`
	Hash    string           `json:"hash"`
	Grades  []string         `json:"grades"`
	Verdict *taskgen.Verdict `json:"verdict"`
}

// adminOnly — токен управления банком. Без настроенного токена эндпоинты
// закрыты (503): включение генерации — осознанный шаг оператора.
func adminOnly(token string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "generation_disabled",
				"hint":  "задайте SANDBOX_ADMIN_TOKEN и SANDBOX_TASKS_FILE",
			})
			return
		}
		got := r.Header.Get("Authorization")
		want := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		h(w, r)
	}
}

// handleTaskGenerate — POST /admin/tasks/generate {family, seed, grade}.
func handleTaskGenerate(bank *tasks.Bank, rn taskgen.Runner, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
			return
		}
		draft, err := taskgen.Build(req.Family, req.Seed, req.Grade)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "build_failed", "detail": err.Error()})
			return
		}
		skinned := req.Statement != ""
		if skinned {
			draft.Statement = req.Statement
		}
		gen := "family:" + req.Family
		if skinned {
			gen += "+skin"
			if req.Generator != "" {
				gen += ":" + req.Generator
			}
		}
		t := &tasks.Task{
			Stack:     familyStack(req.Family),
			Grades:    draft.Grades,
			Title:     draft.Title,
			Statement: draft.Statement,
			Files:     draft.Files,
			Meta:      &tasks.Meta{Family: req.Family, Seed: req.Seed, Generator: gen},
		}
		// Кожа — отдельная задача: id с -skin-, иначе она спорит с базовой за
		// одно место в банке, а в пуле нужны обе.
		idGrade := req.Grade
		if skinned {
			idGrade += "-skin"
		}
		t.ID = generatedID(req.Family, idGrade, req.Seed, tasks.Hash(draft.Files))
		resp, status, err := gateAndStore(r.Context(), bank, rn, t, draft.Reference, draft.Mutants, log)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": "gate_failed", "detail": err.Error()})
			return
		}
		writeJSON(w, status, resp)
	}
}

// handleTaskRegister — POST /admin/tasks: готовое содержимое задачи.
func handleTaskRegister(bank *tasks.Bank, rn taskgen.Runner, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		// 2 МБ с запасом от лимитов банка (512 КБ содержимого + эталон + мутанты).
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
			return
		}
		fam := req.Family
		if fam == "" {
			fam = "external"
		}
		gen := req.Generator
		if gen == "" {
			gen = "external"
		}
		t := &tasks.Task{
			ID:        req.ID,
			Stack:     req.Stack,
			Grades:    req.Grades,
			Title:     req.Title,
			Statement: req.Statement,
			Files:     req.Files,
			Meta:      &tasks.Meta{Family: fam, Seed: req.Seed, Generator: gen},
		}
		if t.ID == "" {
			t.ID = generatedID(fam, firstGrade(req.Grades), req.Seed, tasks.Hash(req.Files))
		}
		resp, status, err := gateAndStore(r.Context(), bank, rn, t, req.Reference, req.Mutants, log)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": "gate_failed", "detail": err.Error()})
			return
		}
		writeJSON(w, status, resp)
	}
}

// gateAndStore — единый путь для семьи и для внешнего содержимого: сначала gate,
// только потом банк. 422 — отбраковка (вердикт с доказательствами), 409 — id занят.
func gateAndStore(ctx context.Context, bank *tasks.Bank, rn taskgen.Runner, t *tasks.Task,
	reference map[string]string, mutants []map[string]string, log *slog.Logger) (*registerResponse, int, error) {

	cctx, cancel := context.WithTimeout(ctx, gateTimeout)
	defer cancel()
	verdict, err := taskgen.Check(cctx, rn, taskgen.Request{Task: t, Reference: reference, Mutants: mutants})
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	resp := &registerResponse{ID: t.ID, Hash: tasks.Hash(t.Files), Grades: t.Grades, Verdict: verdict}
	if !verdict.OK {
		log.Warn("generated task rejected", "id", t.ID, "family", t.Meta.Family, "reasons", verdict.Reasons)
		return resp, http.StatusUnprocessableEntity, nil
	}
	t.Meta.Hash = resp.Hash
	if err := bank.Add(t); err != nil {
		status := http.StatusInternalServerError
		if err == tasks.ErrDuplicateID {
			status = http.StatusConflict
		}
		return nil, status, err
	}
	log.Info("generated task accepted", "id", t.ID, "family", t.Meta.Family, "hash", t.Meta.Hash, "stack", t.Stack)
	return resp, http.StatusCreated, nil
}

// generatedID — детерминированный id: та же семья+грейд+seed дают ту же задачу,
// а хэш содержимого разводит правки генератора на одном seed.
func generatedID(family, grade string, seed int64, hash string) string {
	if grade == "" {
		grade = "any"
	}
	id := fmt.Sprintf("%s-%s-%d-%s", family, grade, seed, hash[:6])
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}

// familyStack — стек семьи; неизвестная семья собирается на go-задачу? Нет:
// Build ниже честно вернёт ошибку про неизвестную семью.
func familyStack(family string) string {
	if st, ok := taskgen.StackOf(family); ok {
		return st
	}
	return "go"
}

func firstGrade(grades []string) string {
	if len(grades) == 0 {
		return ""
	}
	return grades[0]
}
