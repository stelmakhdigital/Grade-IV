/**
 * Постоянный выбор человека (не параметр одного интервью): в каком голосе и с
 * каким характером интервьюера он хочет говорить, какая тема интерфейса.
 *
 * Храним в localStorage, а не на сервере: это предпочтения конкретного браузера
 * (тема — точно), сбой сети не должен влиять на старт интервью, и ни одна из
 * этих вещей не является доказательством компетенций в отчёте.
 */
const SPEAKER_KEY = 'grade.tts-speaker';
const PROFILE_KEY = 'grade.profile-id';

export function prefSpeaker(): string {
  return localStorage.getItem(SPEAKER_KEY) ?? '';
}

export function setPrefSpeaker(speaker: string): void {
  if (speaker) localStorage.setItem(SPEAKER_KEY, speaker);
  else localStorage.removeItem(SPEAKER_KEY);
}

export function prefProfileId(): number | null {
  const raw = localStorage.getItem(PROFILE_KEY);
  if (raw === null) return null;
  const n = Number(raw);
  return Number.isInteger(n) && n > 0 ? n : null;
}

export function setPrefProfileId(id: number | null): void {
  if (id && id > 0) localStorage.setItem(PROFILE_KEY, String(id));
  else localStorage.removeItem(PROFILE_KEY);
}
