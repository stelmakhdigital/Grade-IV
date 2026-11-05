// Package tracks — хранилище треков собеседований (spec-18).
// Источник — JSON-файл (дефолт config/roles.json, оверрайд ROLE_TRACKS_FILE;
// тот же паттерн, что openings.json). Админ-CRUD мутирует in-memory и
// перезаписывает файл атомарно (tmp + rename).
package tracks

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Store — потокобезопасное хранилище треков.
type Store struct {
	mu     sync.RWMutex
	file   string // пусто — только in-memory (тесты)
	tracks map[string]*models.Track
	order  []string // порядок файла/создания — стабильный список
}

// Load — чтение файла треков; файла нет → запись дефолтов. Валидация каждой
// записи; битая запись не валит хранилище (пропускается с логом).
func Load(file string, log *slog.Logger) (*Store, error) {
	s := &Store{file: file, tracks: make(map[string]*models.Track)}
	if data, err := os.ReadFile(file); err == nil {
		var ts []models.Track
		if err := json.Unmarshal(data, &ts); err != nil {
			return nil, fmt.Errorf("tracks: %s: %w", file, err)
		}
		for i := range ts {
			t := ts[i]
			if err := t.Validate(); err != nil {
				if log != nil {
					log.Warn("tracks: запись пропущена (валидация)", "err", err)
				}
				continue
			}
			t.SortCriteria()
			s.tracks[t.ID] = &t
			s.order = append(s.order, t.ID)
		}
		if len(s.tracks) == 0 {
			return nil, fmt.Errorf("tracks: %s: ни одного валидного трека", file)
		}
		return s, nil
	}
	// Файла нет → дефолты + запись файла.
	for _, t := range DefaultTracks() {
		if err := t.Validate(); err != nil {
			return nil, fmt.Errorf("tracks: дефолт %s: %w", t.ID, err)
		}
		t.SortCriteria()
		s.tracks[t.ID] = &t
		s.order = append(s.order, t.ID)
	}
	if file != "" {
		if err := s.persist(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Get — трек по id (nil, nil — не найден).
func (s *Store) Get(id string) (*models.Track, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tracks[id]
	if !ok {
		return nil, nil
	}
	cp := *t // копия: мутабельные срезы наружу не отдаём
	return &cp, nil
}

// GetOrDefault — трек по id; пустой/неизвестный id → backend (fallback
// с логом вызывающей стороны).
func (s *Store) GetOrDefault(id string) (*models.Track, error) {
	if id == "" {
		id = models.DefaultTrackID
	}
	t, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if t == nil && id != models.DefaultTrackID {
		t, err = s.Get(models.DefaultTrackID)
	}
	return t, err
}

// List — треки в стабильном порядке (файл, затем создания).
func (s *Store) List() []models.Track {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.Track, 0, len(s.tracks))
	for _, id := range s.order {
		if t, ok := s.tracks[id]; ok {
			out = append(out, *t)
		}
	}
	return out
}

// Upsert — создание/обновление (валидация; запись файла атомарно).
func (s *Store) Upsert(t models.Track) error {
	if err := t.Validate(); err != nil {
		return err
	}
	t.SortCriteria()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tracks[t.ID]; !exists {
		s.order = append(s.order, t.ID)
	}
	tt := t
	s.tracks[t.ID] = &tt
	return s.persistLocked()
}

// Delete — удаление трека (backend — нельзя: дефолт совместимости).
func (s *Store) Delete(id string) error {
	if id == models.DefaultTrackID {
		return fmt.Errorf("трек %s удалять нельзя (дефолт совместимости)", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tracks[id]; !ok {
		return nil
	}
	delete(s.tracks, id)
	for i, x := range s.order {
		if x == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return s.persistLocked()
}

// Count — число треков (тесты).
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tracks)
}

// persist — потокобезопасная запись файла (используется вне mu-критических
// секций, например в Load).
func (s *Store) persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

// persistLocked — атомарная запись (tmp + rename); mu должна быть зажата.
func (s *Store) persistLocked() error {
	if s.file == "" {
		return nil
	}
	out := make([]models.Track, 0, len(s.tracks))
	for _, id := range s.order {
		out = append(out, *s.tracks[id])
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.file)
	tmp, err := os.CreateTemp(dir, "roles-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.file); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// DefaultTracks — треки из коробки (spec-18): backend (нынешнее поведение),
// sysadmin и pm (некодовые). Программы голосовой стадии для некодовых ролей —
// в interview_templates (track=sysadmin/pm), сидируются при Migrate.
func DefaultTracks() []models.Track {
	backend := models.Track{
		ID: "backend",
		// spec-22: один runtime на роль — Go. Python-роль админ создаёт
		// отдельно (ИИ-заполнение по описанию); выбор стека кандидатом убран.
		Title:    "Backend-разработчик",
		Practice: &models.Practice{Runtime: models.StackGo},
		Grades:   []models.Grade{models.GradeJunior, models.GradeMiddle, models.GradeSenior, models.GradeStaff},
		Board: models.BoardConfig{
			Title: "System Design: сервис с нуля",
			ReviewPrompt: "Кандидат собирал схему системы (System Design). Оцени структуру: " +
				"компоненты и их обязанности, данные и хранилища, узкие места, масштабирование " +
				"(нагрузка ×10), отказоустойчивость. Сверяй схему с устным объяснением: расхождения " +
				"или пустые блоки — минус. Задай 1–2 стресс-вопроса (нагрузка ×10, сбой узла).",
			Groups: []models.BlockGroup{
				{Title: "Трафик", Names: []string{"Client", "Load Balancer", "CDN"}},
				{Title: "Сервисы", Names: []string{"API-сервис", "Worker", "Внешний API"}},
				{Title: "Данные и инфраструктура", Names: []string{"SQL-БД", "NoSQL", "Кэш", "Очередь", "Message Broker", "Monitoring"}},
			},
		},
		Criteria: []models.TrackCriterion{
			{Name: "CS-фундамент (ООП, структуры данных, сложность)", Weight: 0.20, Hint: "теория по ответам голосовой стадии"},
			{Name: "Deep-dive по стеку (Go)", Weight: 0.25, Hint: "глубина знаний стека"},
			{Name: "Live-Code (алгоритм, качество кода, тесты)", Weight: 0.25, Hint: "по запускам кода"},
			{Name: "System Design (сервис с нуля, масштабируемость)", Weight: 0.15, Hint: "по схеме и объяснению"},
			{Name: "Коммуникация (ясность, структура, русский язык)", Weight: 0.15, Hint: "ясность речи и структуры ответов"},
		},
	}
	// Веса backend-трека — усреднённые по грейдам прежних критериев (junior/middle/
	// senior различались); точная развеска по грейдам остаётся в fallback
	// criteriaForGrade для сессий без трека.
	sysadmin := models.Track{
		ID:       "sysadmin",
		Title:    "Системный администратор",
		Practice: &models.Practice{Runtime: models.StackShell},
		Grades:   []models.Grade{models.GradeJunior, models.GradeMiddle, models.GradeSenior},
		Board: models.BoardConfig{
			Title: "Схема инфраструктуры",
			ReviewPrompt: "Кандидат собирал схему инфраструктуры. Оцени: сегментация сети и " +
				"границы доверия, единые точки отказа, резервирование и бэкапы, мониторинг и " +
				"алертинг, автоматизация (скрипты/IaC). Сверяй схему с устным объяснением: " +
				"расхождения или пустые блоки — минус. Задай 1–2 стресс-вопроса (отказ узла, " +
				"восстановление после сбоя).",
			Groups: []models.BlockGroup{
				{Title: "Пользователи и доступ", Names: []string{"Рабочая станция", "Ноутбук", "VPN", "Контроллер домена"}},
				{Title: "Сети", Names: []string{"Сегмент LAN", "Файрвол", "Коммутатор", "DNS", "Интернет-канал"}},
				{Title: "Серверы и сервисы", Names: []string{"Файл-сервер", "Почтовый сервер", "БД-сервер", "Веб-сервер"}},
				{Title: "Эксплуатация", Names: []string{"Бэкап-хранилище", "Мониторинг", "SCM/инвентарь"}},
			},
			Cases: []models.BoardCase{
				{
					Grade: models.GradeMiddle,
					Title: "Сеть нового офиса на 50 человек",
					Statement: "Спроектируй сеть нового офиса на 50 человек: сегменты, доступ " +
						"сотрудников и гостей, выход в интернет, файловый сервер и бэкапы. Объясни " +
						"каждый элемент схемы.",
					Checklist: []string{"сегментация (сотрудники/гости/серверы)", "файрвол и границы доверия", "резервный интернет-канал или VPN", "бэкап-хранилище вне серверного сегмента", "мониторинг"},
				},
			},
		},
		Criteria: []models.TrackCriterion{
			{Name: "Диагностика и устранение неисправностей", Weight: 0.30, Hint: "системность разбора инцидентов в диалоге"},
			{Name: "Сети и сегментация", Weight: 0.25, Hint: "по схеме инфраструктуры и ответам"},
			{Name: "Автоматизация (скрипты, IaC)", Weight: 0.20, Hint: "чем автоматизирует рутину"},
			{Name: "Эксплуатация (бэкапы, мониторинг, документация)", Weight: 0.15, Hint: "привычки эксплуатации"},
			{Name: "Коммуникация (ясность, структура)", Weight: 0.10, Hint: "ясность речи и структуры ответов"},
		},
	}
	pm := models.Track{
		ID:     "pm",
		Title:  "Проджект-менеджер",
		Grades: []models.Grade{models.GradeJunior, models.GradeMiddle, models.GradeSenior},
		Board: models.BoardConfig{
			Title: "План проекта",
			ReviewPrompt: "Кандидат собирал план проекта на доске. Оцени: декомпозиция на этапы " +
				"и артефакты, зависимости и критический путь, риски и их митигация, роли и " +
				"ответственности. Сверяй схему с устным объяснением: расхождения или пустые " +
				"блоки — минус. Задай 1–2 стресс-вопроса (срыв срока ключевого этапа, выпадение " +
				"ресурса).",
			Groups: []models.BlockGroup{
				{Title: "Работы", Names: []string{"Эпик", "Задача", "Веха (milestone)", "Релиз"}},
				{Title: "Команда", Names: []string{"Роль", "Команда", "Стейкхолдер", "Внешний подрядчик"}},
				{Title: "Управление", Names: []string{"Риск", "Зависимость", "Решение", "Отчёт/статус"}},
			},
			Cases: []models.BoardCase{
				{
					Grade: models.GradeMiddle,
					Title: "Запуск продукта за 3 месяца",
					Statement: "Составь план запуска продукта за 3 месяца: эпики, вехи, зависимости, " +
						"риски. Объясни, кто за что отвечает и что пойдёт не так.",
					Checklist: []string{"декомпозиция на эпики", "вехи с датами", "зависимости между работами", "риски с митигацией", "роли и ответственность"},
				},
			},
		},
		Criteria: []models.TrackCriterion{
			{Name: "Декомпозиция и планирование", Weight: 0.30, Hint: "по плану на доске и диалогу"},
			{Name: "Риски и митигация", Weight: 0.25, Hint: "полнота карты рисков"},
			{Name: "Работа со стейкхолдерами", Weight: 0.20, Hint: "коммуникация, ожидания, эскалация"},
			{Name: "Метрики и контроль исполнения", Weight: 0.15, Hint: "как меряет прогресс"},
			{Name: "Коммуникация (ясность, структура)", Weight: 0.10, Hint: "ясность речи и структуры ответов"},
		},
	}
	return []models.Track{backend, sysadmin, pm}
}

// NewMemory — пустое хранилище без файла (крайний fallback; только тесты/авария).
func NewMemory() *Store {
	s := &Store{tracks: make(map[string]*models.Track)}
	for _, t := range DefaultTracks() {
		t.SortCriteria()
		s.tracks[t.ID] = &t
		s.order = append(s.order, t.ID)
	}
	return s
}
