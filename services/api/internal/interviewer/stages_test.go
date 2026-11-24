package interviewer

import (
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

func boardTrack(required ...string) *models.Track {
	return &models.Track{
		ID: "sysadmin",
		Board: models.BoardConfig{
			Title: "Схема инфраструктуры",
			Cases: []models.BoardCase{{
				Grade:    models.GradeMiddle,
				Title:    "Офис",
				Required: required,
			}},
		},
	}
}

// TestDesignStageScoreByRequiredArtifacts — числовая часть балла доски берётся из
// обязательных артефактов кейса; отсутствие элемента видно в отчёте списком.
func TestDesignStageScoreByRequiredArtifacts(t *testing.T) {
	mat := reportMaterial{
		Grade:  "middle",
		Design: map[string]any{"blocks": []any{"Сегмент LAN", "Файрвол", "Файл-сервер"}, "links": float64(4)},
	}
	st := designStageScore(mat, boardTrack("Сегмент LAN", "Файрвол", "VPN", "Файл-сервер", "Мониторинг"), models.GradeMiddle)
	if st == nil {
		t.Fatalf("оценка доски не посчитана")
	}
	// 3 из 5 → 1 + 4·0.6 = 3.4, плюс полбалла за связи.
	if st.Score != 3.9 {
		t.Errorf("score = %v, ожидалось 3.9 (%s)", st.Score, st.Basis)
	}
	if strings.Join(st.Missing, ",") != "VPN,Мониторинг" {
		t.Errorf("missing = %v, ожидалось VPN,Мониторинг", st.Missing)
	}
	if !strings.Contains(st.Basis, "3 из 5") {
		t.Errorf("basis не объясняет число: %q", st.Basis)
	}
}

// TestDesignStageScoreNoRequired — кейс без обязательных артефактов не даёт
// запредельной оценки: потолок 3,5, и basis об этом говорит прямо.
func TestDesignStageScoreNoRequired(t *testing.T) {
	mat := reportMaterial{Design: map[string]any{"blocks": []any{"А", "Б"}, "links": float64(2)}}
	st := designStageScore(mat, boardTrack(), models.GradeMiddle)
	if st.Score != 3.5 {
		t.Errorf("score = %v, ожидалось 3.5", st.Score)
	}
	if !strings.Contains(st.Basis, "без обязательных") {
		t.Errorf("basis не объясняет потолок: %q", st.Basis)
	}
	if st.Missing != nil {
		t.Errorf("missing должен быть пуст: %v", st.Missing)
	}
}

// TestSpeechStageScore — «была речь — 3» заменено на измеримое: объём и длина
// ответов. Молчание — 1, а не 2.
func TestSpeechStageScore(t *testing.T) {
	long := "кандидат: " + strings.Repeat("рассказываю про архитектуру ", 15)
	quiet := ""
	mat := reportMaterial{Transcript: []string{long, long, "интервьюер: вопрос", "кандидат: да"}}
	st := speechStageScore(mat)
	if st.Score < 3 {
		t.Errorf("score = %v, ожидалось >= 3 за развёрнутые ответы", st.Score)
	}
	if !strings.Contains(st.Basis, "реплик 3") {
		t.Errorf("basis не считает реплики кандидата: %q", st.Basis)
	}

	st = speechStageScore(reportMaterial{Transcript: []string{quiet, "интервьюер: вопрос"}})
	if st.Score != 1 {
		t.Errorf("без речи кандидата score = %v, ожидалось 1", st.Score)
	}

	// Много коротких реплик: балл за объём есть, за развёрнутость — нет.
	var many []string
	for i := 0; i < 12; i++ {
		many = append(many, "кандидат: да")
	}
	st = speechStageScore(reportMaterial{Transcript: many})
	if st.Score != 4 {
		t.Errorf("12 коротких реплик: score = %v, ожидалось 4", st.Score)
	}
}

// TestStageSummaryMentionsMissing — сводка уходит в промпт LLM-оценщика: в ней
// должны быть и число, и причина (каких артефактов нет).
func TestStageSummaryMentionsMissing(t *testing.T) {
	mat := reportMaterial{
		Grade:  "middle",
		Design: map[string]any{"blocks": []any{"Файрвол"}, "links": float64(1)},
	}
	sum := stageSummary(stageReports(mat, boardTrack("Файрвол", "VPN")))
	if !strings.Contains(sum, "VPN") || !strings.Contains(sum, "Схема инфраструктуры") {
		t.Fatalf("в сводке нет ни пропущенного артефакта, ни стадии: %q", sum)
	}
}

// TestStageReportsSkipAbsentStages — нет данных стадии — нет оценки (иначе
// «кандидат не рисовал схему» выглядело бы как ноль балла).
func TestStageReportsSkipAbsentStages(t *testing.T) {
	st := stageReports(reportMaterial{Transcript: []string{"кандидат: " + strings.Repeat("а", 200)}}, nil)
	for _, s := range st {
		if s.Stage != string(models.StageVoice) {
			t.Fatalf("появилась стадия без данных: %+v", s)
		}
	}
}
