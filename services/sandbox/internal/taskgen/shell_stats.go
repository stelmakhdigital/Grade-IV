package taskgen

// Семья shell-stats (spec-24): «сводка по файлу чисел». Одна механика парсинга,
// разная глубина по грейдам — junior (sum/n/avg), middle (+min/max), senior
// (+median), staff (+mode). Взята первой, потому что shell-практика в банке
// держалась на двух задачах (0 задач на senior/staff), а задача семьи
// воспроизводится любым seed.

import (
	"fmt"
	"strconv"
	"strings"
)

func init() {
	register(&Family{
		ID:     "shell-stats",
		Stacks: []string{"shell"},
		Grades: []string{"junior", "middle", "senior", "staff"},
		Build:  buildShellStats,
	})
}

func buildShellStats(seed int64, grade string) (*Draft, error) {
	level := gradeLevel(grade)
	if level == 0 {
		return nil, fmt.Errorf("неизвестный грейд %q", grade)
	}
	cs := generateStats(seed, level)
	keys := statsKeys(level)
	exp := statsExpected(cs.nums, level)

	ref, err := statsReference(keys)
	if err != nil {
		return nil, err
	}
	mutants, err := statsMutants(level, ref)
	if err != nil {
		return nil, err
	}
	return &Draft{
		Title:     "Сводка по файлу: " + strings.Join(keys, ", "),
		Statement: statsStatement(keys),
		Grades:    []string{grade},
		Files: map[string]string{
			"solution.sh": "#!/usr/bin/env bash\n# Ваше решение. Первый аргумент — путь к файлу данных.\n",
			"input.txt":   strings.Join(cs.cells, "\n") + "\n",
			"empty.txt":   "\n",
			"check.sh":    statsCheck(keys, exp),
		},
		Reference: map[string]string{"solution.sh": ref},
		Mutants:   mutants,
	}, nil
}

func statsStatement(keys []string) string {
	desc := map[string]string{
		"sum":    "sum=<сумма значений>",
		"n":      "n=<количество учтённых значений>",
		"avg":    "avg=<среднее; целочисленное деление как в bash $(( )), округление к нулю>",
		"min":    "min=<минимальное значение>",
		"max":    "max=<максимальное значение>",
		"median": "median=<медиана; при чётном n — среднее двух центральных тем же целочисленным делением>",
		"mode":   "mode=<самое частое значение; при одинаковой частоте — наименьшее из вершин>",
	}
	var b strings.Builder
	b.WriteString("Напишите bash-скрипт solution.sh: первый аргумент — путь к файлу с данными.\n\n")
	b.WriteString("Формат данных: строки, значения разделены пробелом, табуляцией или запятой " +
		"(разделители могут смешиваться в одном файле). Мусор — строка `NA` и пустые ячейки " +
		"(два разделителя подряд): они не входят в статистику. Значения целые, встречаются отрицательные.\n\n")
	b.WriteString("Напечатать ровно " + strconv.Itoa(len(keys)) + " строк в этом порядке:\n")
	for _, k := range keys {
		b.WriteString("  " + desc[k] + "\n")
	}
	b.WriteString("\nПустой файл (значений нет): sum=0, n=0, в остальных ключах NA.\n\n")
	b.WriteString("Инструменты: bash и coreutils (sort, tr, sed, awk можно); сети нет, python/perl — не решение этой задачи.\n")
	b.WriteString("Проверка запускает `bash solution.sh <путь к input.txt>` и `<путь к empty.txt>` и сверяет вывод построчно, вместе с порядком строк.\n")
	return b.String()
}

// Маркировки блоков эталона: мутанты вырезают/меняют текст по ним, а не по
// «примерно похожему месту». В bash это просто комментарии.
const (
	emptyBlockBegin = "# BEGIN-EMPTY"
	emptyBlockEnd   = "# END-EMPTY"
)

