package httpapi

import (
	"context"
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

func tasksOf(ids ...string) []liveCodeTask {
	out := make([]liveCodeTask, 0, len(ids))
	for _, id := range ids {
		out = append(out, liveCodeTask{ID: id, Title: id})
	}
	return out
}

// TestPickLiveCodeTasksPrefersUnseen — пока свежих задач хватает, ни одна
// уже виденная кандидату не должна попасть в набор (ни на одном seed).
func TestPickLiveCodeTasksPrefersUnseen(t *testing.T) {
	pool := tasksOf("a", "b", "c", "d", "e")
	seen := map[string]bool{"a": true, "b": true, "c": true}
	for seed := int64(1); seed <= 50; seed++ {
		got, reused := pickLiveCodeTasks(models.GradeJunior, pool, nil, seen, rand.New(rand.NewSource(seed)))
		if reused {
			t.Fatalf("seed %d: reused=true, хотя свежих задач достаточно", seed)
		}
		if len(got) != liveCodeTaskCount[models.GradeJunior] {
			t.Fatalf("seed %d: выбрано %d задач", seed, len(got))
		}
		for _, id := range idsOf2(got) {
			if seen[id] {
				t.Fatalf("seed %d: выбрана уже виденная задача %s", seed, id)
			}
		}
	}
}

// TestPickLiveCodeTasksReusesWhenFreshRunsOut — свежее кончилось: берём виденное
// и честно говорим reused=true (не молча повторяем).
func TestPickLiveCodeTasksReusesWhenFreshRunsOut(t *testing.T) {
	pool := tasksOf("a", "b")
	seen := map[string]bool{"a": true, "b": true}
	got, reused := pickLiveCodeTasks(models.GradeJunior, pool, nil, seen, rand.New(rand.NewSource(1)))
	if !reused {
		t.Fatalf("reused=false при пустом свежем пуле")
	}
	if len(got) != liveCodeTaskCount[models.GradeJunior] {
		t.Fatalf("выбрано %d задач, ожидалось %d (повтор лучше голода)", len(got), liveCodeTaskCount[models.GradeJunior])
	}
	if len(idsOf2(got)) != 2 {
		t.Fatalf("задачи повторяются внутри набора: %v", idsOf2(got))
	}
}

// TestPickLiveCodeTasksStretchFresh — stretch-задача тоже берётся свежая, если
// такая есть в пуле следующего грейда.
func TestPickLiveCodeTasksStretchFresh(t *testing.T) {
	pool := tasksOf("a", "b", "c")
	stretch := tasksOf("s-old", "s-new")
	seen := map[string]bool{"s-old": true}
	for seed := int64(1); seed <= 30; seed++ {
		got, _ := pickLiveCodeTasks(models.GradeMiddle, pool, stretch, seen, rand.New(rand.NewSource(seed)))
		last := got[len(got)-1]
		if last.ID != "s-new" {
			t.Fatalf("seed %d: stretch = %s, ожидалось s-new", seed, last.ID)
		}
	}
}

// TestSeenLiveCodeIDs — история берётся по сессиям того же пользователя и без
// текущей сессии (иначе «не повторять» означало бы «не выбрать ничего»).
func TestSeenLiveCodeIDs(t *testing.T) {
	database, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if err := db.Migrate(context.Background(), database, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewSessionStore(database, dialect)
	ctx := context.Background()

	newSession := func(userID int64) int64 {
		s, err := store.Create(ctx, models.Session{
			UserID: userID, Grade: models.GradeJunior, Stack: models.StackGo,
			Stage: models.StageLiveCode, Status: models.StatusActive, DurationLimitS: 1800,
		})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		return s.ID
	}
	addTasks := func(sessionID int64, ids ...string) {
		data, err := json.Marshal(map[string]any{"tasks": tasksOf(ids...)})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := store.AddEvent(ctx, sessionID, "livecode_tasks", data); err != nil {
			t.Fatalf("add event: %v", err)
		}
	}

	old := newSession(1)
	addTasks(old, "go-a", "shell-b")
	other := newSession(2)
	addTasks(other, "go-other-user")
	current := newSession(1)
	addTasks(current, "go-current")

	s := &Server{sessions: store, log: discardLogger()}
	seen := s.seenLiveCodeIDs(ctx, models.Session{ID: current, UserID: 1})
	if !seen["go-a"] || !seen["shell-b"] {
		t.Fatalf("история задач не найдена: %v", seen)
	}
	if seen["go-other-user"] {
		t.Errorf("в историю попали чужие сессии: %v", seen)
	}
	if seen["go-current"] {
		t.Errorf("текущая сессия не исключена: %v", seen)
	}
}

// TestSeenLiveCodeIDsNoUser — сессия без пользователя (анонимный прогон):
// истории нет, молча не паникуем.
func TestSeenLiveCodeIDsNoUser(t *testing.T) {
	s := &Server{log: discardLogger()}
	if got := s.seenLiveCodeIDs(context.Background(), models.Session{ID: 1}); got != nil {
		t.Fatalf("для сессии без пользователя истории быть не должно, получено %v", got)
	}
}
