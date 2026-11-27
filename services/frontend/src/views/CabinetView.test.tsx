import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, type Mock } from 'vitest';
import { setToken } from '../api';
import { AuthProvider } from '../auth';
import { CabinetView } from './CabinetView';

const ME = {
  user: { id: 1, email: 'cand@example.com', created_at: '2026-09-14T00:00:00Z' },
};

const S_FINISHED = {
  id: 1,
  grade: 'senior',
  base_grade: 'middle',
  stack: 'go',
  track_id: 'backend',
  stage: 'report',
  status: 'finished',
  duration_limit_s: 3000,
  active_seconds: 3000,
  time_left_s: 0,
  started_at: '2026-09-14T10:00:00Z',
  finished_at: '2026-09-14T10:50:00Z',
};
const S_ACTIVE = {
  id: 2,
  grade: 'middle',
  stack: 'go',
  track_id: 'backend',
  stage: 'voice',
  status: 'active',
  duration_limit_s: 2700,
  active_seconds: 60,
  time_left_s: 2640,
  started_at: '2026-09-14T11:00:00Z',
};

const PROFILES = [
  { id: 2, name: 'Сбалансированный наставник', tone: 'balanced', difficulty: 'standard', is_preset: true },
  { id: 1, name: 'Строгий Senior', tone: 'strict', difficulty: 'plus', is_preset: true },
];

const RESUME = { about: 'Go 5 лет', file_name: 'resume.pdf', text_chars: 1234 };

function cabinetRoutes(
  opts: {
    sessions?: unknown;
    create?: unknown;
    me?: unknown;
    profiles?: unknown;
    /** Ответ PUT (сохранение). */
    resume?: unknown;
    /** Ответ GET (текущее состояние). По умолчанию — резюме нет. */
    resumeGet?: unknown;
    /** Ответ GET /sessions/{id}/report (для тренда по итогам). */
    report?: unknown;
  } = {},
) {
  return {
    '/auth/me': opts.me ?? ME,
    'PUT /me/resume': opts.resume ?? RESUME,
    '/me/resume': opts.resumeGet ?? { about: '', file_name: '', text_chars: 0 },
    'POST /sessions': opts.create ?? S_ACTIVE,
    '/profiles': opts.profiles ?? PROFILES,
    '/sessions': opts.sessions ?? [],
    '/report': opts.report ?? { overall: 3, grade_recommendation: 'middle', criteria: [], strengths: [], weaknesses: [], recommendations: [] },
  };
}

/** Рендер кабинета; возвращает fetch-mock — ассерты по URL, методу и телу. */
function renderCabinet(opts: Parameters<typeof cabinetRoutes>[0] = {}) {
  const fetchMock = __api.on(cabinetRoutes(opts));
  render(
    <AuthProvider>
      <CabinetView />
    </AuthProvider>,
  );
  return fetchMock;
}

/** Тело запроса старт-сессии (POST /sessions). */
function startBody(fetchMock: Mock): Record<string, unknown> {
  const call = fetchMock.mock.calls.find((c) => c[1]?.method === 'POST' && String(c[0]).endsWith('/sessions'));
  return JSON.parse(((call?.[1] as RequestInit).body as string) ?? '{}');
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
});

