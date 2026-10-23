'use strict';

                                                                               
                                                                         
                                                  
                                                                               
                                                                              
                                                                         
                                                                           
                                                                  
                                                                               
(function () {
  const docEl = document.documentElement;
  const body = document.body;
  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

  const RAIL_KEY = 'cockpit_nav_rail';
  const UI_PREFS_KEY = 'cockpit_ui_prefs_v2';
  const MOBILE = 920;

  const DEFAULT_UI_PREFS = {
    accent: 'orange',
    surface: 'glass',
    navLayout: 'topbar',
    width: 'wide',
    scale: 'md',
    showKpis: true,
    showCharts: true,
    showScanner: true,
  };

  const MODULE_CODES = {
    home: 'MOD-01 // TRIAGE & DISPATCH',
    orders: 'MOD-02 // ORDER PIPELINE & SLA',
    workload: 'MOD-03 // CAPACITY & FLOOR LOAD',
    checklists: 'MOD-04 // PICK LIST TEMPLATES',
    quality: 'MOD-05 // QUALITY TELEMETRY',
    qcbench: 'MOD-06 // QC VERIFICATION BENCH',
    manuals: 'MOD-07 // RUNBOOK SNAPSHOTS',
    products: 'MOD-08 // HARDWARE CATALOG',
    si: 'MOD-09 // SI INTEGRATIONS',
    users: 'MOD-10 // OPERATORS & ROLES',
    devices: 'MOD-11 // RAN PREP TELEMETRY',
    config: 'MOD-12 // SYSTEM & SLA ENGINE',
    capa: 'MOD-13 // CORRECTIVE ACTIONS',
    automations: 'MOD-14 // WORKFLOW AUTOMATION',
  };

  const isMobile = () => window.innerWidth <= MOBILE;

  function loadUIPrefs() {
    try {
      const raw = localStorage.getItem(UI_PREFS_KEY);
      if (!raw) return { ...DEFAULT_UI_PREFS };
      return { ...DEFAULT_UI_PREFS, ...JSON.parse(raw) };
    } catch {
      return { ...DEFAULT_UI_PREFS };
    }
  }

  function saveUIPrefs(prefs) {
    try {
      localStorage.setItem(UI_PREFS_KEY, JSON.stringify(prefs));
    } catch { }
  }

                                                                                  
  function applyRail(rail) {
    body.classList.toggle('nav-rail', !!rail);
    const btn = $('[data-ui="nav-rail"]');
    if (btn) {
      btn.setAttribute('aria-pressed', String(!!rail));
      btn.setAttribute('title', rail ? 'Expand navigation' : 'Collapse navigation');
      btn.setAttribute('aria-label', rail ? 'Expand navigation' : 'Collapse navigation');
      const svg = btn.querySelector('svg path');
      if (svg) svg.setAttribute('d', rail ? 'M9 6l6 6-6 6' : 'M15 6l-6 6 6 6');
    }
  }

                                                                                 
  function applyUIPrefs() {
    const prefs = loadUIPrefs();
    docEl.setAttribute('data-accent', prefs.accent || 'orange');
    docEl.setAttribute('data-surface', prefs.surface || 'glass');
    docEl.setAttribute('data-width', prefs.width || 'wide');
    docEl.setAttribute('data-scale', prefs.scale || 'md');

    if (!docEl.getAttribute('data-density')) {
      docEl.setAttribute('data-density', localStorage.getItem('cockpit_density') || 'spacious');
    }

    const navLayout = prefs.navLayout || 'topbar';
    body.setAttribute('data-nav-layout', navLayout);

    if (navLayout === 'rail') {
      applyRail(true);
    } else if (navLayout === 'both' || navLayout === 'sidebar') {
      let rail = false;
      try { rail = localStorage.getItem(RAIL_KEY) === '1'; } catch { }
      applyRail(rail);
    } else {
      applyRail(false);
    }

    body.classList.toggle('hide-kpis', prefs.showKpis === false);
    body.classList.toggle('hide-charts', prefs.showCharts === false);
    body.classList.toggle('hide-scanner', prefs.showScanner === false);

    syncPrefControls(prefs);
    requestAnimationFrame(updateTopbarScrollState);
  }

  function syncPrefControls(prefs = loadUIPrefs()) {
    $$('.pref-swatch[data-accent]').forEach((sw) => {
      const on = sw.dataset.accent === (prefs.accent || 'orange');
      sw.classList.toggle('active', on);
      sw.setAttribute('aria-checked', String(on));
    });
    const surf = $('#pref-surface');
    if (surf) surf.value = prefs.surface || 'glass';
    const navL = $('#pref-nav-layout');
    if (navL) navL.value = prefs.navLayout || 'topbar';
    const widthEl = $('#pref-width');
    if (widthEl) widthEl.value = prefs.width || 'wide';
    const scaleEl = $('#pref-scale');
    if (scaleEl) scaleEl.value = prefs.scale || 'md';
    const kpisEl = $('#pref-show-kpis');
    if (kpisEl) kpisEl.checked = prefs.showKpis !== false;
    const chartsEl = $('#pref-show-charts');
    if (chartsEl) chartsEl.checked = prefs.showCharts !== false;
    const scanEl = $('#pref-show-scanner');
    if (scanEl) scanEl.checked = prefs.showScanner !== false;
    const densEl = $('#pref-density');
    if (densEl) densEl.value = localStorage.getItem('cockpit_density') || 'spacious';
  }

  function updateSinglePref(key, value) {
    const prefs = loadUIPrefs();
    prefs[key] = value;
    saveUIPrefs(prefs);
    applyUIPrefs();
  }

  function resetAllPrefs() {
    try {
      localStorage.removeItem(UI_PREFS_KEY);
      localStorage.removeItem(RAIL_KEY);
      localStorage.setItem('cockpit_theme', 'dark');
      localStorage.setItem('cockpit_density', 'spacious');
    } catch { }
    docEl.setAttribute('data-theme', 'dark');
    docEl.setAttribute('data-density', 'spacious');
    const themeSel = $('#pref-theme');
    if (themeSel) themeSel.value = 'dark';
    applyUIPrefs();
  }

                                                                                 
  function setDrawer(open) {
    body.classList.toggle('nav-open', open);
    const btn = $('[data-ui="nav-toggle"]');
    if (btn) btn.setAttribute('aria-expanded', String(open));
    const side = $('.sidebar');
    if (side) {
      if (open) side.removeAttribute('aria-hidden');
      else side.setAttribute('aria-hidden', 'true');
      side.inert = !open && (isMobile() || body.getAttribute('data-nav-layout') === 'topbar');
    }
  }

  function initDrawer() {
    if (isMobile() || body.getAttribute('data-nav-layout') === 'topbar') {
      const side = $('.sidebar');
      if (side) { side.setAttribute('aria-hidden', 'true'); side.inert = true; }
    }
    let resizeTimer = null;
    window.addEventListener('resize', () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => {
        if (!isMobile() && body.classList.contains('nav-open') && body.getAttribute('data-nav-layout') !== 'topbar') {
          setDrawer(false);
        }
        updateTopbarScrollState();
      }, 120);
    });
  }

                                                                                
  function updateTopbarScrollState() {
    const shell = $('#tb-scroll-shell');
    const track = $('#tb-scroll-track');
    if (!shell || !track) return;
    const maxScroll = Math.max(0, track.scrollWidth - track.clientWidth);
    const canLeft = track.scrollLeft > 3;
    const canRight = maxScroll > 3 && track.scrollLeft < maxScroll - 3;
    shell.classList.toggle('can-scroll-left', canLeft);
    shell.classList.toggle('can-scroll-right', canRight);
    shell.classList.toggle('is-scrollable', maxScroll > 3);
  }

  function scrollTopbarBy(delta) {
    const track = $('#tb-scroll-track');
    if (!track) return;
    track.scrollBy({ left: delta, behavior: 'smooth' });
  }

  function centerActiveTopbarPill(smooth = true) {
    const track = $('#tb-scroll-track');
    if (!track) return;
    const active = track.querySelector('.tb-pill.active:not(.hidden)');
    if (!active) return;
    const targetLeft = active.offsetLeft - (track.clientWidth / 2) + (active.offsetWidth / 2);
    track.scrollTo({
      left: Math.max(0, targetLeft),
      behavior: smooth ? 'smooth' : 'auto',
    });
    setTimeout(updateTopbarScrollState, smooth ? 240 : 20);
  }

  function syncTopbarNav(smoothScroll = false) {
    const page = currentPage();
    $$('.tb-pill[data-tb-nav]').forEach((pill) => {
      const key = pill.dataset.tbNav;
      const sideLink = $('#nav-' + key);
      if (sideLink) {
        pill.classList.toggle('hidden', sideLink.classList.contains('hidden'));
                                                                     
        if (!pill.querySelector('svg')) {
          const sideSvg = sideLink.querySelector('svg');
          if (sideSvg) {
            const clone = sideSvg.cloneNode(true);
            clone.classList.add('tb-pill-ico');
            pill.insertBefore(clone, pill.firstChild);
          }
        }
        const sideLabel = sideLink.querySelector('span');
        const pillLabel = pill.querySelector('span');
        if (sideLabel && pillLabel && sideLabel.textContent.trim()) {
          pillLabel.textContent = sideLabel.textContent.trim();
        }
      }
      const isActive = key === page;
      pill.classList.toggle('active', isActive);
      if (isActive) pill.setAttribute('aria-current', 'page');
      else pill.removeAttribute('aria-current');
    });

                                      
    $$('.bd-item[data-bd-nav]').forEach((bd) => {
      const key = bd.dataset.bdNav;
      const sideLink = $('#nav-' + key);
      if (sideLink) {
        bd.classList.toggle('hidden', sideLink.classList.contains('hidden'));
      }
      bd.classList.toggle('active', key === page);
    });

    updateTopbarScrollState();
    centerActiveTopbarPill(smoothScroll);
  }

  function initTopbarScroll() {
    const track = $('#tb-scroll-track');
    const topbar = $('.topbar');
    if (!track) return;

    track.addEventListener('scroll', updateTopbarScrollState, { passive: true });

    const handleWheel = (e) => {
      if (track.scrollWidth <= track.clientWidth) return;
      const delta = Math.abs(e.deltaY) >= Math.abs(e.deltaX) ? e.deltaY : e.deltaX;
      if (delta === 0) return;
      e.preventDefault();
      track.scrollLeft += delta;
      updateTopbarScrollState();
    };

    track.addEventListener('wheel', handleWheel, { passive: false });
    if (topbar) {
      topbar.addEventListener('wheel', (e) => {
        if (e.target.closest('#tb-scroll-shell')) return;
        if (track.scrollWidth <= track.clientWidth) return;
        handleWheel(e);
      }, { passive: false });
    }

    if (window.ResizeObserver) {
      new ResizeObserver(() => updateTopbarScrollState()).observe(track);
    }
  }

                                                                           
  function decoratePageHeaders() {
                                                  
    $$('.page-eyebrow').forEach((el) => el.remove());
  }

  function updateTelemetryRibbon() {
                                                               
  }

                                                                                 
  const CRUMBS = {
    home: 'Operations', orders: 'Operations', workload: 'Operations', checklists: 'Operations',
    quality: 'Quality', capa: 'Quality', qcbench: 'Quality', manuals: 'Quality',
    products: 'Catalog', si: 'Integrations',
    automations: 'Administration', users: 'Administration', devices: 'Administration', config: 'Administration',
  };

  function currentPage() {
    const h = (location.hash || '#/home').replace(/^#\/?/, '');
    return (h.split(/[/?]/)[0] || 'home');
  }

  function syncContext() {
    const page = currentPage();
    const pane = $('#view-' + page);
    const titleEl = $('#tb-context-title');
    const crumbEl = $('.tb-crumb');
    if (titleEl) {
      const h1 = pane && pane.querySelector('h1');
      const nav = $('.nav-link[data-nav="' + page + '"] span');
      titleEl.textContent = (h1 && h1.textContent.trim())
        || (nav && nav.textContent.trim())
        || document.title;
    }
    if (crumbEl) crumbEl.textContent = 'Cockpit · ' + (CRUMBS[page] || 'Overview');
    decoratePageHeaders();
    syncTopbarNav(false);
    updateTelemetryRibbon();
  }

  function scrollTopOnRoute() {
    const main = $('#main-content');
    if (main) main.scrollTop = 0;
  }

                                                                              
  function ensureRequiredIds() {
    const pairs = [
      ['#view-checklists table', 'checklists-table'],
      ['#view-products table', 'products-table'],
      ['#view-manuals table', 'manuals-table'],
      ['#view-users table', 'users-table'],
      ['#view-devices .rd-kpis', 'rd-kpis'],
      ['#view-devices .rd-list', 'rd-list'],
      ['#view-devices .rd-detail', 'rd-detail'],
      ['#view-devices .rd-search', 'rd-search'],
    ];
    for (const [sel, id] of pairs) {
      if (!document.getElementById(id)) {
        const el = $(sel);
        if (el) el.id = id;
      }
    }
  }

  function triggerQuickNewOrder() {
    if (window.OrderCreate && typeof window.OrderCreate.open === 'function') {
      window.OrderCreate.open();
      return;
    }
    const btn = $('#new-order-btn') || $('#home-new-order-btn');
    if (btn) btn.click();
  }

                                                                                 
  function init() {
    ensureRequiredIds();
    decoratePageHeaders();
    applyUIPrefs();
    initDrawer();
    initTopbarScroll();
    syncContext();

    document.addEventListener('click', (e) => {
      const t = e.target.closest('[data-ui]');
      if (!t) return;
      const action = t.dataset.ui;

      if (action === 'nav-toggle') { setDrawer(!body.classList.contains('nav-open')); return; }
      if (action === 'nav-close') { setDrawer(false); return; }
      if (action === 'nav-rail') {
        const next = !body.classList.contains('nav-rail');
        try { localStorage.setItem(RAIL_KEY, next ? '1' : '0'); } catch { }
        const prefs = loadUIPrefs();
        if (prefs.navLayout === 'rail' && !next) {
          prefs.navLayout = 'both';
          saveUIPrefs(prefs);
          body.setAttribute('data-nav-layout', 'both');
          syncPrefControls(prefs);
        }
        applyRail(next);
        return;
      }
      if (action === 'tb-scroll-left') { scrollTopbarBy(-240); return; }
      if (action === 'tb-scroll-right') { scrollTopbarBy(240); return; }
      if (action === 'accent' && t.dataset.accent) {
        updateSinglePref('accent', t.dataset.accent);
        return;
      }
      if (action === 'pref-reset') {
        resetAllPrefs();
        return;
      }
      if (action === 'shortcuts') {
        const real = $('#shortcuts-btn');
        if (real) real.click();
        return;
      }
      if (action === 'quick-new-order') {
        triggerQuickNewOrder();
        return;
      }
      if (action === 'open-palette') {
        const pal = $('#palette-btn');
        if (pal) pal.click();
        return;
      }
    });

    const bindSelect = (sel, key) => {
      const el = $(sel);
      if (el) el.addEventListener('change', (e) => updateSinglePref(key, e.target.value));
    };
    const bindCheck = (sel, key) => {
      const el = $(sel);
      if (el) el.addEventListener('change', (e) => updateSinglePref(key, !!e.target.checked));
    };

    bindSelect('#pref-surface', 'surface');
    bindSelect('#pref-nav-layout', 'navLayout');
    bindSelect('#pref-width', 'width');
    bindSelect('#pref-scale', 'scale');
    bindCheck('#pref-show-kpis', 'showKpis');
    bindCheck('#pref-show-charts', 'showCharts');
    bindCheck('#pref-show-scanner', 'showScanner');

    const settingsBtn = $('#settings-btn');
    if (settingsBtn) {
      settingsBtn.addEventListener('click', () => {
        requestAnimationFrame(() => syncPrefControls());
      });
    }

    document.addEventListener('click', (e) => {
      const link = e.target.closest('.side-scroll .nav-link, .side-brand');
      if (link && (isMobile() || body.classList.contains('nav-open'))) setDrawer(false);
    });

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && body.classList.contains('nav-open')) setDrawer(false);
    });

    window.addEventListener('hashchange', () => {
      syncContext();
      centerActiveTopbarPill(true);
      scrollTopOnRoute();
      if (body.classList.contains('nav-open')) setDrawer(false);
    });

    window.addEventListener('cockpit:sla-config-changed', updateTelemetryRibbon);

    const main = $('#main-content');
    const side = $('.sidebar');
    if (window.MutationObserver) {
      let t = null;
      const scheduleSync = () => {
        clearTimeout(t);
        t = setTimeout(() => {
          syncContext();
          updateTelemetryRibbon();
        }, 80);
      };
      if (main) {
        new MutationObserver(scheduleSync).observe(main, { childList: true, subtree: true, attributeFilter: ['class'] });
      }
      if (side) {
        new MutationObserver(scheduleSync).observe(side, { childList: true, subtree: true, attributeFilter: ['class'] });
      }
    }
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
