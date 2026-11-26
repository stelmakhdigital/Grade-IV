// Package taskpool пополняет пул сгенерированных задач Live-Code в sandbox
// (spec-24, инкремент 2).
//
// Идея: задач не должно быть конечным набором — пока кандидат решает, пул
// дозаполняется семьями-генераторами (детерминированными, за pre-flight gate).
// Sandbox сам ничего не генерирует: он только принимает задачи, прошедшие gate.
// Здесь — «когда» и «сколько».
package taskpool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"sort"
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
	// LLM — модель для уровня B (кожа условия) и C (задача целиком). nil —
	// оба уровня выключены.
	LLM LLM
	// Skin — разрешить Level B (переписать условие семьи под домен роли).
	Skin bool
	// FullGen — разрешить уровень C (модель пишет задачу целиком). Оставлен
	// выключенным по умолчанию: пока не видно, что местная модель выдаёт
	// проходившие gate задачи чаще, чем мусор.
	FullGen bool
	// Domain — предметная область для кожи («log-анализ», «инфраструктура»);
	// пусто — общая разработка.
	Domain string

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

// Defaults. Стеки и грейды по умолчанию НЕ задаются: за чем следить, решает
// sandbox — пары (стек, грейд) берутся из его семей (familyPairs). Иначе список
// пришлось бы синхронизировать с двумя репозиториями кода: новая семья молчала
// бы, пока кто-нибудь не допишет сюда стек.
func applyDefaults(c *Config) {
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
		// Генерация задачи = pre-flight gate: до 7 прогонов (эталон + мутанты), и
		// каждый для go — сборка. С общим кэшем (SANDBOX_GO_CACHE) это десятки
		// секунд, без него — минуты на сборку std; 2 минуты из предыдущей
		// редакции здесь срабатывали на ровном месте. Работу это больше не
		// убивает: sandbox дозапускает gate без отмены от клиента.
		c.HTTP = &http.Client{Timeout: 6 * time.Minute}
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

// pass — один проход: долить пул семьями-генераторами, затем (если разрешено)
// одна попытка кожи условия и одна — задачи целиком. Возвращает число принятых
// семейных задач.
//
// LLM-проходы идут всегда, даже когда порог выполнен: пополнение и разнообразие
// — разные задачи, и «пул полный» не означает «кандидату нечего нового показать».
func (j *Job) pass(ctx context.Context) int {
	made := j.fillFamilies(ctx)
	if j.cfg.LLM != nil {
		if j.cfg.Skin {
			j.skinTick(ctx)
		}
		if j.cfg.FullGen {
			j.fullGenTick(ctx)
		}
	}
	return made
}

// fillFamilies — для каждой пары (стек, грейд) сравнить число задач с порогом и
// добрать недостающее.
func (j *Job) fillFamilies(ctx context.Context) int {
	fams, err := j.families(ctx)
	if err != nil {
		j.log.Warn("taskpool: семьи недоступны", "err", err)
		return 0
	}
	made := 0
	for _, p := range j.watchedPairs(fams) {
		stack, grade := p.stack, p.grade
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
	return made
}

// pair — пара (стек, грейд), по которой пул держит порог задач.
type pair struct{ stack, grade string }

// familyPairs — пары, которые умеют семьи sandbox: порядок детерминированный
// (стек, затем грейд), дубликаты убраны.
func familyPairs(fams []Family) []pair {
	set := map[pair]bool{}
	for _, f := range fams {
		for _, st := range f.Stacks {
			for _, g := range f.Grades {
				set[pair{st, g}] = true
			}
		}
	}
	out := make([]pair, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].stack != out[j].stack {
			return out[i].stack < out[j].stack
		}
		return out[i].grade < out[j].grade
	})
	return out
}

// watchedPairs — за чем следим: пары семей; если в конфиге задан явный фильтр по
// стекам/грейдам — он ограничивает (операторскийrучкой, не дефолт).
func (j *Job) watchedPairs(fams []Family) []pair {
	all := familyPairs(fams)
	if len(j.cfg.Stacks) == 0 && len(j.cfg.Grades) == 0 {
		return all
	}
	inList := func(list []string, v string) bool {
		if len(list) == 0 {
			return true
		}
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	out := make([]pair, 0, len(all))
	for _, p := range all {
		if inList(j.cfg.Stacks, p.stack) && inList(j.cfg.Grades, p.grade) {
			out = append(out, p)
		}
	}
	return out
}

// count — сколько задач стека/грейда видно в банке.
// count — сколько задач пары выдал генератор (у таких meta.family заполнен).
// Встроенный банк в порог не входит: он конечен и велик, поэтому «не меньше Min
// на пару» поверх встроенных 12 задач означало бы, что динамический слой для
// go и python не включается никогда — ровно то, ради чего семьи и заводили.
func (j *Job) count(ctx context.Context, stack, grade string) (int, error) {
	var tasks []bankTask
	if err := j.getJSON(ctx, j.cfg.SandboxURL+"/api/v1/tasks?stack="+stack+"&grade="+grade, nil, &tasks); err != nil {
		return 0, err
	}
	n := 0
	for i := range tasks {
		if tasks[i].Meta != nil && tasks[i].Meta.Family != "" {
			n++
		}
	}
	return n, nil
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

// generate — попросить sandbox собрать и зарегистрировать задачу семьи.
func (j *Job) generate(ctx context.Context, family string, seed int64, grade string) (string, error) {
	body, _ := json.Marshal(map[string]any{"family": family, "seed": seed, "grade": grade})
	id, status, detail, err := j.postAdmin(ctx, "/admin/tasks/generate", body)
	if err != nil {
		return "", err
	}
	if id == "" && status == http.StatusUnprocessableEntity {
		// Gate отверг: задача не в пуле, в этом проходе сюда не лезем повторно.
		return "", fmt.Errorf("gate отверг задачу: %s", detail)
	}
	return id, nil
}

// readLimited — чтение ответа с потолком (sandbox может ответить и большим
// списком задач).
func readLimited(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, n))
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
