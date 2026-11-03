package session

import (
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// TestBoardStagePayload — payload доски в stage-сообщении (spec-18/Ф3/Ф5):
// палитра из трека; чек-лист кейса кандидату не отдаётся.
func TestBoardStagePayload(t *testing.T) {
	if b := boardStagePayload(nil, models.GradeMiddle); b != nil {
		t.Fatalf("nil-трек → nil payload, получено %v", b)
	}
	sysadmin := models.Track{
		ID: "sysadmin",
		Board: models.BoardConfig{
			Title: "Схема инфраструктуры",
			Groups: []models.BlockGroup{
				{Title: "Сети", Names: []string{"Файрвол", "VPN"}},
			},
			Cases: []models.BoardCase{{
				Grade:     models.GradeMiddle,
				Title:     "Кейс",
				Statement: "Собери схему",
				Checklist: []string{"секретный пункт рубрики"},
			}},
		},
	}
	b := boardStagePayload(&sysadmin, models.GradeMiddle)
	if b["title"] != "Схема инфраструктуры" {
		t.Fatalf("title: %v", b["title"])
	}
	caseAny, ok := b["case"].(map[string]any)
	if !ok {
		t.Fatalf("case отсутствует: %v", b)
	}
	if _, has := caseAny["checklist"]; has {
		t.Fatal("чек-лист не должен попадать кандидату")
	}
	// другой грейд — без кейса
	b2 := boardStagePayload(&sysadmin, models.GradeJunior)
	if _, has := b2["case"]; has {
		t.Fatal("кейс не для этого грейда не должен выдаваться")
	}
}
