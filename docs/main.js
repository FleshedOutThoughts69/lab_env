/* ═══════════════════════════════════════════════════════════════
   lab_env Landing Page — main.js
   Nav observer, drawer, clipboard, filter.
   ═══════════════════════════════════════════════════════════════ */

// ── C-01 IntersectionObserver (active nav link) ──────────────
(function () {
  const sections = document.querySelectorAll('main > section');
  const navLinks = document.querySelectorAll('.nav__link[href^="#"]');
  if (!sections.length || !navLinks.length) return;

  let observer;
  const mq = window.matchMedia('(min-width: 640px)');

  function activate(entries) {
    entries.forEach(entry => {
      if (!entry.isIntersecting) return;
      const headingId = entry.target.getAttribute('aria-labelledby');
      navLinks.forEach(link => {
        const active = link.getAttribute('href') === `#${headingId}`;
        link.classList.toggle('nav__link--active', active);
        link.setAttribute('aria-current', active ? 'true' : 'false');
      });
    });
  }

  function initObserver() {
    if (!mq.matches) {
      // remove observer and clear active states when below 640px
      if (observer) {
        observer.disconnect();
        observer = null;
        navLinks.forEach(link => {
          link.classList.remove('nav__link--active');
          link.setAttribute('aria-current', 'false');
        });
      }
      return;
    }

    if (observer) return; // already set up
    observer = new IntersectionObserver(activate, {
      rootMargin: '-20% 0px -60% 0px'
    });
    sections.forEach(s => observer.observe(s));
  }

  // initial check
  initObserver();

  // watch for changes (rotate, window resize)
  mq.addEventListener('change', initObserver);
})();


// ── C-02 Drawer (mobile) ─────────────────────────────────────
(function () {
  const hamburger = document.querySelector('.nav__hamburger');
  const drawer    = document.getElementById('nav-drawer');
  if (!hamburger || !drawer) return;

  const focusable = () => [...drawer.querySelectorAll(
    'a[href], button:not([disabled])'
  )];
  let savedScrollY = 0;

  function openDrawer() {
    savedScrollY = window.scrollY;
    document.body.style.cssText =
      `overflow:hidden;position:fixed;top:-${savedScrollY}px;width:100%`;

    document.body.setAttribute('data-drawer-open', '');
    drawer.classList.add('drawer--open');
    drawer.setAttribute('aria-hidden', 'false');
    hamburger.setAttribute('aria-expanded', 'true');

    requestAnimationFrame(() => focusable()[0]?.focus());
  }

  function closeDrawer() {
    document.body.removeAttribute('data-drawer-open');
    document.body.style.cssText = '';
    window.scrollTo(0, savedScrollY);

    drawer.classList.remove('drawer--open');
    drawer.setAttribute('aria-hidden', 'true');
    hamburger.setAttribute('aria-expanded', 'false');
    hamburger.focus();
  }

  hamburger.addEventListener('click', openDrawer);

  drawer.querySelector('.drawer__backdrop')
    .addEventListener('click', closeDrawer);

  document.addEventListener('keydown', e => {
    if (e.key === 'Escape' &&
        document.body.hasAttribute('data-drawer-open')) {
      closeDrawer();
    }
  });

  drawer.querySelectorAll('.drawer__link').forEach(link =>
    link.addEventListener('click', closeDrawer)
  );

  drawer.addEventListener('keydown', e => {
    if (e.key !== 'Tab') return;
    const items = focusable();
    if (!items.length) return;
    const first = items[0];
    const last  = items[items.length - 1];

    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  });
})();


// ── C-03 Clipboard (CTAs) ────────────────────────────────────
(function () {
  const GIT_CLONE =
    'git clone https://github.com/FleshedOutThoughts69/lab_env.git /opt/lab-env' +
    ' && cd /opt/lab-env && sudo bash scripts/bootstrap.sh';

  document.querySelectorAll('.cta--clipboard').forEach(btn => {
    const originalText = btn.textContent.trim();

    btn.addEventListener('click', async () => {
      if (!window.matchMedia('(hover: none)').matches) return;

      try {
        await navigator.clipboard.writeText(GIT_CLONE);
        btn.textContent = 'Copied ✓';
        setTimeout(() => { btn.textContent = originalText; }, 1500);
      } catch {
        // Clipboard unavailable – fail silently
      }
    });
  });
})();


// ── C-08 Filter chips ────────────────────────────────────────
(function () {
  const grid    = document.querySelector('.fault-grid');
  const chips   = document.querySelectorAll('.chip');
  const count   = document.querySelector('.filter-count');
  const inline  = document.querySelector('.filter-count-inline');
  if (!grid || !chips.length || !count) return;

  const cards = () => grid.querySelectorAll('.fault-card');

  chips.forEach(chip => {
    chip.addEventListener('click', () => {
      const filterVal = chip.dataset.filter;

      chips.forEach(c => {
        c.classList.remove('chip--active');
        c.setAttribute('aria-checked', 'false');
      });
      chip.classList.add('chip--active');
      chip.setAttribute('aria-checked', 'true');

      const applyFilter = () => {
        let visible = 0;
        cards().forEach(card => {
          const match = filterVal === 'all' ||
                        card.dataset.layer === filterVal;
          card.classList.toggle('filtering-out', !match);
          if (match) visible++;
        });
        const text = `${visible} fault${visible !== 1 ? 's' : ''} shown`;
        count.textContent = text;
        if (inline) inline.textContent = visible;
      };

      if (document.startViewTransition) {
        cards().forEach(card => {
          const match = filterVal === 'all' ||
                        card.dataset.layer === filterVal;
          if (!match) card.classList.add('filtering-out');
        });
        document.startViewTransition(applyFilter);
      } else {
        applyFilter();
      }
    });
  });

  // Escape clears filter
  document.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    const allChip = document.querySelector('.chip[data-filter="all"]');
    if (allChip) {
      allChip.click();
      allChip.focus();
    }
  });
})();