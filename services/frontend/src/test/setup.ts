import '@testing-library/jest-dom/vitest';
import { beforeEach } from 'vitest';

// Часть состояния живёт в адресе (фильтр истории, вкладка админки, номер
// задачи), а jsdom на файл один: без сброса соседний тест стартует с чужим
// ?f= / ?tab= / ?t=.
beforeEach(() => {
  window.location.hash = '#/';
});