// statsReference — эталонное решение. Печатает ровно те ключи, которые требует
// уровень, иначе не пройдёт проверку на число строк.
func statsReference(keys []string) (string, error) {
	need := map[string]bool{}
	for _, k := range keys {
		need[k] = true
	}
	var b strings.Builder
	b.WriteString(`#!/usr/bin/env bash
set -u
f="${1:-}"
nums=()
if [ -n "$f" ] && [ -s "$f" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line//$'\t'/ }"; line="${line//,/ }"
    for tok in $line; do
      tok="${tok#+}"
      case "$tok" in NA|'') continue ;; esac
      if [[ "$tok" =~ ^-?[0-9]+$ ]]; then nums+=("$tok"); fi
    done
  done < "$f"
fi
n=${#nums[@]}
`)
	b.WriteString(emptyBlockBegin + "\n")
	b.WriteString("if [ \"$n\" -eq 0 ]; then\n")
	for _, k := range keys {
		v := "NA"
		if k == "sum" || k == "n" {
			v = "0"
		}
		b.WriteString(fmt.Sprintf("  printf '%s=%s\\n'\n", k, v))
	}
	b.WriteString("  exit 0\nfi\n")
	b.WriteString(emptyBlockEnd + "\n")

	b.WriteString(`sum=0; min=${nums[0]}; max=${nums[0]}
for x in "${nums[@]}"; do
  sum=$((sum + x))
  (( x < min )) && min=$x
  (( x > max )) && max=$x
done
avg=$(( sum / n ))
printf 'sum=%s\n' "$sum"
printf 'n=%s\n' "$n"
printf 'avg=%s\n' "$avg"
`)
	if need["min"] {
		b.WriteString("printf 'min=%s\\n' \"$min\"\nprintf 'max=%s\\n' \"$max\"\n")
	}
	if need["median"] {
		b.WriteString(`mapfile -t sorted < <(printf '%s\n' "${nums[@]}" | sort -n)
if (( n % 2 == 1 )); then med=${sorted[n/2]}; else med=$(( (sorted[n/2-1] + sorted[n/2]) / 2 )); fi
printf 'median=%s\n' "$med"
`)
	}
	if need["mode"] {
		b.WriteString(`declare -A cnt
for x in "${nums[@]}"; do cnt[$x]=$(( ${cnt[$x]:-0} + 1 )); done
best=""; bestc=0
for k in $(printf '%s\n' "${!cnt[@]}" | sort -n); do
  c=${cnt[$k]}
  if (( c > bestc )); then best=$k; bestc=$c; fi
done
printf 'mode=%s\n' "$best"
`)
	}
	return b.String(), nil
}

// statsCheck — чек банка: точное число строк, каждая ожидаемая строка, поведение
// на пустом файле. Причины — в stderr (stdout прогона уходит кандидату).
func statsCheck(keys []string, exp map[string]string) string {
	var b strings.Builder
	b.WriteString(`#!/usr/bin/env bash
# Чек банка: доверенный файл, лежит вне каталога решения (BUG-20261122-033).
set -u
fail() { echo "FAIL: $1" >&2; exit 1; }
expect() { printf '%s\n' "$2" | grep -qx -- "$1" || fail "нет строки $1 (вывод: $(printf '%s' "$2" | tr '\n' '|'))"; }
lines() { printf '%s\n' "$1" | wc -l; }
# Данные лежат рядом с чеком: их задаёт банк, а не кандидат (submit-версии
# input.txt раннер выбрасывает).
DIR=$(cd -- "$(dirname -- "$0")" && pwd)
`)
	b.WriteString(fmt.Sprintf("K=%d\n", len(keys)))
	b.WriteString(`OUT=$(bash solution.sh "$DIR/input.txt") || fail "решение завершилось с ошибкой"
[ "$(lines "$OUT")" -eq "$K" ] || fail "ожидалось $K строк, получено $(lines "$OUT") (вывод: $(printf '%s' "$OUT" | tr '\n' '|'))"
`)
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("expect '%s=%s' \"$OUT\"\n", k, exp[k]))
	}
	b.WriteString(`EMPTY=$(bash solution.sh "$DIR/empty.txt") || fail "на пустом файле решение завершилось с ошибкой"
[ "$(lines "$EMPTY")" -eq "$K" ] || fail "пустой файл: ожидалось $K строк, получено $(lines "$EMPTY")"
`)
	for _, k := range keys {
		v := "NA"
		if k == "sum" || k == "n" {
			v = "0"
		}
		b.WriteString(fmt.Sprintf("expect '%s=%s' \"$EMPTY\"\n", k, v))
	}
	b.WriteString("echo OK\n")
	return b.String()
}

