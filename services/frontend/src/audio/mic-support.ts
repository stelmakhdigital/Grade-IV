/**
 * Доступен ли микрофон в принципе — проверка вне класса захвата: вне
 * secure context браузер не даёт navigator.mediaDevices.
 */

/**
 * Причина, по которой микрофон недоступен в принципе (или null, если доступен).
 *
 * Вне secure context (http://<IP> — типичный запуск с телефона в LAN) у
 * браузера нет navigator.mediaDevices: getUserMedia не существует, запрос
 * разрешения даже не показывается. Подсказка «разрешите в браузере» там
 * только путает — доступ не разрешают, а открывают страницу по HTTPS
 * (или localhost).
 */
export function micUnsupportedReason(): string | null {
  const getUserMedia = typeof navigator === 'undefined'
    ? undefined
    : navigator.mediaDevices?.getUserMedia;
  if (typeof getUserMedia === 'function') return null;
  return (
    'Микрофон недоступен на этом адресе: браузер даёт доступ к нему только по '
      + 'HTTPS (или localhost). Откройте страницу по https:// — либо проверьте '
      + 'микрофон на /audio-debug.html.'
  );
}