describe('CabinetView (WP-7)', () => {
  it('email и длительность интервью вместо баланса; история со статусами и действиями', async () => {
    renderCabinet({ sessions: [S_ACTIVE, S_FINISHED] });
    expect(await screen.findByTestId('user-email')).toBeInTheDocument();
    // Баланса минут больше нет; время — метрика: суммарная длительность сессий.
    expect(screen.queryByTestId('minutes')).not.toBeInTheDocument();
    expect(await screen.findByTestId('total-time')).toHaveTextContent('51 мин');
    // Роль и грейд не выбираются — ИИ берёт их из резюме.
    expect(screen.queryByLabelText('Грейды')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Роль')).not.toBeInTheDocument();
    expect(screen.getByText('активна')).toBeInTheDocument();
    expect(screen.getByText('завершена')).toBeInTheDocument();
    // уровень: цель senior, база по резюме middle
    expect(screen.getByText(/по резюме middle/)).toBeInTheDocument();
    // завершённая → «Отчёт» (spec-20), активная → «Продолжить»
    const links = screen.getAllByRole('link');
    expect(links.some((l) => l.textContent === 'Отчёт')).toBe(true);
    expect(links.some((l) => l.textContent === 'Продолжить')).toBe(true);
  });

  it('пустая история — подсказка', async () => {
    renderCabinet({ sessions: [] });
    expect(await screen.findByText(/Сессий пока нет/)).toBeInTheDocument();
  });

  it('старт: роль и грейд не отправляем — их подбирает ИИ по резюме', async () => {
    const fetchMock = renderCabinet({ sessions: [], create: S_ACTIVE });
    await screen.findByTestId('user-email');
    const user = userEvent.setup();
    await user.click(screen.getByTestId('start-session'));
    expect(window.location.hash).toBe('#/sessions/2');
    // профиль дефолтный (balanced/standard, id=2); ни grade, ни track_id, ни stack.
    expect(startBody(fetchMock)).toEqual({ profile_id: 2 });
  });

  it('выбор профиля интервьюера — profile_id в body (итерация B)', async () => {
    const fetchMock = renderCabinet({ sessions: [], create: S_ACTIVE });
    await screen.findByTestId('user-email');
    const user = userEvent.setup();
    await user.selectOptions(screen.getByLabelText('Профиль интервьюера'), '1');
    await user.click(screen.getByTestId('start-session'));
    expect(startBody(fetchMock)).toEqual({ profile_id: 1 });
  });

  it('резюме: индикатор из GET, сохранение — multipart PUT', async () => {
    const fetchMock = renderCabinet({ resume: RESUME, resumeGet: RESUME });
    await screen.findByTestId('resume-indicator');
    expect(screen.getByTestId('resume-indicator')).toHaveTextContent('resume.pdf');
    expect(screen.getByTestId('resume-indicator')).toHaveTextContent('1234');

    const user = userEvent.setup();
    await user.click(screen.getByTestId('resume-open'));
    const about = await screen.findByTestId('resume-about');
    expect(about).toHaveValue('Go 5 лет');
    const file = screen.getByTestId('resume-file') as HTMLInputElement;
    await user.upload(file, new File(['%PDF-1.4'], 'resume.pdf', { type: 'application/pdf' }));
    expect(screen.getByTestId('resume-current')).toHaveTextContent('будет загружен: resume.pdf');
    await user.click(screen.getByTestId('resume-save'));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find((c) => c[1]?.method === 'PUT');
      expect(call).toBeTruthy();
      expect(String(call![0])).toBe('/api/v1/me/resume');
      expect(call![1]!.body).toBeInstanceOf(FormData);
    });
    expect(screen.queryByTestId('resume-save')).not.toBeInTheDocument();
  });

  it('резюме нет — индикатор подсказывает заполнить', async () => {
    renderCabinet();
    await screen.findByTestId('resume-indicator');
    expect(screen.getByTestId('resume-indicator')).toHaveTextContent('не заполнено');
  });

  it('диалог: Escape закрывает при фокусе вне диалога, Tab не уходит в фон, фокус возвращается на кнопку', async () => {
    renderCabinet({ resumeGet: RESUME });
    await screen.findByTestId('resume-indicator');
    const user = userEvent.setup();
    await user.click(screen.getByTestId('resume-open'));
    await screen.findByTestId('resume-about');

    // ловушка Tab: из последнего элемента диалога — в первый, а не в фон
    const items = () =>
      Array.from(
        document.querySelectorAll<HTMLElement>('[role="dialog"] button, [role="dialog"] textarea, [role="dialog"] input'),
      );
    items().at(-1)!.focus();
    await user.tab();
    expect(items()).toContain(document.activeElement);
    items()[0].focus();
    await user.tab({ shift: true });
    expect(items()).toContain(document.activeElement);

    // Escape работает, даже когда фокус оказался на странице за диалогом
    (document.activeElement as HTMLElement).blur();
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByTestId('resume-about')).not.toBeInTheDocument());
    expect(document.activeElement).toBe(screen.getByTestId('resume-open'));
  });
});

