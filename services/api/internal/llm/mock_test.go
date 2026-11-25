package llm

import (
	"context"
	"strings"
	"testing"
)

// TestChatReplyIsInterviewerSpeech — мок озвучивается кандидату (голосовая
// стадия, dev и LLM_MOCK). Последней репликой в запросе всегда директива модели
// («Кандидат перешёл на эту стадию. Заведи её…»), и эхо-ответ зачитывал именно
// её: кандидат слышал служебное указание вместо вопроса.
func TestChatReplyIsInterviewerSpeech(t *testing.T) {
	m := NewMockProvider()
	req := Request{Messages: []Message{
		{Role: RoleSystem, Content: "Ты интервьюер."},
		{Role: RoleUser, Content: "Кандидат перешёл на эту стадию. Заведи её: представь формат и задай первый вопрос."},
	}}
	resp, err := m.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	for _, leak := range []string{"Заведи её", "представь формат", "Кандидат перешёл"} {
		if strings.Contains(resp.Content, leak) {
			t.Fatalf("в ответ мока попала директива (%q): %q", leak, resp.Content)
		}
	}
	if resp.Content != MockReply {
		t.Fatalf("ответ = %q, want MockReply", resp.Content)
	}
}

// TestChatRecordsRequests — ответ больше не эхо, поэтому проверка «что ушло в
// модель» остаётся только через Calls(): убедимся, что канал не потеряли.
func TestChatRecordsRequests(t *testing.T) {
	m := NewMockProvider()
	want := "Расскажи про последний проект"
	if _, err := m.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: want}}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	calls := m.Calls()
	if len(calls) != 1 || len(calls[0].Messages) != 1 || calls[0].Messages[0].Content != want {
		t.Fatalf("запрос не записан: %+v", calls)
	}
}

// TestChatTurnsDiffer — на dev-стенде без LLM-узла интервьюер не должен вечно
// повторять одну фразу: ходы идут по MockReplies, детерминированно по номеру хода.
func TestChatTurnsDiffer(t *testing.T) {
	m := NewMockProvider()
	req := Request{Messages: []Message{{Role: RoleUser, Content: "давай дальше"}}}
	seen := map[string]bool{}
	for i := range len(MockReplies) {
		resp, err := m.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("chat %d: %v", i, err)
		}
		if resp.Content != MockReplies[i] {
			t.Fatalf("ход %d: %q, want %q", i, resp.Content, MockReplies[i])
		}
		if seen[resp.Content] {
			t.Fatalf("ход %d повторил %q раньше, чем прошлись по списку", i, resp.Content)
		}
		seen[resp.Content] = true
	}
	// цикл замкнут: после последнего хода снова первый ответ
	last, err := m.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("chat после цикла: %v", err)
	}
	if last.Content != MockReplies[0] {
		t.Fatalf("после цикла: %q, want %q", last.Content, MockReplies[0])
	}
}
