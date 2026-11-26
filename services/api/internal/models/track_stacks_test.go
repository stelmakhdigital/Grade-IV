package models

import "testing"

// Track.HasStack — какие языки роль готова принять на кодовой стадии:
// дефолтный practice.runtime плюс practice_stacks (роль «backend» умеет и go,
// и python). Практика при этом остаётся одной.
func TestTrackHasStack(t *testing.T) {
	multi := &Track{ID: "backend", Practice: &Practice{Runtime: StackGo},
		PracticeStacks: []Stack{StackGo, StackPython}}
	solo := &Track{ID: "dev", Practice: &Practice{Runtime: StackGo}}
	nocode := &Track{ID: "pm"}

	for _, tc := range []struct {
		name  string
		track *Track
		stack Stack
		want  bool
	}{
		{"дефолт роли", multi, StackGo, true},
		{"второй стек роли", multi, StackPython, true},
		{"рантайм без списка", solo, StackGo, true},
		{"роль не умеет", solo, StackPython, false},
		{"роль без практики", nocode, StackGo, false},
		{"пустой стек", multi, Stack(""), false},
	} {
		if got := tc.track.HasStack(tc.stack); got != tc.want {
			t.Errorf("%s: HasStack(%q) = %v, хочу %v", tc.name, tc.stack, got, tc.want)
		}
	}
}
