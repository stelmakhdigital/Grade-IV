package taskgen

import (
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// Проверки без исполнения кода (реальный gate — TestCodeSeqLiveGate). Смысл
// инвариантов: данные обязаны быть такими, чтобы каждый мутант обязан был
// упасть. Мутант, который «не заметен» на сгенерированных данных, даёт кандидату
// честный на вид, но пустой балл — ловить это надо здесь, а не глазами.

func TestCodeSeqFamiliesShape(t *testing.T) {
	for _, fam := range []string{"code-seq-go", "code-seq-py"} {
		stack, ok := StackOf(fam)
		if !ok {
			t.Fatalf("%s: стек не объявлен", fam)
		}
		want := map[string]string{"code-seq-go": "go", "code-seq-py": "python"}[fam]
		if stack != want {
			t.Errorf("%s: стек %q, ожидалось %q", fam, stack, want)
		}
		for _, grade := range []string{"junior", "middle", "senior"} {
			d, err := Build(fam, 5, grade)
			if err != nil {
				t.Fatalf("%s/%s: %v", fam, grade, err)
			}
			if strings.TrimSpace(d.Statement) == "" || strings.TrimSpace(d.Title) == "" {
				t.Errorf("%s/%s: пустое условие или заголовок", fam, grade)
			}
			if len(d.Mutants) < 3 {
				t.Errorf("%s/%s: мутантов %d, ожидалось >= 3", fam, grade, len(d.Mutants))
			}
			// Мутант обязан отличаться от эталона, иначе он не мутант.
			var refFile, refText string
			for f, c := range d.Reference {
				refFile, refText = f, c
			}
			for i, m := range d.Mutants {
				if m[refFile] == refText {
					t.Errorf("%s/%s: мутант %d совпадает с эталоном", fam, grade, i)
				}
				for f := range m {
					if f != refFile {
						t.Errorf("%s/%s: мутант %d правит %q, а не %q", fam, grade, i, f, refFile)
					}
				}
			}
			// Заготовка и тест говорят про одну и ту же функцию: переименование
			// в одном месте поймается здесь, а не на прогоне gate.
			stub := d.Files[refFile]
			if stub == "" {
				t.Fatalf("%s/%s: в файлах нет %q", fam, grade, refFile)
			}
			name := funcName(grade, stack)
			if !strings.Contains(stub, name) {
				t.Errorf("%s/%s: заготовка не объявляет %s", fam, grade, name)
			}
			if !strings.Contains(d.Files[graderFile(stack)], name) {
				t.Errorf("%s/%s: тест банка не вызывает %s", fam, grade, name)
			}
			// Эталон обязан определять то же имя.
			if !strings.Contains(refText, name) {
				t.Errorf("%s/%s: эталон не определяет %s", fam, grade, name)
			}
			_ = refText
			// Файл тестов — доверенный: версия кандидата выбрасывается раннером.
			if !tasks.IsTrustedFile(graderFile(stack), stack) {
				t.Errorf("%s: %q не считается доверенным файлом", fam, graderFile(stack))
			}
		}
		if _, err := Build(fam, 5, "staff"); err == nil {
			t.Errorf("%s: грейд staff обещан, но не собрался", fam)
		}
	}
}

func TestCodeSeqDeterministic(t *testing.T) {
	for _, fam := range []string{"code-seq-go", "code-seq-py"} {
		a, err := Build(fam, 777, "middle")
		if err != nil {
			t.Fatal(err)
		}
		b, err := Build(fam, 777, "middle")
		if err != nil {
			t.Fatal(err)
		}
		for f, c := range a.Files {
			if b.Files[f] != c {
				t.Errorf("%s: %q зависит от вызова (недетерминированно)", fam, f)
			}
		}
		c, err := Build(fam, 778, "middle")
		if err != nil {
			t.Fatal(err)
		}
		if c.Files[graderFile(stackOfTest(fam))] == a.Files[graderFile(stackOfTest(fam))] {
			t.Errorf("%s: другой seed дал те же тесты", fam)
		}
	}
}

// TestCodeSeqDataInvariants — то, без чего мутанты молчат.
func TestCodeSeqDataInvariants(t *testing.T) {
	goJunior, err := Build("code-seq-go", 3, "junior")
	if err != nil {
		t.Fatal(err)
	}
	test := goJunior.Files["solution_test.go"]
	if !strings.Contains(test, ", 0, []int{}") || !strings.Contains(test, ", -2, []int{}") {
		t.Errorf("junior: нет случаев k=0 и k<0 с пустым ожиданием")
	}
	if !strings.Contains(test, "[]int{}, ") {
		t.Errorf("junior: нет случая с пустым входом")
	}
	// Повторы максимума + k меньше длины: иначе «не обрезать по k» и «схлопнуть
	// повторы» проходили бы незаметно.
	if !strings.Contains(test, "5, 5, 5, 5") {
		t.Errorf("junior: нет случая «все равные»")
	}
	if !strings.Contains(test, "-25") {
		t.Errorf("junior: случайный набор без отрицательных значений")
	}

	mid, err := Build("code-seq-py", 3, "middle")
	if err != nil {
		t.Fatal(err)
	}
	pyTest := mid.Files["test_solution.py"]
	for _, w := range []string{"узел", "юнит"} {
		if n := strings.Count(pyTest, `"`+w+`"`); n < 5 {
			t.Errorf("middle: слово %q встречается %d раз — ничейной частоты нет", w, n)
		}
	}
	if !strings.Contains(pyTest, "k = 0") && !strings.Contains(pyTest, ", 0,") {
		t.Errorf("middle: нет случая k=0")
	}

	sen, err := Build("code-seq-go", 3, "senior")
	if err != nil {
		t.Fatal(err)
	}
	senTest := sen.Files["solution_test.go"]
	// Формы, которые могут появиться только если ожидания правда склеены:
	// [100,105]+[103,110] → [100,110], [200,210]+[211,215] → [200,215].
	for _, frag := range []string{
		"{100, 110}",     // перекрывающиеся склеены
		"{200, 215}",     // соседние склеены
		"{1, 2}, {4, 5}", // не склеиваются — тоже должно быть
		"[][2]int{},",    // пустой вход
	} {
		if !strings.Contains(senTest, frag) {
			t.Errorf("senior: в тестах нет %q", frag)
		}
	}
	for _, frag := range []string{"{100, 105}", "{103, 110}", "{200, 210}", "{211, 215}", "{300, 350}", "{305, 310}"} {
		if !strings.Contains(senTest, frag) {
			t.Errorf("senior: во входах нет отрезка %s", frag)
		}
	}
}

func graderFile(stack string) string {
	if stack == "go" {
		return "solution_test.go"
	}
	return "test_solution.py"
}

// funcName — имя функции-обязанности: у go и python свои, путать их нельзя.
func funcName(grade, stack string) string {
	if stack == "python" {
		switch grade {
		case "junior":
			return "top_k"
		case "middle":
			return "top_words"
		default:
			return "merge_intervals"
		}
	}
	switch grade {
	case "junior":
		return "TopK"
	case "middle":
		return "TopWords"
	default:
		return "MergeIntervals"
	}
}

func stackOfTest(fam string) string {
	st, _ := StackOf(fam)
	return st
}
