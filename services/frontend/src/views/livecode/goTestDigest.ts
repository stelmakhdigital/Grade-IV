/**
 * Разбор сырого вывода `go test -json` в читабельный дайджест (spec-15 UX).
 * Кандидат видел стену машинного JSON (`{"Time":...,"Action":"run",...}`) —
 * диагностика проваленных тестов терялась. Дайджест: per-test статусы с
 * диагностикой (t.Logf/ошибки) + сводка пакетов; сырой вывод — в <details>.
 * Не-json вывод (pytest, ошибки компиляции go) возвращается как есть.
 */

export interface GoTestEvent {
  Action?: string;
  Test?: string;
  Output?: string;
  Elapsed?: number;
  Package?: string;
}

export interface GoTestDigest {
  /** per-test: имя, статус, длительность (с), диагностические строки. */
  tests: Array<{
    name: string;
    passed: boolean | null;
    elapsed: number | null;
    output: string[];
  }>;
  /** Сводка пакетов: «ok …» / «FAIL … [с]». */
  packages: string[];
  /** Сырой вывод (для <details>). */
  raw: string;
}

/** Служебные строки go-test, дублирующие структуру дайджеста. */
const BOILERPLATE = /^(=== RUN|=== PAUSE|=== CONT|--- PASS|--- FAIL|PASS$)/;

export function parseGoTestJSON(stdout: string): GoTestDigest | null {
  const src = stdout ?? '';
  const lines = src.split('\n').map((l) => l.trim()).filter(Boolean);
  // go test -json узнаём по первому событию-объекту с полем Action.
  if (lines.length === 0 || !lines[0].startsWith('{')) return null;
  let first: unknown;
  try {
    first = JSON.parse(lines[0]);
  } catch {
    return null;
  }
  if (first == null || typeof first !== 'object' || !('Action' in first)) return null;

  const events: GoTestEvent[] = [];
  for (const line of lines) {
    if (!line.startsWith('{')) continue; // не-JSON хвост — игнорируем
    try {
      const e = JSON.parse(line) as GoTestEvent;
      if (e != null && typeof e.Action === 'string') events.push(e);
    } catch {
      // битая строка — пропускаем (остальной поток важнее)
    }
  }

  const byTest = new Map<string, { name: string; passed: boolean | null; elapsed: number | null; output: string[] }>();
  const packages: string[] = [];
  for (const e of events) {
    const hasTest = typeof e.Test === 'string' && e.Test !== '';
    let t = hasTest
      ? (byTest.get(e.Test!) ?? { name: e.Test!, passed: null, elapsed: null, output: [] })
      : null;
    if (t != null) byTest.set(e.Test!, t);
    switch (e.Action) {
      case 'pass':
      case 'fail': {
        if (t != null) {
          if (t.passed === null) t.passed = e.Action === 'pass';
          if (t.elapsed === null && typeof e.Elapsed === 'number') t.elapsed = e.Elapsed;
        } else if (typeof e.Package === 'string') {
          const el = typeof e.Elapsed === 'number' ? ` (${e.Elapsed.toFixed(3)}с)` : '';
          packages.push(`${e.Action === 'pass' ? 'ok' : 'FAIL'} ${e.Package}${el}`);
        }
        break;
      }
      case 'output': {
        const line = (e.Output ?? '').replace(/\n+$/, '');
        if (line === '') break;
        if (t != null) {
          if (BOILERPLATE.test(line)) break;
          t.output.push(line);
          break;
        }
        // финальная строка пакета дублирует fail/pass-события — не дублируем
        if (/^(FAIL|ok|PASS)(\t| )/.test(line)) break;
        // голая сводка (FAIL/PASS/ok без пакета) — избыточна
        if (/^(FAIL|ok|PASS)$/.test(line)) break;
        packages.push(line); // вывод вне тестов (например, лог приложения)
        break;
      }
      default:
        break;
    }
  }

  return { tests: [...byTest.values()], packages, raw: src };
}