// statsMutants — заведомо неправильные решения. Каждое — правка эталона в одном
// месте; если правка не изменила текст (эталон переехал, маркер пропал) —
// ошибка, а не тихий «мутант, который не мутант».
func statsMutants(level int, ref string) ([]map[string]string, error) {
	type patch struct {
		name     string
		old      string
		replace  string
		cutBlock bool // вырезать блок между метками (мутант «пустой файл не обработан»)
	}
	patches := []patch{
		{
			name:    "NA считается нулём",
			old:     `case "$tok" in NA|'') continue ;; esac`,
			replace: `case "$tok" in '') continue ;; NA) nums+=("0"); continue ;; esac`,
		},
		{
			name:    "avg округляется к ближайшему, а не к нулю",
			old:     "avg=$(( sum / n ))",
			replace: `avg=$(awk -v s="$sum" -v n="$n" 'BEGIN{ if (s>=0) printf "%d", int((s*2+n)/(2*n)); else printf "%d", -int(((0-s)*2+n)/(2*n)) }')`,
		},
	}
	if level >= 2 {
		patches = append(patches,
			patch{
				name:    "минимум ищется как максимум",
				old:     "(( x < min )) && min=$x",
				replace: "(( x > min )) && min=$x",
			},
		)
	}
	if level >= 3 {
		patches = append(patches,
			patch{
				name:    "медиана берётся «верхняя»",
				old:     "med=$(( (sorted[n/2-1] + sorted[n/2]) / 2 ))",
				replace: "med=$(( (sorted[n/2-1] + sorted[n/2] + 1) / 2 ))",
			},
		)
	}
	if level >= 4 {
		patches = append(patches,
			patch{
				name:    "при перевязке mod берётся большее значение",
				old:     "for k in $(printf '%s\\n' \"${!cnt[@]}\" | sort -n); do",
				replace: "for k in $(printf '%s\\n' \"${!cnt[@]}\" | sort -rn); do",
			},
		)
	}
	// Пустой файл без отдельной ветки: деление на ноль и падающий скрипт.
	patches = append(patches, patch{name: "пустой файл не обработан", cutBlock: true})

	out := make([]map[string]string, 0, len(patches))
	for _, p := range patches {
		s := ref
		if p.cutBlock {
			i := strings.Index(s, emptyBlockBegin)
			j := strings.Index(s, emptyBlockEnd)
			if i < 0 || j < 0 || j <= i {
				return nil, fmt.Errorf("мутант %q: метки блока пустого файла не найдены", p.name)
			}
			s = s[:i] + s[j+len(emptyBlockEnd):]
		} else {
			if n := strings.Count(s, p.old); n != 1 {
				return nil, fmt.Errorf("мутант %q: правка не уникальна в эталоне (%d совпадений)", p.name, n)
			}
			s = strings.Replace(s, p.old, p.replace, 1)
			if s == ref {
				return nil, fmt.Errorf("мутант %q: правка ничего не изменила", p.name)
			}
		}
		// Маркеры блоков — служебные, в решению кандидата их быть не должно.
		s = strings.ReplaceAll(s, emptyBlockBegin+"\n", "")
		s = strings.ReplaceAll(s, emptyBlockEnd+"\n", "")
		out = append(out, map[string]string{"solution.sh": s})
	}
	return out, nil
}
