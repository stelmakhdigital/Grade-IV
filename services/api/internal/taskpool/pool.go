// Package taskpool пополняет пул сгенерированных задач Live-Code в sandbox
// (spec-24, инкремент 2).
//
// Идея: задач не должно быть конечным набором — пока кандидат решает, пул
// дозаполняется семьями-генераторами (детерминированными, за pre-flight gate).
// Sandbox сам ничего не генерирует: он только принимает задачи, прошедшие gate.
// Здесь — «когда» и «сколько».
package taskpool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"time"
)

// Config — параметры пополнения.
type Config struct {
	SandboxURL string   // адрес sandbox (SANDBOX_URL)
	AdminToken string   // токен /admin/tasks (SANDBOX_ADMIN_TOKEN); пусто — задача выключена
	Stacks     []string // стеки, за которыми следим (def: shell)
	Grades     []string // грейды (def: junior middle senior staff)
	// Min — сколько задач стека/грейда должно быть в пуле хотя бы. Ниже порога —
	// генерируем недостающее.
	Min int
	// MaxPerTick — потолок генераций за проход: если генератор сломан и gate
	// бракует всё, тикет не должен гонять gate по кругу.
	MaxPerTick int
	Interval   time.Duration // период проверки (def 10m)
	// HTTP — транспорт (в тестах — httptest); nil → http.DefaultClient.
	HTTP *http.Client
	Rand *rand.Rand
}

// Job — фоновое пополнение пула.
type Job struct {
	cfg Config
	log *slog.Logger
}

// Families — семья-генератор из sandbox (GET /admin/families).
type Family struct {
	ID     string   `json:"id"`
	Stacks []string `json:"stacks"`
	Grades []string `json:"grades"`
}

// Defaults: следим за shell — единственная семья пока shell-ная, и именно по
// shell банк был тоньше всего (1 задача на junior, 0 на senior/staff).
func applyDefaults(c *Config) {
	if len(c.Stacks) == 0 {
		c.Stacks = []string{"shell"}
	}
	if len(c.Grades) == 0 {
		c.Grades = []string{"junior", "middle", "senior", "staff"}
	}
	if c.Min <= 0 {
		c.Min = 4
	}
	if c.MaxPerTick <= 0 {
		c.MaxPerTick = 4
	}
	if c.Interval <= 0 {
		c.Interval = 10 * time.Minute
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 2 * time.Minute}
	}
	if c.Rand == nil {
		c.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
}

// New — задача по конфигy. Токена нет → Returns nil: функция выключена, а не
// «работает вхолостую» (иначе логи забиваются 401/503).
func New(cfg Config, log *slog.Logger) *Job {
	if cfg.SandboxURL == "" || cfg.AdminToken == "" {
		return nil
	}
	applyDefaults(&cfg)
	return &Job{cfg: cfg, log: log}
}

// Run — цикл до отмены ctx: сначала один проход сразу (после рестарта пул может
// быть пуст), далее по тикеру.
func (j *Job) Run(ctx context.Context) {
	j.pass(ctx)
	t := time.NewTicker(j.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.pass(ctx)
		}
	}
}

// pass — один проход: для каждой пары (стек, грейд) сравнить число задач с
// порогом и добрать недостающее. Возвращает число принятых задач.
func (j *Job) pass(ctx context.Context) int {
	fams, err := j.families(ctx)
	if err != nil {
		j.log.Warn("taskpool: семьи недоступны", "err", err)
		return 0
	}
	made := 0
	for _, stack := range j.cfg.Stacks {
		for _, grade := range j.cfg.Grades {
			if made >= j.cfg.MaxPerTick {
				return made
			}
			have, err := j.count(ctx, stack, grade)
			if err != nil {
				j.log.Warn("taskpool: счёт задач не получен", "stack", stack, "grade", grade, "err", err)
				continue
			}
			for have < j.cfg.Min && made < j.cfg.MaxPerTick {
				fam := pickFamily(fams, stack, grade, j.cfg.Rand)
				if fam == "" {
					j.log.Warn("taskpool: нет семьи для стека/грейда", "stack", stack, "grade", grade)
					break
				}
				seed := j.cfg.Rand.Int63n(1_000_000) + 1
				accepted, err := j.generate(ctx, fam, seed, grade)
				if err != nil {
					// Gate отверг задачу или sandbox недоступен. Повторим на
					// следующем тике с другим seed; в этом проходе больше не лезем.
					j.log.Warn("taskpool: генерация не принята",
						"family", fam, "seed", seed, "stack", stack, "grade", grade, "err", err)
					break
				}
				if accepted == "" {
					break
				}
				j.log.Info("taskpool: задача в пуле", "id", accepted, "family", fam, "seed", seed, "grade", grade)
				have++
				made++
			}
		}
	}
	return made
}

// count — сколько задач стека/грейда видно в банке.
func (j *Job) count(ctx context.Context, stack, grade string) (int, error) {
	var tasks []json.RawMessage
	if err := j.getJSON(ctx, j.cfg.SandboxURL+"/api/v1/tasks?stack="+stack+"&grade="+grade, nil, &tasks); err != nil {
		return 0, err
	}
	return len(tasks), nil
}

// families — список семей-генераторов (админский маршрут: семья — про генерацию,
// не про кандидата).
func (j *Job) families(ctx context.Context) ([]Family, error) {
	var out struct {
		Families []Family `json:"families"`
	}
	err := j.getJSON(ctx, j.cfg.SandboxURL+"/admin/families", map[string]string{"Authorization": "Bearer " + j.cfg.AdminToken}, &out)
	return out.Families, err
}

// generate — попросить sandbox собрать и зарегистрировать задачу. Возвращает id
// принятой задачи ("" — не принято).
func (j *Job) generate(ctx context.Context, family string, seed int64, grade string) (string, error) {
	body, _ := json.Marshal(map[string]any{"family": family, "seed": seed, "grade": grade})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		j.cfg.SandboxURL+"/admin/tasks/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+j.cfg.AdminToken)
	resp, err := j.cfg.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusCreated:
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", fmt.Errorf("разбор ответа генерации: %w", err)
		}
		return out.ID, nil
	case resp.StatusCode == http.StatusConflict:
		// Такой seed уже в пуле — это не поломка, просто следующий тик возьмёт
		// другой seed.
		return "", nil
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return "", errors.New("gate отверг задачу: " + string(raw))
	default:
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
}

func (j *Job) getJSON(ctx context.Context, url string, headers map[string]string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := j.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// pickFamily — семья, которая умеет нужный стек и грейд; выбор случайный, чтобы
// при нескольких семьях пул не состоял из одной.
func pickFamily(fams []Family, stack, grade string, rng *rand.Rand) string {
	var ok []string
	for _, f := range fams {
		if contains(f.Stacks, stack) && (len(f.Grades) == 0 || contains(f.Grades, grade)) {
			ok = append(ok, f.ID)
		}
	}
	if len(ok) == 0 {
		return ""
	}
	return ok[rng.Intn(len(ok))]
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
