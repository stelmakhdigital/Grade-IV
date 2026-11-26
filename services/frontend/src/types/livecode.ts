/** Результат запуска тестов Live-Code / практики (§4.4). */

export interface RunTest {
  name: string;
  passed: boolean;
  output?: string;
}

export interface RunResult {
  exit_code: number;
  stdout: string;
  stderr: string;
  duration_ms: number;
  passed: boolean;
  /**
   * BUG-20261011-013: бэкенд отдаёт `"tests": null` (nil-slice в Go), когда
   * тесты не запускались (compile-ошибка, нет тулчейна, timeout-setup).
   * runTests() нормализует null в [] — здесь тип уже «после нормализации».
   */
  tests: RunTest[];
}
