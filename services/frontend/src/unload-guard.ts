/**
 * Отбрасывает регистрацию unload-обработчиков.
 *
 * Chromium держит политику `unload` выключенной по умолчанию, и Excalidraw на
 * каждый монтаж доски вызывает `window.addEventListener('unload', ...)` для
 * своего автосохранения в localStorage. Браузер ругается
 * «Permissions policy violation: unload» красным в консоли, а обработчик всё
 * равно не выполняется — то есть поведение уже сегодня «сохранения на unload
 * нет». Отбрасываем саму регистрацию: шума нет, поведение то же.
 *
 * Почему не ослабляем политику заголовком: unload отключён не только у нас, он
 * ломает bfcache (возврат «назад» перезагружал бы SPA целиком), и разрешать его
 * ради чужого обработчика, который мы не ждём, — плохой обмен.
 *
 * Своего unload в приложении нет (в src ни одного addEventListener('unload')),
 * персистентность холста — на бэкенде: PUT /sessions/{id}/whiteboard.
 */

export {};

type AddListener = (
  type: string,
  listener: EventListenerOrEventListenerObject | null,
  options?: AddEventListenerOptions | boolean,
) => void;

const native = window.addEventListener.bind(window) as AddListener;

const guard: AddListener = (type, listener, options) => {
  if (type === 'unload') return;
  native(type, listener, options);
};

// Только window: обработчики элементов (React, Monaco, сам холст) не трогаем.
window.addEventListener = guard as typeof window.addEventListener;
