/* ============================================================
   «Грейд» — логика главной страницы:
   выбор грейда и стека, живая сводка, CTA с конфетти,
   reveal-анимации, счётчики, мобильное меню.
   ============================================================ */
(() => {
  "use strict";

  /* ---------- Данные ---------- */

  const GRADES = {
    junior: {
      name: "Junior",
      color: "#34d399",
      time: "45 минут",
      blocks: [
        "База CS: ООП, коллекции, сложность алгоритмов",
        "Алгоритмы и кодинг на глазах интервьюера",
        "Короткий разбор вашего проекта",
        "Soft skills и мотивация",
      ],
    },
    middle: {
      name: "Middle",
      color: "#22d3ee",
      time: "50 минут",
      blocks: [
        "Deep dive по стеку: {stack}",
        "Алгоритмы и CS на повышенной сложности",
        "Основы системного дизайна: кэширование, балансировка",
        "Поведенческое: конфликты, ownership, ошибки",
      ],
    },
    senior: {
      name: "Senior",
      color: "#a78bfa",
      time: "60 минут",
      blocks: [
        "Архитектура: разбор реального проекта вглубь",
        "Deep dive по стеку: {stack}",
        "System design: highload-сервис с нуля",
        "Поведенческое: влияние, компромиссы, наставничество",
      ],
    },
    staff: {
      name: "Staff / Lead",
      color: "#fb923c",
      time: "75 минут",
      blocks: [
        "Кросс-системный design: масштаб ×10 и его цена",
        "Архитектурные компромиссы: деньги, скорость, риск",
        "Deep dive по стеку: {stack}",
        "Leadership: команда, найм, стратегия направления",
      ],
    },
  };

  const STACKS = {
    go:     { name: "Go",                 color: "#00ADD8", detail: "goroutines, GC, каналы, net/http" },
    python: { name: "Python",             color: "#FFD43B", detail: "GIL, asyncio, внутренности stdlib" },
    js:     { name: "JavaScript / TS",    color: "#F7DF1E", detail: "event loop, типизация, performance" },
    java:   { name: "Java",               color: "#f89820", detail: "JVM, concurrency, streams, память" },
    cpp:    { name: "C++",                color: "#64a4d8", detail: "память, move-семантика, STL" },
    rust:   { name: "Rust",               color: "#dea584", detail: "ownership, lifetimes, async" },
    php:    { name: "PHP",                color: "#8e92d9", detail: "FPM/opcache, очереди, архитектура" },
    kotlin: { name: "Kotlin",             color: "#a78bfa", detail: "coroutines, JVM, идиомы языка" },
  };

  /* ---------- Состояние ---------- */

  const state = { grade: null, stack: null, submitted: false };

  /* ---------- Элементы ---------- */

  const $ = (sel) => document.querySelector(sel);
  const $$ = (sel) => Array.from(document.querySelectorAll(sel));

  const gradeCards = $$(".grade-card");
  const stackChips = $$(".stack-chip");

  const elGrade = $("#sumGrade");
  const elStack = $("#sumStack");
  const elTime = $("#sumTime");
  const elBlocks = $("#sumBlocks");
  const ctaBtn = $("#ctaBtn");
  const summary = $("#summary");
  const toast = $("#toast");
  const toastTimer = { id: null };

  /* ---------- Рендер сводки ---------- */

  function setPick(el, text, color) {
    el.textContent = text || "Не выбран";
    if (color && text) {
      el.classList.remove("empty");
      el.style.color = color;
    } else {
      el.classList.add("empty");
      el.style.color = "";
    }
  }

  function render() {
    const g = state.grade ? GRADES[state.grade] : null;
    const s = state.stack ? STACKS[state.stack] : null;

    setPick(elGrade, g ? g.name : null, g && g.color);
    setPick(elStack, s ? s.name : null, s && s.color);
    elTime.textContent = g ? g.time : "—";

    /* Блоки сессии */
    elBlocks.innerHTML = "";
    if (!g) {
      elBlocks.innerHTML = '<li class="placeholder">Выберите грейд и стек — покажем программу</li>';
    } else {
      g.blocks.forEach((block, i) => {
        const li = document.createElement("li");
        if (block.includes("{stack}")) {
          const text = block.replace(
            "{stack}",
            s ? s.name + " — " + s.detail : "укажите стек, чтобы увидеть детали"
          );
          li.textContent = text;
        } else {
          li.textContent = block;
        }
        li.style.animationDelay = i * 70 + "ms";
        elBlocks.appendChild(li);
      });
    }

    /* Кнопка */
    if (state.submitted) {
      ctaBtn.textContent = "✓ Заявка создана — ждите письма";
      ctaBtn.disabled = true;
      ctaBtn.classList.add("done");
    } else {
      ctaBtn.classList.remove("done");
      if (g && s) {
        ctaBtn.disabled = false;
        ctaBtn.textContent = `Начать интервью · ${g.name} · ${s.name}`;
      } else if (g) {
        ctaBtn.disabled = true;
        ctaBtn.textContent = "Осталось выбрать стек";
      } else if (s) {
        ctaBtn.disabled = true;
        ctaBtn.textContent = "Осталось выбрать грейд";
      } else {
        ctaBtn.disabled = true;
        ctaBtn.textContent = "Выберите грейд и стек";
      }
    }

    summary.classList.toggle("ready", Boolean(g && s) || state.submitted);
  }

  /* ---------- Выбор ---------- */

  function selectFrom(list, key, store) {
    list.forEach((el) => {
      el.classList.toggle("selected", el.dataset[key] === store);
      el.setAttribute("aria-checked", String(el.dataset[key] === store));
    });
  }

  gradeCards.forEach((card) => {
    card.addEventListener("click", () => {
      if (state.submitted) return;
      const id = card.dataset.grade;
      state.grade = state.grade === id ? null : id;
      selectFrom(gradeCards, "grade", state.grade);
      render();
    });
  });

  stackChips.forEach((chip) => {
    chip.addEventListener("click", () => {
      if (state.submitted) return;
      const id = chip.dataset.stack;
      state.stack = state.stack === id ? null : id;
      selectFrom(stackChips, "stack", state.stack);
      render();
    });
  });

  /* ---------- CTA, тост, конфетти ---------- */

  function showToast(html) {
    toast.innerHTML = html;
    toast.classList.add("show");
    clearTimeout(toastTimer.id);
    toastTimer.id = setTimeout(() => toast.classList.remove("show"), 5200);
  }

  function confetti(originX, originY) {
    const colors = ["#22d3ee", "#a78bfa", "#34d399", "#fb923c", "#FFD43B", "#f87171"];
    for (let i = 0; i < 40; i++) {
      const p = document.createElement("i");
      p.className = "confetti";
      p.style.left = originX + "px";
      p.style.top = originY + "px";
      p.style.background = colors[i % colors.length];
      document.body.appendChild(p);

      const angle = Math.random() * Math.PI * 2;
      const dist = 110 + Math.random() * 230;
      const dx = Math.cos(angle) * dist;
      const dy = Math.sin(angle) * dist - 150;
      const rot = 360 + Math.random() * 540;

      const anim = p.animate(
        [
          { transform: "translate(0, 0) rotate(0deg)", opacity: 1 },
          { transform: `translate(${dx}px, ${dy + 340}px) rotate(${rot}deg)`, opacity: 0 },
        ],
        { duration: 950 + Math.random() * 650, easing: "cubic-bezier(.2,.7,.3,1)" }
      );
      anim.onfinish = () => p.remove();
    }
  }

  ctaBtn.addEventListener("click", () => {
    const g = GRADES[state.grade];
    const s = STACKS[state.stack];
    if (!g || !s || state.submitted) return;

    state.submitted = true;
    render();

    const rect = ctaBtn.getBoundingClientRect();
    confetti(rect.left + rect.width / 2, rect.top + rect.height / 2);

    showToast(
      `<strong>Заявка создана!</strong> Инженер по стеку <b>${s.name}</b> (трек <b>${g.name}</b>) напишет вам в течение 24 часов, чтобы назначить удобное время. Это демо — письмо не отправлялось 😉`
    );
  });

  /* ---------- Reveal при скролле ---------- */

  const revealEls = $$(".reveal");
  if ("IntersectionObserver" in window) {
    const io = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry, idx) => {
          if (!entry.isIntersecting) return;
          const el = entry.target;
          el.style.transitionDelay = (idx % 4) * 90 + "ms";
          el.classList.add("visible");
          io.unobserve(el);
        });
      },
      { threshold: 0.15, rootMargin: "0px 0px -40px 0px" }
    );
    revealEls.forEach((el) => io.observe(el));
  } else {
    revealEls.forEach((el) => el.classList.add("visible"));
  }

  /* ---------- Счётчики ---------- */

  function animateCounter(el) {
    const target = parseFloat(el.dataset.target);
    const decimals = parseInt(el.dataset.decimals || "0", 10);
    const dur = 1500;
    const start = performance.now();

    function frame(now) {
      const t = Math.min(1, (now - start) / dur);
      const eased = 1 - Math.pow(1 - t, 3);
      const val = target * eased;
      el.textContent = val.toLocaleString("ru-RU", {
        minimumFractionDigits: decimals,
        maximumFractionDigits: decimals,
      });
      if (t < 1) requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }

  const counters = $$(".counter");
  if ("IntersectionObserver" in window) {
    const cio = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting) return;
          animateCounter(entry.target);
          cio.unobserve(entry.target);
        });
      },
      { threshold: 0.4 }
    );
    counters.forEach((el) => cio.observe(el));
  } else {
    counters.forEach((el) => {
      el.textContent = parseFloat(el.dataset.target).toLocaleString("ru-RU");
    });
  }

  /* ---------- Шапка и меню ---------- */

  const header = $("#siteHeader");
  const onScroll = () => header.classList.toggle("scrolled", window.scrollY > 12);
  window.addEventListener("scroll", onScroll, { passive: true });
  onScroll();

  const navToggle = $("#navToggle");
  const mainNav = $("#mainNav");
  navToggle.addEventListener("click", () => {
    const open = mainNav.classList.toggle("open");
    navToggle.setAttribute("aria-expanded", String(open));
  });
  mainNav.querySelectorAll("a").forEach((a) =>
    a.addEventListener("click", () => {
      mainNav.classList.remove("open");
      navToggle.setAttribute("aria-expanded", "false");
    })
  );

  /* ---------- Год ---------- */

  const yearEl = $("#year");
  if (yearEl) yearEl.textContent = String(new Date().getFullYear());

  render();
})();
