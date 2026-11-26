package interviewer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

// fakeProvider — scripted-ответы + запись запросов (проверяем, что правка
// дослана именно туда, где модель увидела цитату).
type fakeProvider struct {
	replies []string
	err     error
	calls   []llm.Request
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(_ context.Context, req llm.Request) (llm.Response, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return llm.Response{}, f.err
	}
	i := len(f.calls) - 1
	if i >= len(f.replies) {
		i = len(f.replies) - 1
	}
	return llm.Response{Content: f.replies[i]}, nil
}

func (f *fakeProvider) ChatStream(context.Context, llm.Request) (<-chan string, error) {
	return nil, errors.New("не используется")
}

func newGuarded(f *fakeProvider) *Interviewer {
	return &Interviewer{llm: f, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestLeaksDirective(t *testing.T) {
	yes := []string{
		directiveStageEntry,
		"Кандидат перешёл на эту стадию. Сейчас обсудим кэш.",
		"Хорошо. Заведи её — то есть задам вопрос про кэш.",
	}
	for _, s := range yes {
		if !leaksDirective(s) {
			t.Fatalf("не распознана цитата: %q", s)
		}
	}
	no := []string{
		"Давай продолжим. Расскажи, над чем ты работаешь и что было самым сложным?",
		// «заведи» в другой форме — не цитата директивы
		"Заведи логирование в сервисе и покажи, как оно помогает в проде.",
	}
	for _, s := range no {
		if leaksDirective(s) {
			t.Fatalf("ложная цитата: %q", s)
		}
	}
}

func TestClauseReady(t *testing.T) {
	if clauseReady("Привет, я Грейд") {
		t.Fatal("фраза без границы не готова — озвучивать ещё нечего")
	}
	if !clauseReady("Привет, я Грейд. Дальше") {
		t.Fatal("граница фразы есть — фрагмент готов")
	}
	if !clauseReady(strings.Repeat("а", guardHoldLimit)) {
		t.Fatal("лимит символов должен разрешать озвучку без знаков препинания")
	}
	if clauseReady(strings.Repeat("а", guardHoldLimit-1)) {
		t.Fatal("до лимита ждём")
	}
}

func TestChatGuarded(t *testing.T) {
	clean := "Привет! Расскажи, над чем ты сейчас работаешь?"
	leak := directiveStageEntry + " ..."

	t.Run("чистый ответ — один запрос", func(t *testing.T) {
		f := &fakeProvider{replies: []string{clean}}
		got, err := newGuarded(f).chatGuarded(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "привет"}}})
		if err != nil {
			t.Fatal(err)
		}
		if got != clean || len(f.calls) != 1 {
			t.Fatalf("ответ %q, запросов %d", got, len(f.calls))
		}
	})

	t.Run("цитата — повтор с правкой", func(t *testing.T) {
		f := &fakeProvider{replies: []string{leak, clean}}
		got, err := newGuarded(f).chatGuarded(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "привет"}}})
		if err != nil {
			t.Fatal(err)
		}
		if got != clean {
			t.Fatalf("озвучен не чистый ответ: %q", got)
		}
		if len(f.calls) != 2 {
			t.Fatalf("запросов %d, want 2", len(f.calls))
		}
		last := f.calls[1].Messages[len(f.calls[1].Messages)-1]
		if last.Role != llm.RoleUser || !strings.Contains(last.Content, "служебн") {
			t.Fatalf("правка не дослана: %+v", last)
		}
		// исходная история не должна быть испорчена правкой
		if len(f.calls[0].Messages) != 1 {
			t.Fatalf("первый запрос изменён: %d сообщений", len(f.calls[0].Messages))
		}
	})

	t.Run("цитата и после повтора — нейтральный вопрос", func(t *testing.T) {
		f := &fakeProvider{replies: []string{leak, leak}}
		got, err := newGuarded(f).chatGuarded(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "привет"}}})
		if err != nil {
			t.Fatal(err)
		}
		if leaksDirective(got) {
			t.Fatalf("директива ушла наружу: %q", got)
		}
		if got != llm.MockReply {
			t.Fatalf("ожидан нейтральный вопрос llm.MockReply, получили %q", got)
		}
	})

	t.Run("ошибка модели — наружу err", func(t *testing.T) {
		f := &fakeProvider{err: errors.New("узел недоступен")}
		if _, err := newGuarded(f).chatGuarded(context.Background(), llm.Request{}); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})
}
