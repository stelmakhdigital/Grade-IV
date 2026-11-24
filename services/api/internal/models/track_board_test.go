package models

import "testing"

// TestBoardCaseCoverage — обязательные артефакты ищутся по нормализованному
// виду: регистр, скобочные уточнения и лишние слова не должны мешать.
func TestBoardCaseCoverage(t *testing.T) {
	c := &BoardCase{
		Grade:     GradeMiddle,
		Title:     "Запуск продукта",
		Required:  []string{"Эпик", "Веха (milestone)", "Риск", "Стейкхолдер"},
		Checklist: []string{"декомпозиция"},
	}
	cases := []struct {
		name        string
		blocks      []string
		wantFound   int
		wantMissing []string
	}{
		{
			name:        "доски нет вообще",
			blocks:      nil,
			wantFound:   0,
			wantMissing: []string{"Эпик", "Веха (milestone)", "Риск", "Стейкхолдер"},
		},
		{
			name:      "точные названия палитры",
			blocks:    []string{"Эпик", "Веха (milestone)", "Риск", "Стейкхолдер"},
			wantFound: 4,
		},
		{
			name:      "регистр и лишние слова не мешают",
			blocks:    []string{"эпик", "Веха", "риск бизнес-рисков", "стейкхолдеры и роли"},
			wantFound: 4,
		},
		{
			name:        "часть артефактов отсутствует",
			blocks:      []string{"Эпик", "Задача", "Релиз"},
			wantFound:   1,
			wantMissing: []string{"Веха (milestone)", "Риск", "Стейкхолдер"},
		},
		{
			name:      "блок длиннее и короче артефакта",
			blocks:    []string{"эпик авторизации", "веха беты", "риск срыва срока", "стейкхолдер: заказчик"},
			wantFound: 4,
		},
		{
			name:        "роль в блоке — не стейкхолдер",
			blocks:      []string{"роль: техлид"},
			wantFound:   0,
			wantMissing: []string{"Эпик", "Веха (milestone)", "Риск", "Стейкхолдер"},
		},
	}
	for _, tc := range cases {
		found, missing := c.Coverage(tc.blocks)
		if len(found) != tc.wantFound {
			t.Errorf("%s: найдено %d, ожидалось %d (found=%v missing=%v)", tc.name, len(found), tc.wantFound, found, missing)
		}
		if len(missing) != len(tc.wantMissing) {
			t.Errorf("%s: отсутствующих %d, ожидалось %d (%v)", tc.name, len(missing), len(tc.wantMissing), missing)
			continue
		}
		for i := range missing {
			if missing[i] != tc.wantMissing[i] {
				t.Errorf("%s: missing[%d] = %q, ожидалось %q", tc.name, i, missing[i], tc.wantMissing[i])
			}
		}
	}
}

// TestBoardCaseCoverageEmpty — кейс без обязательных артефактов не даёт ни
// найденного, ни пропущенного: стадия тогда оценивается по факту схемы.
func TestBoardCaseCoverageEmpty(t *testing.T) {
	var c *BoardCase
	if f, m := c.Coverage([]string{"БД"}); f != nil || m != nil {
		t.Fatalf("nil-кейс вернул результат: %v / %v", f, m)
	}
	c2 := &BoardCase{Grade: GradeJunior}
	if f, m := c2.Coverage([]string{"БД"}); f != nil || m != nil {
		t.Fatalf("кейс без required вернул результат: %v / %v", f, m)
	}
}

// TestCaseForGradeRequired — required доезжает до кейса из конфигурации трека
// (иначе детальная проверка стадии никогда не сработает).
func TestCaseForGradeRequired(t *testing.T) {
	tr := &Track{Board: BoardConfig{Cases: []BoardCase{
		{Grade: GradeMiddle, Title: "Офис", Required: []string{"Файрвол"}},
	}}}
	c := tr.Board.CaseForGrade(GradeMiddle)
	if c == nil {
		t.Fatalf("кейс не найден")
	}
	if len(c.Required) != 1 || c.Required[0] != "Файрвол" {
		t.Fatalf("required не дошёл: %v", c.Required)
	}
	if tr.Board.CaseForGrade(GradeStaff) != nil {
		t.Errorf("кейс для чужого грейда найден")
	}
}
