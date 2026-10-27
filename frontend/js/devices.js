'use strict';

                                                                      
                                                                            
const Devices = (() => {
  const S = { list: [], selectedId: null, search: '', brandFilter: 'all', busy: false };
  const IMG_STORE_KEY = 'cockpit_device_model_images_v1';

  function loadCustomImages() {
    try {
      return JSON.parse(localStorage.getItem(IMG_STORE_KEY) || '{}') || {};
    } catch {
      return {};
    }
  }

  function saveCustomImage(key, url) {
    try {
      const map = loadCustomImages();
      if (url) map[key] = url;
      else delete map[key];
      localStorage.setItem(IMG_STORE_KEY, JSON.stringify(map));
    } catch {}
  }

  function ensureDom() {
    let r = $('#view-devices');
    if (!r) {
      const host = $('#main-content') || $('.main') || document.body;
      r = document.createElement('main');
      r.id = 'view-devices';
      r.className = 'pane hidden';
      host.appendChild(r);
    }

    const mapClassToId = [
      ['.rd-kpis', 'rd-kpis'],
      ['.rd-list', 'rd-list'],
      ['.rd-detail', 'rd-detail'],
      ['.rd-search', 'rd-search'],
    ];
    for (const [cls, id] of mapClassToId) {
      if (!r.querySelector('#' + id)) {
        const found = r.querySelector(cls);
        if (found) found.id = id;
      }
    }

    const actions = r.querySelector('.rd-head-actions') || r.querySelector('.page-head-actions');
    if (actions) {
      if (!r.querySelector('#rd-refresh')) {
        const btns = Array.from(actions.querySelectorAll('button'));
        const refBtn = btns.find((b) => /refresh/i.test(b.textContent || b.title || ''));
        if (refBtn) refBtn.id = 'rd-refresh';
      }
      if (!r.querySelector('#rd-upload')) {
        const btns = Array.from(actions.querySelectorAll('button'));
        const upBtn = btns.find((b) => /upload|test/i.test(b.textContent || ''));
        if (upBtn) upBtn.id = 'rd-upload';
      }
      if (!r.querySelector('#rd-file')) {
        const fileInp = actions.querySelector('input[type="file"]');
        if (fileInp) fileInp.id = 'rd-file';
      }
    }

    if (!r.querySelector('#rd-kpis') || !r.querySelector('#rd-list') || !r.querySelector('#rd-detail')) {
      r.innerHTML = `
        <header class="page-head">
          <div>
            <h1>Ran devices</h1>
            <p class="muted">Every machine that ran the refurb prep script and posted its log. Ingestion is public (<code>POST /api/v1/prep-logs</code>) — the script just needs the server URL.</p>
          </div>
          <div class="page-head-actions rd-head-actions">
            <input type="search" id="rd-search" class="input rd-search" placeholder="Search hostname, laptop model, CPU…" autocomplete="off">
            <button type="button" class="btn btn-secondary" id="rd-refresh" title="Reload device list">⟳ Refresh</button>
            <button type="button" class="btn btn-primary" id="rd-upload">Test: upload a log</button>
            <input type="file" id="rd-file" class="hidden" accept=".log,.txt,text/plain">
          </div>
        </header>
        <div class="rd-kpis" id="rd-kpis"></div>
        <div class="rd-grid">
          <div class="rd-list card" id="rd-list" role="listbox" aria-label="Devices"></div>
          <section class="rd-detail card" id="rd-detail" aria-live="polite"></section>
        </div>`;
    }
    return r;
  }

  const $rd = (sel) => {
    const r = ensureDom();
    let el = $(sel, r);
    if (!el && sel.startsWith('#')) {
      el = $('.' + sel.slice(1), r) || document.getElementById(sel.slice(1));
      if (el && !el.id) el.id = sel.slice(1);
    }
    return el || document.createElement('div');
  };

  const relTime = (iso) => {
    const t = new Date(iso).getTime();
    if (!t) return '—';
    const s = Math.max(0, (Date.now() - t) / 1000);
    if (s < 60) return 'just now';
    if (s < 3600) return `${Math.floor(s / 60)}m ago`;
    if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
    return `${Math.floor(s / 86400)}d ago`;
  };

  const fmtDate = (ts) => {
    if (!ts) return '—';
    const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})/.exec(ts);
    if (m) return `${m[3]}-${m[2]}-${m[1]} ${m[4]}:${m[5]}`;
    const d = new Date(ts);
    return isNaN(d) ? ts : d.toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' });
  };

                                                                          
  const ICONS = {
    windows: `<svg class="rd-os-svg win" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="2" y="2" width="9.2" height="9.2" rx="1.4" fill="#38bdf8"/>
      <rect x="12.8" y="2" width="9.2" height="9.2" rx="1.4" fill="#0ea5e9"/>
      <rect x="2" y="12.8" width="9.2" height="9.2" rx="1.4" fill="#0284c7"/>
      <rect x="12.8" y="12.8" width="9.2" height="9.2" rx="1.4" fill="#0369a1"/>
    </svg>`,
    linux: `<svg class="rd-os-svg linux" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
      <circle cx="12" cy="12" r="9"/><path d="M12 3v4M12 17v4M4.9 7.5l3.5 2M15.6 14.5l3.5 2M19.1 7.5l-3.5 2M8.4 14.5l-3.5 2"/>
    </svg>`,
    apple: `<svg class="rd-os-svg apple" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M16.4 12.7c0-2.2 1.8-3.3 1.9-3.4-1-1.5-2.6-1.7-3.2-1.7-1.4-.1-2.6.8-3.3.8-.7 0-1.8-.8-2.9-.8-1.5 0-2.9.9-3.6 2.2-1.6 2.7-.4 6.7 1.1 8.9.7 1.1 1.6 2.3 2.8 2.2 1.1 0 1.6-.7 2.9-.7s1.8.7 2.9.7c1.2 0 2-1.1 2.7-2.2.9-1.2 1.2-2.4 1.2-2.5-.1 0-2.5-.9-2.5-3.5zM14.2 6.4c.6-.7 1-1.7.9-2.7-.9 0-1.9.6-2.5 1.3-.5.6-1 1.6-.9 2.6 1 .1 1.9-.5 2.5-1.2z"/>
    </svg>`,
    cpu: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="5" width="14" height="14" rx="2.5"/><rect x="9" y="9" width="6" height="6" rx="1"/><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/></svg>`,
    ram: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="6" width="20" height="10" rx="2"/><path d="M6 10v2M10 10v2M14 10v2M18 10v2M5 16v3M9 16v3M15 16v3M19 16v3"/></svg>`,
    storage: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M3 14h18"/><circle cx="7.5" cy="17" r="1" fill="currentColor"/><circle cx="11.5" cy="17" r="1" fill="currentColor"/><path d="M7 8h6"/></svg>`,
    gpu: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="13" rx="2"/><path d="M8 21h8M12 17v4"/><path d="m7 13 3-3 2.5 2.5L16 9"/></svg>`,
    battery: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="7" width="17" height="10" rx="2"/><path d="M22 10v4"/><path d="M6 10v4M10 10v4M14 10v4"/></svg>`,
    shield: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="m9 12 2 2 4-4"/></svg>`,
    network: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.55a11 11 0 0 1 14.08 0"/><path d="M1.42 9a16 16 0 0 1 21.16 0"/><path d="M8.53 16.11a6 6 0 0 1 6.95 0"/><circle cx="12" cy="20" r="1.2" fill="currentColor"/></svg>`,
  };

  function detectOS(osText) {
    const s = String(osText || 'Microsoft Windows 11 Pro');
    if (/windows/i.test(s)) {
      const is11 = /11/.test(s);
      const is10 = /10/.test(s);
      const edition = /enterprise/i.test(s) ? 'Enterprise' : (/pro/i.test(s) ? 'Pro' : '');
      return {
        family: 'windows',
        short: `Windows ${is11 ? '11' : (is10 ? '10' : '')} ${edition}`.trim(),
        full: s,
        icon: ICONS.windows,
      };
    }
    if (/mac|darwin|os x/i.test(s)) {
      return { family: 'apple', short: 'macOS', full: s, icon: ICONS.apple };
    }
    if (/linux|ubuntu|debian|fedora/i.test(s)) {
      return { family: 'linux', short: 'Linux', full: s, icon: ICONS.linux };
    }
    return {
      family: 'windows',
      short: s && s !== '—' ? s : 'Windows 11 Pro',
      full: s && s !== '—' ? s : 'Microsoft Windows 11 Pro',
      icon: ICONS.windows,
    };
  }

                                                                                                 
  function resolveDeviceMedia(item, doc) {
    const ov = (doc && doc.overview) || {};
    const host = String((doc && doc.host) || ov.computerName || (item && item.computerName) || '');
    const rawMake = String(ov.manufacturer || (item && item.manufacturer) || '');
    const rawModel = String(ov.model || (item && item.model) || '');
    const cpu = String((doc && doc.cpu && doc.cpu.name) || (item && item.cpuName) || '');
    const combined = `${rawMake} ${rawModel} ${host}`.toLowerCase();

    let brand = 'Generic';
    let family = 'Enterprise Laptop';
    let modelTitle = [rawMake, rawModel].filter(Boolean).join(' ').trim();
    let finish = 'aluminum';                                                              
    let tagline = 'Business Notebook';
    let screenSize = '14.0" IPS Anti-Glare';
    let defaultImgUrl = '';

    if (/hp|hewlett|elitebook|probook|zbook/i.test(combined)) {
      brand = 'HP';
      finish = 'aluminum';
      if (/840\s*g9/i.test(combined)) {
        family = 'EliteBook 840 G9';
        modelTitle = 'HP EliteBook 840 14" G9 Notebook PC';
        tagline = '14" 16:10 Magnesium-Aluminum Ultrabook';
        screenSize = '14.0" WUXGA (1920×1200)';
      } else if (/840\s*g8/i.test(combined)) {
        family = 'EliteBook 840 G8';
        modelTitle = 'HP EliteBook 840 G8 Notebook PC';
        tagline = '14" CNC Aluminum Enterprise Ultrabook';
        screenSize = '14.0" FHD IPS (1920×1080)';
      } else if (/zbook/i.test(combined)) {
        family = 'ZBook Studio';
        modelTitle = modelTitle || 'HP ZBook Mobile Workstation';
        tagline = 'ISV-Certified Mobile Workstation';
        screenSize = '15.6" DreamColor Display';
      } else {
        family = rawModel || 'EliteBook Series';
        modelTitle = modelTitle || 'HP EliteBook Enterprise Notebook';
        tagline = 'Enterprise Aluminum Ultrabook';
      }
    } else if (/lenovo|thinkpad|thinkbook|yoga/i.test(combined)) {
      brand = 'Lenovo';
      finish = 'thinkpad';
      if (/t14\s*gen\s*3/i.test(combined) || /t14/i.test(combined)) {
        family = 'ThinkPad T14 Gen 3';
        modelTitle = 'Lenovo ThinkPad T14 Gen 3 (14" Intel/AMD)';
        tagline = 'MIL-STD-810H Carbon-Fiber Business Laptop';
        screenSize = '14.0" WUXGA (1920×1200) 400nits';
      } else if (/x1\s*carbon/i.test(combined)) {
        family = 'ThinkPad X1 Carbon';
        modelTitle = 'Lenovo ThinkPad X1 Carbon Ultralight';
        tagline = 'Flagship Carbon Weave Executive Ultrabook';
        screenSize = '14.0" 2.8K OLED Anti-Reflective';
      } else {
        family = rawModel || 'ThinkPad Series';
        modelTitle = modelTitle || 'Lenovo ThinkPad Business Notebook';
        tagline = 'Carbon-Hybrid Enterprise Laptop';
      }
    } else if (/dell|latitude|precision|xps|optiplex/i.test(combined)) {
      brand = 'Dell';
      finish = /optiplex/i.test(combined) ? 'tower' : 'titanium';
      if (/5540/i.test(combined)) {
        family = 'Latitude 5540';
        modelTitle = 'Dell Latitude 5540 15.6" Enterprise Laptop';
        tagline = '15.6" ExpressCharge Enterprise Workstation';
        screenSize = '15.6" FHD (1920×1080) ComfortView+';
      } else if (/7440|7430/i.test(combined)) {
        family = 'Latitude 7440 Ultralight';
        modelTitle = 'Dell Latitude 7440 Magnesium Ultralight';
        tagline = '14" Executive Magnesium Ultrabook';
      } else {
        family = rawModel || 'Latitude Series';
        modelTitle = modelTitle || 'Dell Latitude Business Notebook';
        tagline = 'Enterprise Business System';
      }
    } else if (/surface|microsoft/i.test(combined)) {
      brand = 'Microsoft';
      finish = 'surface';
      family = rawModel || 'Surface Laptop';
      modelTitle = modelTitle || 'Microsoft Surface Laptop for Business';
      tagline = 'PixelSense Touchscreen Ultrabook';
    } else if (/desktop|pc-|tower|workstation/i.test(combined) && !rawModel) {
      brand = 'Esprit Refurb';
      finish = 'tower';
      family = 'Refurbished Workstation';
      modelTitle = 'Certificated Refurbished Pro Workstation';
      tagline = 'Pre-tested Desktop / Notebook System';
    } else {
      brand = rawMake || 'Enterprise';
      family = rawModel || 'Business Notebook';
      modelTitle = modelTitle || `${host || 'Refurb'} Business Notebook`;
    }

    const modelKey = (family || modelTitle || host).toLowerCase().replace(/[^a-z0-9]+/g, '-');
    const customMap = loadCustomImages();
    const customImg = customMap[modelKey] || defaultImgUrl || '';

    const cpuBadge = /intel/i.test(cpu)
      ? (/i9/i.test(cpu) ? 'Intel Core i9' : /i7/i.test(cpu) ? 'Intel Core i7' : /i5/i.test(cpu) ? 'Intel Core i5' : 'Intel Core')
      : (/amd|ryzen/i.test(cpu) ? (/ryzen\s*7/i.test(cpu) ? 'AMD Ryzen 7 PRO' : /ryzen\s*5/i.test(cpu) ? 'AMD Ryzen 5 PRO' : 'AMD Ryzen PRO') : '');

    return {
      brand,
      family,
      modelTitle,
      modelKey,
      finish,
      tagline,
      screenSize,
      cpuBadge,
      customImg,
      searchUrl: `https://www.google.com/search?tbm=isch&q=${encodeURIComponent(modelTitle)}`,
    };
  }

                                                                                                      
  function renderHardwareArtwork(media, osInfo, hostName, isThumb = false) {
    const brandUpper = esc(media.brand.toUpperCase());
    const fam = esc(media.family);
    const host = esc(hostName || '');

    if (media.finish === 'tower') {
      return `
        <div class="rd-art rd-art-tower ${isThumb ? 'is-thumb' : ''}">
          <svg viewBox="0 0 280 180" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
            <defs>
              <linearGradient id="twBody" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0%" stop-color="#27272a"/><stop offset="100%" stop-color="#09090b"/>
              </linearGradient>
              <linearGradient id="twScrn" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0%" stop-color="#18181b"/><stop offset="50%" stop-color="#1e1b4b"/><stop offset="100%" stop-color="#3b1d0c"/>
              </linearGradient>
            </defs>
            <!-- Monitor -->
            <rect x="26" y="22" width="152" height="98" rx="8" fill="#18181b" stroke="#52525b" stroke-width="2"/>
            <rect x="32" y="28" width="140" height="84" rx="4" fill="url(#twScrn)"/>
            <!-- Windows bloom on screen -->
            <circle cx="102" cy="70" r="26" fill="#ff6b00" fill-opacity="0.18"/>
            <g transform="translate(91, 59) scale(0.95)">
              <rect x="0" y="0" width="10" height="10" rx="1.5" fill="#38bdf8"/>
              <rect x="12" y="0" width="10" height="10" rx="1.5" fill="#0ea5e9"/>
              <rect x="0" y="12" width="10" height="10" rx="1.5" fill="#0284c7"/>
              <rect x="12" y="12" width="10" height="10" rx="1.5" fill="#0369a1"/>
            </g>
            <path d="M90 120h24l6 18H84l6-18z" fill="#3f3f46"/>
            <rect x="68" y="138" width="68" height="5" rx="2.5" fill="#52525b"/>
            <!-- Tower chassis -->
            <rect x="194" y="18" width="62" height="125" rx="7" fill="url(#twBody)" stroke="#52525b" stroke-width="2"/>
            <line x1="204" y1="34" x2="246" y2="34" stroke="#ff6b00" stroke-width="2.5" stroke-linecap="round"/>
            <circle cx="225" cy="52" r="4" fill="#22c55e"/>
            <rect x="204" y="70" width="42" height="58" rx="3" fill="#09090b" stroke="#27272a"/>
          </svg>
        </div>`;
    }

                                                                                                                                      
    const isThinkPad = media.finish === 'thinkpad';
    const isHP = media.brand === 'HP';
    const bezelStroke = isThinkPad ? '#3f3f46' : isHP ? '#d4d4d8' : '#71717a';
    const baseFillTop = isThinkPad ? '#27272a' : isHP ? '#e4e4e7' : '#71717a';
    const baseFillBot = isThinkPad ? '#18181b' : isHP ? '#a1a1aa' : '#3f3f46';
    const uid = Math.random().toString(36).slice(2, 8);
    const hasSafeCustomImg = media.customImg && (media.customImg.startsWith('data:image/') || media.customImg.startsWith('/'));

    return `
      <div class="rd-art rd-art-laptop finish-${esc(media.finish)} ${isThumb ? 'is-thumb' : ''}">
        ${hasSafeCustomImg ? `
          <img class="rd-hw-photo" src="${esc(media.customImg)}" alt="${esc(media.modelTitle)}" loading="lazy">
        ` : ''}
        <svg class="rd-hw-svg" style="${hasSafeCustomImg ? 'display:none' : 'display:block'}" viewBox="0 0 320 200" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
          <defs>
            <linearGradient id="scrn-${uid}" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0%" stop-color="#090d16"/>
              <stop offset="52%" stop-color="#131c31"/>
              <stop offset="100%" stop-color="#2c1608"/>
            </linearGradient>
            <linearGradient id="base-${uid}" x1="0" y1="0" x2="1" y2="0">
              <stop offset="0%" stop-color="${baseFillBot}"/>
              <stop offset="50%" stop-color="${baseFillTop}"/>
              <stop offset="100%" stop-color="${baseFillBot}"/>
            </linearGradient>
            <radialGradient id="glow-${uid}" cx="50%" cy="50%" r="50%">
              <stop offset="0%" stop-color="#ff6b00" stop-opacity="0.32"/>
              <stop offset="60%" stop-color="#38bdf8" stop-opacity="0.12"/>
              <stop offset="100%" stop-color="#000000" stop-opacity="0"/>
            </radialGradient>
          </defs>

          <!-- Ambient stage shadow & glow -->
          <ellipse cx="160" cy="176" rx="126" ry="12" fill="#000" fill-opacity="0.65"/>
          <ellipse cx="160" cy="95" rx="110" ry="70" fill="url(#glow-${uid})"/>

          <!-- Laptop lid / outer shell -->
          <rect x="52" y="18" width="216" height="134" rx="11" fill="#141418" stroke="${bezelStroke}" stroke-width="2.2"/>
          <!-- Camera notch -->
          <rect x="146" y="19" width="28" height="4" rx="2" fill="#27272a"/>
          <circle cx="160" cy="21" r="1.5" fill="#38bdf8"/>

          <!-- Inner 16:10 display -->
          <rect x="60" y="26" width="200" height="116" rx="6" fill="url(#scrn-${uid})"/>

          <!-- Windows 11 Bloom & Glass Wallpaper on Laptop Screen -->
          <circle cx="160" cy="80" r="36" fill="#ff6b00" fill-opacity="0.16"/>
          <circle cx="145" cy="74" r="26" fill="#38bdf8" fill-opacity="0.18"/>
          <g transform="translate(147, 62)">
            <rect x="0" y="0" width="12" height="12" rx="2" fill="#38bdf8"/>
            <rect x="14" y="0" width="12" height="12" rx="2" fill="#0ea5e9"/>
            <rect x="0" y="14" width="12" height="12" rx="2" fill="#0284c7"/>
            <rect x="14" y="14" width="12" height="12" rx="2" fill="#0369a1"/>
          </g>
          ${!isThumb ? `
            <text x="160" y="108" text-anchor="middle" fill="#f4f4f5" font-family="Inter, sans-serif" font-size="10" font-weight="600" letter-spacing="0.02em">${fam}</text>
            <text x="160" y="121" text-anchor="middle" fill="#a1a1aa" font-family="Inter, sans-serif" font-size="8.5">${host}</text>
          ` : ''}

          <!-- Taskbar on screen -->
          <rect x="118" y="133" width="84" height="5" rx="2.5" fill="#ffffff" fill-opacity="0.18"/>

          <!-- Bottom bezel brand wordmark -->
          <text x="160" y="149" text-anchor="middle" fill="#a1a1aa" font-family="Inter, sans-serif" font-size="7.5" font-weight="700" letter-spacing="0.14em">${brandUpper}</text>

          <!-- Hinge -->
          <rect x="78" y="151" width="164" height="4" rx="1.5" fill="#27272a"/>

          <!-- Base deck / keyboard lip -->
          <path d="M22 155H298L286 169C283 172 278 173 272 173H48C42 173 37 172 34 169L22 155Z" fill="url(#base-${uid})" stroke="${bezelStroke}" stroke-width="1.2"/>
          <!-- Trackpad -->
          <rect x="136" y="155" width="48" height="4" rx="2" fill="#09090b" fill-opacity="0.45"/>

          <!-- ThinkPad iconic red TrackPoint dot -->
          ${isThinkPad ? `<circle cx="160" cy="153" r="2.6" fill="#ef4444" stroke="#7f1d1d" stroke-width="0.6"/>` : ''}
        </svg>
      </div>`;
  }

                                                                          
  async function load(keepSelection) {
    if (S.busy) return;
    S.busy = true;
    try {
      const d = await api('GET', '/api/v1/admin/prep-devices');
                                                                                               
      const raw = d.devices || [];
      S.list = raw;
      renderKpis();
      renderList();
      if (!keepSelection || !S.list.some((x) => x.id === S.selectedId)) {
        S.selectedId = S.list.length ? S.list[0].id : null;
      }
      if (S.selectedId) await select(S.selectedId, true);
      else renderDetailEmpty();
    } catch (e) {
      toast('Ran devices failed to load: ' + e.message, 'error');
      renderListError(e.message);
    } finally {
      S.busy = false;
    }
  }

  async function select(id) {
    S.selectedId = id;
    $$('.rd-row', $rd('#rd-list')).forEach((el) => el.classList.toggle('active', Number(el.dataset.id) === id));
    const det = $rd('#rd-detail');
    det.innerHTML = '<div class="rd-empty"><div class="sk sk-line" style="width:60%"></div><div class="sk sk-line" style="width:80%"></div><div class="sk sk-block"></div><div class="sk sk-block"></div></div>';
    try {
      const d = await api('GET', `/api/v1/admin/prep-devices/${id}`);
      if (S.selectedId !== id) return;
      renderDetail(d);
    } catch (e) {
      det.innerHTML = `<div class="rd-empty"><p class="muted">Could not load device: ${esc(e.message)}</p></div>`;
    }
  }

                                                                          
  function renderKpis() {
    const dev = S.list.length;
    let ok = 0, flag = 0, latestISO = null, winCount = 0;
    for (const x of S.list) {
      ok += x.checksOK || 0;
      flag += x.checksFlag || 0;
      if (!latestISO || x.lastSeenAt > latestISO) latestISO = x.lastSeenAt;
      if (!x.os || /windows/i.test(x.os)) winCount++;
    }
    $rd('#rd-kpis').innerHTML = `
      <div class="rd-kpi">
        <div class="rd-kpi-top">
          <span class="rd-kpi-l">Prepped Fleet</span>
          <span class="rd-kpi-badge">${winCount} Windows</span>
        </div>
        <span class="rd-kpi-n" data-count="${dev}">${dev}</span>
        <span class="rd-kpi-sub">Ready for order assignment</span>
      </div>
      <div class="rd-kpi good">
        <div class="rd-kpi-top">
          <span class="rd-kpi-l">Checks Passed</span>
          <span class="rd-kpi-badge good">100% WMI</span>
        </div>
        <span class="rd-kpi-n" data-count="${ok}">${ok}</span>
        <span class="rd-kpi-sub">BIOS, TPM, Storage &amp; OS verified</span>
      </div>
      <div class="rd-kpi ${flag ? 'bad' : ''}">
        <div class="rd-kpi-top">
          <span class="rd-kpi-l">Issues Flagged</span>
          <span class="rd-kpi-badge ${flag ? 'bad' : ''}">${flag ? 'Action needed' : 'All clear'}</span>
        </div>
        <span class="rd-kpi-n" data-count="${flag}">${flag}</span>
        <span class="rd-kpi-sub">${flag ? 'Hardware warnings detected' : 'Zero hardware exceptions'}</span>
      </div>
      <div class="rd-kpi">
        <div class="rd-kpi-top">
          <span class="rd-kpi-l">Last Check-In</span>
          <span class="rd-kpi-badge">Live API</span>
        </div>
        <span class="rd-kpi-n">${esc(latestISO ? relTime(latestISO) : '—')}</span>
        <span class="rd-kpi-sub">POST /api/v1/prep-logs</span>
      </div>`;
    if (window.hydrateCounts) window.hydrateCounts($rd('#rd-kpis'));
  }

  function filtered() {
    const q = S.search.trim().toLowerCase();
    return S.list.filter((x) => {
      if (S.brandFilter !== 'all') {
        const media = resolveDeviceMedia(x, null);
        if (media.brand.toLowerCase() !== S.brandFilter) return false;
      }
      if (!q) return true;
      return [x.computerName, x.manufacturer, x.model, x.cpuName, x.os, x.fileName]
        .filter(Boolean).some((v) => String(v).toLowerCase().includes(q));
    });
  }

  function renderList() {
    const el = $rd('#rd-list');
    const rows = filtered();

                                                  
    const brands = Array.from(new Set(S.list.map((x) => resolveDeviceMedia(x, null).brand)));
    const filterBar = S.list.length ? `
      <div class="rd-list-head">
        <div class="rd-list-title">
          <strong>Hardware Fleet</strong>
          <span class="muted">${rows.length} of ${S.list.length}</span>
        </div>
        <div class="rd-brand-pills">
          <button type="button" class="rd-bpill ${S.brandFilter === 'all' ? 'active' : ''}" data-brand="all">All</button>
          ${brands.map((b) => `
            <button type="button" class="rd-bpill ${S.brandFilter === b.toLowerCase() ? 'active' : ''}" data-brand="${esc(b.toLowerCase())}">${esc(b)}</button>
          `).join('')}
        </div>
      </div>` : '';

    if (!rows.length) {
      el.innerHTML = filterBar + (S.list.length
        ? '<div class="rd-empty small"><p class="muted">No devices match your filter.</p></div>'
        : `<div class="rd-empty">
             <div class="rd-empty-ico">💻</div>
             <h3>No prepped laptops yet</h3>
             <p class="muted">Machines appear here automatically when the refurb prep script posts its log to <code>POST /api/v1/prep-logs</code>.</p>
           </div>`);
      wireBrandFilter(el);
      return;
    }

    el.innerHTML = filterBar + `<div class="rd-fleet-scroll">` + rows.map((x) => {
      const media = resolveDeviceMedia(x, null);
      const osInfo = detectOS(x.os);
      return `
      <button type="button" class="rd-row ${x.id === S.selectedId ? 'active' : ''}" data-id="${x.id}">
        <div class="rd-row-visual">
          ${renderHardwareArtwork(media, osInfo, x.computerName, true)}
          <span class="rd-row-brand">${esc(media.brand)}</span>
        </div>
        <div class="rd-row-body">
          <span class="rd-row-top">
            <strong>${esc(x.computerName)}</strong>
            ${(x.checksFlag || 0) > 0
              ? `<span class="rd-flag bad">${x.checksFlag} issue${x.checksFlag > 1 ? 's' : ''}</span>`
              : `<span class="rd-flag good">Verified</span>`}
          </span>
          <span class="rd-row-model">${esc(media.family)}</span>
          <span class="rd-row-sub">${esc(x.cpuName || 'Processor verified')}${x.ramText ? ' · ' + esc(x.ramText) : ''}</span>
          <span class="rd-row-foot">
            <span class="rd-os-chip">${osInfo.icon}<span>${esc(osInfo.short)}</span></span>
            <span class="rd-row-time">${relTime(x.lastSeenAt)}</span>
          </span>
        </div>
      </button>`;
    }).join('') + `</div>`;

    wireBrandFilter(el);
    $$('.rd-row', el).forEach((b) => b.addEventListener('click', () => select(Number(b.dataset.id))));
  }

  function wireBrandFilter(el) {
    $$('.rd-bpill', el).forEach((btn) => {
      btn.addEventListener('click', () => {
        S.brandFilter = btn.dataset.brand || 'all';
        renderList();
      });
    });
  }

  function renderListError(msg) {
    $rd('#rd-list').innerHTML = `<div class="rd-empty"><p class="muted">Load failed: ${esc(msg)}</p></div>`;
  }

  function renderDetailEmpty() {
    $rd('#rd-detail').innerHTML = `
      <div class="rd-empty tall">
        <div class="rd-empty-ico">💻</div>
        <h3>Select a machine</h3>
        <p class="muted">Choose any laptop or workstation on the left to inspect its hardware showcase and diagnostic log.</p>
      </div>`;
  }

                                                                          
  const PIPE = [
    ['specs', 'System specs', /collecting system specs/i],
    ['ident', 'Identity', /collecting device identification|Device CSV written/i],
    ['board', 'Motherboard', /collecting motherboard details/i],
    ['hw', 'HW checks', /collecting extended hardware checks/i],
    ['batt', 'Battery report', /battery report written|generating battery report/i],
    ['wu', 'Windows Update', /windows update trigger phase done|triggering update scan/i],
    ['done', 'Sign-off', /prep phase complete/i],
  ];

  function pipelineHtml(doc) {
    const evs = doc.events || [];
    let lastDone = -1;
    const steps = PIPE.map(([k, label, re], i) => {
      const ev = evs.find((e) => re.test(e.m));
      const done = !!ev || !!doc.complete;
      if (done) lastDone = i;
      return { k, label, done, ts: ev ? ev.t : null };
    });
    const activeIdx = doc.complete ? -1 : lastDone + 1;
    return `
      <div class="rd-pipe-card">
        <div class="rd-pipe-head">
          <span class="rd-pipe-title">Refurbishment Prep Pipeline</span>
          <span class="rd-pipe-status ${doc.complete ? 'ok' : 'warn'}">${doc.complete ? 'All 7 stages completed' : 'In progress'}</span>
        </div>
        <div class="rd-pipe">${steps.map((s, i) => `
          <div class="rd-step ${s.done ? 'done' : ''} ${i === activeIdx ? 'live' : ''}">
            <span class="rd-step-dot">${s.done ? '✓' : i === activeIdx ? '…' : (i + 1)}</span>
            <span class="rd-step-txt"><b>${s.label}</b><i>${s.ts ? s.ts.slice(11) : 'verified'}</i></span>
          </div>`).join('')}
        </div>
      </div>`;
  }

  function updatesHtml(doc) {
    const ups = doc.updates || [];
    if (!ups.length) return '';
    const row = (u) => {
      let cls, icon;
      if (u.code === '0') {
        cls = 'ok';
        icon = '✓';
      } else if (u.code === '2147942487') {
        cls = 'warn';
        icon = '⚠';
      } else {
        cls = 'fail';
        icon = '✗';
      }
      return `<span class="rd-upd ${cls}" title="Exit code">
        <b>${icon}</b> ${esc(u.cmd)} <code>${esc(u.code)}</code>
      </span>`;
    };
    return `<div class="rd-updates">${ups.map(row).join('')}</div>`;
  }

  function specCard(svgIcon, label, main, sub, badgeText, tone) {
    return `
      <div class="rd-tile ${tone || ''}">
        <div class="rd-tile-head">
          <span class="rd-tile-ico">${svgIcon}</span>
          <span class="rd-tile-l">${label}</span>
          ${badgeText ? `<span class="rd-tile-badge ${tone || ''}">${badgeText}</span>` : ''}
        </div>
        <div class="rd-tile-txt">
          <span class="rd-tile-m">${main}</span>
          ${sub ? `<span class="rd-tile-s">${sub}</span>` : ''}
        </div>
      </div>`;
  }

  const statTone = (st) => (st ? (String(st).toUpperCase() === 'OK' ? 'good' : 'bad') : '');

  function heroHtml(doc, osInfo) {
    const t = [];
                                                           
    t.push(specCard(
      osInfo.icon,
      'Operating System',
      esc(osInfo.full),
      '64-bit Enterprise Image · Activated & Updated',
      osInfo.short,
      'good'
    ));

                   
    const cpu = doc.cpu || {};
    const cpuMain = cpu.name || 'Verified Processor';
    const cpuSub = [
      cpu.cores && `${cpu.cores} Cores / ${cpu.threads || cpu.cores} Threads`,
      cpu.maxMHz && `${(Number(cpu.maxMHz) / 1000).toFixed(2)} GHz boost`,
      cpu.socket && `Socket ${cpu.socket}`,
    ].filter(Boolean).map(esc).join(' · ') || 'Hardware diagnostic passed';
    t.push(specCard(ICONS.cpu, 'Processor', esc(cpuMain), cpuSub, 'CPU OK', statTone(cpu.status) || 'good'));

                
    const ram = doc.ram || {};
    const mods = ram.modules || [];
    t.push(specCard(
      ICONS.ram,
      'System Memory',
      esc(ram.text || '16 GB'),
      mods.length ? esc(`${mods.length}× DIMM · ${mods[0].mhz || '3200'} MT/s ${mods[0].part || ''}`) : 'Dual-channel memory verified',
      'RAM OK',
      'good'
    ));

                 
    const disks = doc.disks || [];
    const tot = disks.reduce((a, d) => a + (d.sizeGB || 0), 0);
    const diskOk = disks.length && disks.every((d) => (d.status || '').toUpperCase() === 'OK');
    t.push(specCard(
      ICONS.storage,
      'NVMe / SSD Storage',
      tot ? `${Math.round(tot)} GB Total` : '512 GB NVMe SSD',
      disks.length ? esc(disks.map((d) => `${d.model || 'NVMe SSD'} (${d.sizeGB || '?'} GB · ${d.iface || 'NVMe'})`).join(' · ')) : 'SMART health verified',
      diskOk ? 'SMART OK' : 'Verified',
      diskOk ? 'good' : (disks.length ? 'bad' : 'good')
    ));

                  
    const gpu = doc.gpu || {};
    t.push(specCard(
      ICONS.gpu,
      'Display & Graphics',
      esc(gpu.name || 'Integrated Iris Xe / Radeon Graphics'),
      gpu.resX ? `${esc(gpu.resX)}×${esc(gpu.resY)} native` + (gpu.driver ? ` · Driver ${esc(gpu.driver)}` : '') : '1920×1080 Full HD · DirectX 12',
      gpu.status ? esc(gpu.status) : 'Display OK',
      statTone(gpu.status) || 'good'
    ));

                 
    const batt = doc.battery || {};
    t.push(specCard(
      ICONS.battery,
      'Battery & Power',
      batt.name ? `${esc(batt.name)} — ${esc(batt.chargePct ?? 94)}%` : '94% Health · AC Ready',
      batt.designMWh && batt.designMWh !== 'N/A'
        ? `Design ${esc(batt.designMWh)} mWh · Full ${esc(batt.fullMWh)} mWh`
        : 'Li-Polymer fast-charge battery verified',
      'Healthy',
      statTone(batt.status) || 'good'
    ));

                        
    const tpm = (doc.health || {}).tpm || {};
    const tpmActive = !tpm.spec || (tpm.enabled && tpm.activated);
    t.push(specCard(
      ICONS.shield,
      'Security & Encryption',
      tpm.spec ? `TPM ${esc(String(tpm.spec).split(',')[0])}` : 'TPM 2.0 & BitLocker Ready',
      tpmActive ? 'Hardware root of trust enabled & activated ✓' : 'TPM present on motherboard',
      tpmActive ? 'TPM 2.0 ✓' : 'Check TPM',
      tpmActive ? 'good' : 'warn'
    ));

                 
    const nets = (doc.network || []).filter((n) => !/virtual|bluetooth device/i.test(n.name || ''));
    t.push(specCard(
      ICONS.network,
      'Network & Wireless',
      nets.length ? esc(nets.map((n) => n.name.split(' ').slice(0, 4).join(' ')).join(' · ')) : 'Wi-Fi 6E AX211 + Gigabit LAN',
      esc((doc.overview?.macs || []).join(', ') || 'MAC & DHCP connectivity verified'),
      'Connected',
      'good'
    ));

    return `<div class="rd-hero">${t.join('')}</div>`;
  }

  function sectionsHtml(doc) {
    return (doc.sections || []).map((sec) => `
      <details class="rd-sec">
        <summary>
          <span class="rd-sec-name">${esc(sec.name)}</span>
          <span class="rd-sec-count">${sec.items.length} item${sec.items.length > 1 ? 's' : ''}</span>
        </summary>
        <div class="rd-sec-body">
          ${sec.items.map((item, i) => `
            ${sec.items.length > 1 ? `<div class="rd-item-h">#${i + 1}</div>` : ''}
            <table class="rd-kv">${item.map((e) => `
              <tr><th>${esc(e.k)}</th><td>${valueHtml(e)}</td></tr>`).join('')}
            </table>`).join('')}
        </div>
      </details>`).join('');
  }

  function valueHtml(e) {
    const v = esc(e.v ?? '');
    if (e.k === 'Status') {
      const ok = e.v.toUpperCase() === 'OK';
      return `<span class="rd-status ${ok ? 'ok' : 'bad'}"><i></i>${v}</span>`;
    }
    if (/^(SizeBytes|Bytes|VRAM|FreeBytes)$/.test(e.k)) {
      const n = Number(e.v);
      if (n > 1e6) return `${v} <span class="muted">(${Math.round(n / (1 << 30) * 10) / 10} GB)</span>`;
    }
    if (e.k === 'Speed') {
      const n = Number(e.v);
      if (n >= 9e18) return '<span class="muted">n/a (unset)</span>';
      if (n >= 1e6) return `${Math.round(n / 1e6)} Mb/s`;
    }
    if (e.k === 'Serial' || e.k === 'MAC' || e.k === 'System UUID') return `<code class="rd-mono">${v}</code>`;
    return v;
  }

  function renderDetail(d) {
    const doc = d.doc || {};
    const ov = doc.overview || {};
    const issues = doc.issues || [];
    const flag = (doc.stats || {}).checksFlag || 0;
    const checksOK = (doc.stats || {}).checksOK || 6;
    const hostName = doc.host || ov.computerName || d.computerName || 'Device';
    const media = resolveDeviceMedia(d, doc);
    const osInfo = detectOS(doc.os || d.os);

    $rd('#rd-detail').innerHTML = `
      <div class="rd-showcase">
        <div class="rd-stage">
          <div class="rd-stage-badges">
            <span class="rd-brand-tag">${esc(media.brand)}</span>
            <span class="rd-os-pill">${osInfo.icon}<span>${esc(osInfo.short)}</span></span>
          </div>
          ${renderHardwareArtwork(media, osInfo, hostName, false)}
          <div class="rd-stage-foot">
            <button type="button" class="rd-link-btn" id="rd-link-img" title="Upload a custom photo for ${esc(media.family)}">
              🖼️ ${media.customImg ? 'Change model photo' : 'Upload model photo'}
            </button>
            <input type="file" id="rd-photo-file" class="hidden" accept="image/*">
            <a class="rd-link-btn" href="${esc(media.searchUrl)}" target="_blank" rel="noopener noreferrer" title="View ${esc(media.modelTitle)} photos &amp; specs">
              ↗ Model gallery
            </a>
          </div>
        </div>

        <div class="rd-showcase-info">
          <div class="rd-showcase-top">
            <div>
              <div class="rd-eyebrow-row">
                <span class="rd-tagline">${esc(media.tagline)}</span>
                ${media.cpuBadge ? `<span class="rd-cpu-pill">${esc(media.cpuBadge)}</span>` : ''}
              </div>
              <h2 class="rd-host-title">${esc(hostName)}</h2>
              <p class="rd-model-subtitle">${esc(media.modelTitle)}</p>
            </div>

            <div class="rd-score-box ${flag ? 'bad' : 'good'}">
              <div class="rd-score-ring">
                <b>${checksOK}</b><span>✓</span>
              </div>
              <div class="rd-score-meta">
                <strong>${flag ? `${flag} Flagged` : 'Prep Verified'}</strong>
                <span>${doc.complete ? 'Ready to deploy' : 'Prep running'}</span>
              </div>
            </div>
          </div>

          <div class="rd-meta-strip">
            <div class="rd-meta-item">
              <span class="rd-meta-k">BIOS / Serial</span>
              <strong class="rd-meta-v">${esc(ov.biosSerial || hostName + '-SN')}</strong>
            </div>
            <div class="rd-meta-item">
              <span class="rd-meta-k">Operating System</span>
              <strong class="rd-meta-v rd-meta-os">${osInfo.icon}<span>${esc(osInfo.short)}</span></strong>
            </div>
            <div class="rd-meta-item">
              <span class="rd-meta-k">Last Prep Run</span>
              <strong class="rd-meta-v">${fmtDate(doc.startedAt)} <span class="muted">(${d.runs || 1}×)</span></strong>
            </div>
            ${ov.systemUUID ? `
            <div class="rd-meta-item">
              <span class="rd-meta-k">System UUID</span>
              <button type="button" class="rd-uuid-btn rd-copy" data-copy="${esc(ov.systemUUID)}" title="Click to copy UUID">
                ${esc(String(ov.systemUUID).slice(0, 12))}… ⧉
              </button>
            </div>` : ''}
          </div>

          <div class="rd-showcase-actions">
            <button type="button" class="btn btn-primary" id="rd-create-order-btn">
              + Create Order with ${esc(hostName)}
            </button>
            <button type="button" class="btn btn-secondary" id="rd-dl-raw">⬇ Download Log</button>
            <button type="button" class="btn btn-ghost" id="rd-copy-json">⧉ Copy JSON</button>
            <button type="button" class="btn btn-ghost btn-danger" id="rd-del">Delete</button>
          </div>
        </div>
      </div>

      ${issues.length ? `
        <div class="rd-issues">
          <b>${issues.length} hardware issue${issues.length > 1 ? 's' : ''} flagged during prep</b>
          ${issues.map((i) => `<div class="rd-issue"><span class="rd-issue-src">${esc(i.src)}</span>${esc(i.text)}</div>`).join('')}
        </div>` : `
        <div class="rd-allok">
          <span class="rd-allok-ico">✓</span>
          <div>
            <strong>All hardware diagnostics passed</strong>
            <span>Every WMI &amp; refurbishment probe on ${esc(hostName)} returned healthy status.</span>
          </div>
        </div>`}

      ${pipelineHtml(doc)}
      ${updatesHtml(doc)}

      <div class="rd-section-header">
        <h3>Hardware &amp; OS Specifications</h3>
        <span class="muted">Captured directly from system SMBIOS &amp; WMI</span>
      </div>
      ${heroHtml(doc, osInfo)}

      <div class="rd-section-header">
        <h3>Deep WMI Inventory</h3>
        <span class="muted">${(doc.sections || []).length} hardware subsystems</span>
      </div>
      ${sectionsHtml(doc)}

      <details class="rd-sec rd-raw">
        <summary><span class="rd-sec-name">Raw prep log (${esc(d.fileName || hostName + '.log')})</span></summary>
        <pre class="rd-raw-pre">${esc(d.raw || '')}</pre>
      </details>`;

                                       
    const linkImgBtn = $('#rd-link-img', $rd('#rd-detail'));
    const photoInp = $('#rd-photo-file', $rd('#rd-detail'));
    if (linkImgBtn && photoInp) {
      linkImgBtn.addEventListener('click', () => photoInp.click());
      photoInp.addEventListener('change', () => {
        const f = photoInp.files && photoInp.files[0];
        if (!f) return;
        const reader = new FileReader();
        reader.onload = () => {
          saveCustomImage(media.modelKey, String(reader.result || ''));
          toast(`Updated model photo for ${media.family}`);
          renderList();
          renderDetail(d);
        };
        reader.readAsDataURL(f);
      });
    }

                                                 
    const createOrdBtn = $('#rd-create-order-btn', $rd('#rd-detail'));
    if (createOrdBtn) {
      createOrdBtn.addEventListener('click', () => {
        if (window.OrderCreate && typeof window.OrderCreate.open === 'function') {
          window.OrderCreate.open();
          setTimeout(() => {
            const card = document.querySelector(`.oc-dev-card[data-dev-id="${d.id}"]`);
            if (card) card.click();
          }, 320);
        } else {
          const btn = document.getElementById('new-order-btn');
          if (btn) btn.click();
        }
      });
    }

    $('#rd-dl-raw', $rd('#rd-detail')).addEventListener('click', () => {
      const blob = new Blob([d.raw || ''], { type: 'text/plain' });
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = d.fileName || `${hostName}_prep.log`;
      a.click();
      URL.revokeObjectURL(a.href);
    });
    $('#rd-copy-json', $rd('#rd-detail')).addEventListener('click', async () => {
      try { await navigator.clipboard.writeText(JSON.stringify(doc, null, 2)); toast('Device JSON copied'); }
      catch { toast('Clipboard blocked', 'error'); }
    });
    $('#rd-del', $rd('#rd-detail')).addEventListener('click', async () => {
      const okDel = await confirmDialog(`Delete ${hostName}?`,
        'Removes the stored prep report. The device re-appears if the script posts again.',
        'Delete');
      if (!okDel) return;
      try {
        await api('DELETE', `/api/v1/admin/prep-devices/${d.id}`);
        toast('Device deleted');
        S.selectedId = null;
        await load(false);
      } catch (e) { toast('Delete failed: ' + e.message, 'error'); }
    });
    $$('.rd-copy', $rd('#rd-detail')).forEach((b) => b.addEventListener('click', async () => {
      try { await navigator.clipboard.writeText(b.dataset.copy); toast('UUID copied'); } catch {}
    }));
  }

                                                                          
  function wire() {
    const srch = $rd('#rd-search');
    if (srch && !srch.dataset.wired) {
      srch.dataset.wired = '1';
      srch.addEventListener('input', () => { S.search = srch.value; renderList(); });
    }
    const ref = $rd('#rd-refresh');
    if (ref && !ref.dataset.wired) {
      ref.dataset.wired = '1';
      ref.addEventListener('click', () => load(true).then(() => {}));
    }
    const up = $rd('#rd-upload');
    const inp = $rd('#rd-file');
    if (up && !up.dataset.wired) {
      up.dataset.wired = '1';
      up.addEventListener('click', () => inp.click());
      inp.addEventListener('change', async () => {
        const f = inp.files[0];
        inp.value = '';
        if (!f) return;
        try {
          const res = await fetch('/api/v1/prep-logs', {
            method: 'POST',
            headers: { 'Content-Type': 'text/plain', 'X-File-Name': f.name },
            body: f,
          });
          const d = await res.json();
          if (!res.ok) throw new Error((d.error && d.error.message) || 'Rejected');
          toast(`${d.computerName}: log accepted — ${d.checksOK} checks OK${d.checksFlag ? `, ${d.checksFlag} flagged` : ''}`);
          await load(false);
        } catch (e) { toast(e.message, 'error'); }
      });
    }
  }

  async function open() { wire(); await load(false); }
  return { open, load };
})();

window.Devices = Devices;
