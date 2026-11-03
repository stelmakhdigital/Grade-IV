package tracks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

func validTrack() models.Track {
	return models.Track{
		ID:      "qa",
		Title:   "QA-инженер",
		Coding:  false,
		Grades:  []models.Grade{models.GradeJunior, models.GradeMiddle},
		Board: models.BoardConfig{
			Title:        "Схема тест-плана",
			ReviewPrompt: "Оцени полноту покрытия.",
			Groups:       []models.BlockGroup{{Title: "Тесты", Names: []string{"Модуль", "Сценарий"}}},
		},
		Criteria: []models.TrackCriterion{
			{Name: "Качество тест-дизайна", Weight: 0.5},
			{Name: "Коммуникация", Weight: 0.5},
		},
	}
}

func TestValidateOK(t *testing.T) {
	tk := validTrack()
	if err := tk.Validate(); err != nil {
		t.Fatalf("валидный трек отвергнут: %v", err)
	}
}

func TestValidateBadID(t *testing.T) {
	tk := validTrack()
	tk.ID = "Bad_ID!"
	if err := tk.Validate(); err == nil {
		t.Fatal("ожидаем отказ на невалидном id")
	}
}

func TestValidateWeightsNormalized(t *testing.T) {
	tk := validTrack()
	tk.Criteria[0].Weight, tk.Criteria[1].Weight = 0.3, 0.3 // сумма 0.6 — вне допуска
	if err := tk.Validate(); err == nil {
		t.Fatal("ожидаем отказ: сумма весов < 0.95")
	}
	// в допуске 0.95..1.05 → нормализуется к сумме 1
	tk.Criteria[0].Weight, tk.Criteria[1].Weight = 0.30, 0.72 // сумма 1.02
	if err := tk.Validate(); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	sum := tk.Criteria[0].Weight + tk.Criteria[1].Weight
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("веса не нормализованы: %v, %v (сумма %.3f)", tk.Criteria[0].Weight, tk.Criteria[1].Weight, sum)
	}
}

func TestValidateCodingStacks(t *testing.T) {
	tk := validTrack()
	tk.Coding = true // coding без стеков
	if err := tk.Validate(); err == nil {
		t.Fatal("coding без стеков должен быть отвергнут")
	}
	tk.Stacks = []models.Stack{models.StackGo}
	if err := tk.Validate(); err != nil {
		t.Fatalf("coding+go: %v", err)
	}
	backend := validTrack()
	backend.Coding, backend.Stacks = false, []models.Stack{models.StackGo}
	if err := backend.Validate(); err == nil {
		t.Fatal("некодовый трек со стеками должен быть отвергнут")
	}
}

func TestStages(t *testing.T) {
	sys := DefaultTracks()
	var sysTrack *models.Track
	for i := range sys {
		if sys[i].ID == "sysadmin" {
			sysTrack = &sys[i]
		}
	}
	if sysTrack == nil {
		t.Fatal("нет трека sysadmin")
	}
	st := sysTrack.Stages(models.GradeJunior)
	if len(st) != 3 || st[1] != models.StageDesign {
		t.Fatalf("некодовый трек: voice→design→report, получено %v", st)
	}
	var backend *models.Track
	for i := range sys {
		if sys[i].ID == "backend" {
			backend = &sys[i]
		}
	}
	jb := backend.Stages(models.GradeJunior)
	if len(jb) != 3 || jb[1] != models.StageLiveCode {
		t.Fatalf("backend junior: voice→livecode→report, получено %v", jb)
	}
	mb := backend.Stages(models.GradeMiddle)
	if len(mb) != 4 {
		t.Fatalf("backend middle: 4 стадии, получено %v", mb)
	}
}

func TestLoadDefaultsAndFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "roles.json")
	s, err := Load(file, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.List()) < 3 {
		t.Fatalf("дефолтов < 3: %d", len(s.List()))
	}
	// файл записан (дефолты персистятся)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("файл дефолтов не записан: %v", err)
	}
	// CRUD: upsert → перечитывание файла
	tk := validTrack()
	if err := s.Upsert(tk); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	s2, err := Load(file, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !s2.trackExistsInList("qa") {
		t.Fatal("qa не персистился")
	}
	// delete + защита backend
	if err := s2.Delete("backend"); err == nil {
		t.Fatal("backend удалять нельзя")
	}
	if err := s2.Delete("qa"); err != nil {
		t.Fatalf("delete qa: %v", err)
	}
	if s2.Count() != len(s.List())-1 {
		t.Fatal("count после delete")
	}
}

func (s *Store) trackExistsInList(id string) bool {
	for _, t := range s.List() {
		if t.ID == id {
			return true
		}
	}
	return false
}

func TestLoadSkipsInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "roles.json")
	good := validTrack()
	data, _ := json.Marshal([]models.Track{good, {ID: "broken", Title: " ", Grades: nil}})
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(file, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("битая запись должна пропускаться: %d", s.Count())
	}
	if !strings.Contains(file, "roles.json") {
		t.Fatal("sanity")
	}
}

func TestGetOrDefaultFallsBack(t *testing.T) {
	s, _ := Load("", nil)
	tk, err := s.GetOrDefault("no-such-track")
	if err != nil {
		t.Fatal(err)
	}
	if tk.ID != "backend" {
		t.Fatalf("fallback на backend, получен %s", tk.ID)
	}
}