describe('история: страницы, фильтр в URL, инертный фон диалога (ui-review 2026-11-27)', () => {
  beforeEach(() => {
    window.location.hash = '#/';
  });

  it('больше 20 сессий — показаны 20 и кнопка «Показать ещё»', async () => {
    const many = Array.from({ length: 25 }, (_, i) => ({
      ...S_ACTIVE,
      id: 100 + i,
      started_at: `2026-09-15T1${i % 10}:00:00Z`,
    }));
    renderCabinet({ sessions: many });
    await screen.findByTestId('user-email');
    expect(document.querySelectorAll('tbody tr').length).toBe(20);
    expect(screen.getByTestId('hist-more')).toHaveTextContent('Показать ещё 5 из 5');
    const user = userEvent.setup();
    await user.click(screen.getByTestId('hist-more'));
    expect(document.querySelectorAll('tbody tr').length).toBe(25);
  });

  it('фильтр пишется в адрес — F5 и «Назад» его сохраняют', async () => {
    renderCabinet({ sessions: [S_ACTIVE, S_FINISHED] });
    await screen.findByTestId('user-email');
    const user = userEvent.setup();
    const chip = screen.getByTestId('hist-filter-active');
    expect(chip).toHaveAttribute('aria-pressed', 'false');
    await user.click(chip);
    expect(window.location.hash).toBe('#/?f=active');
    expect(screen.getByTestId('hist-filter-active')).toHaveAttribute('aria-pressed', 'true');
    // в фильтре только активные
    await waitFor(() => expect(document.querySelectorAll('tbody tr').length).toBe(1));
  });

  it('открытый диалог делает фон inert и aria-hidden, закрытый — снимает', async () => {
    renderCabinet({ resumeGet: RESUME });
    await screen.findByTestId('resume-indicator');
    const header = () => document.querySelector('.header') as HTMLElement;
    expect(header().hasAttribute('inert')).toBe(false);
    const user = userEvent.setup();
    await user.click(screen.getByTestId('resume-open'));
    await screen.findByTestId('resume-about');
    expect(header().hasAttribute('inert')).toBe(true);
    expect(header()).toHaveAttribute('aria-hidden', 'true');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByTestId('resume-about')).not.toBeInTheDocument());
    expect(header().hasAttribute('inert')).toBe(false);
  });
});

describe('CabinetView: тренд по итогам', () => {
  const S_DONE_2 = { ...S_FINISHED, id: 3, started_at: '2026-09-15T10:00:00Z', finished_at: '2026-09-15T10:40:00Z' };

  it('отчёт по завершённой сессии грузится один раз — «Обновить» не перезапускает веер', async () => {
    const fetchMock = renderCabinet({ sessions: [S_FINISHED, S_DONE_2] });
    const reportCalls = () => fetchMock.mock.calls.filter((c) => String(c[0]).endsWith('/report')).length;
    await screen.findByTestId('trend-row');
    expect(reportCalls()).toBe(2);

    const total = fetchMock.mock.calls.length;
    await userEvent.click(screen.getByRole('button', { name: 'Обновить' }));
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(total));
    // 50 мс — чтобы цепочка «список пришёл → состояние обновилось → эффект»
    // точно успела бы выпустить лишние запросы, если фильтра нет.
    await new Promise((r) => setTimeout(r, 50));
    expect(reportCalls()).toBe(2);
  });
});
