/* ============================================================
   Cockpit — Order Operations
   Vanilla JS SPA against /api/v1
   ============================================================ */

(() => {
  'use strict';

  const $  = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

  /* ------------------------------------------------------------ state */

  const state = {
    token: localStorage.getItem('cockpit_token'),
    user: JSON.parse(localStorage.getItem('cockpit_user') || 'null'),
    orders: [],
    users: [],
    filter: 'All',
    search: '',
    sort: { key: 'id', dir: 'desc' },
    selectedId: null,
    detail: null,
    audit: [],
    comments: [],
    checklist: [],
    picklistMeta: null,
    attention: null,
    notifications: [],
    unread: 0,
    loadingOrders: true,
    loadingUsers: true,
    lastSync: null,
    lastHomeSync: null,

    // portaalconfiguratie (excel -> json). Alleen admins mogen dit bewerken (config:manage).
    config: {
      artikellocaties: {},
      klanten: {},
      uitgeslotenArtikelen: [],
      uitgeslotenKlanten: [],
      kleurregels: [],
      meta: {},
    },
    configBron: null,
  };

  const rolePerms = {
    viewer:   ['orders:list', 'orders:view', 'audit:view', 'comments:read'],
    operator: ['orders:list', 'orders:create', 'orders:view', 'orders:transition', 'holds:create', 'holds:resolve', 'audit:view', 'comments:read', 'comments:post', 'scan:use', 'pick:use'],
    qc:       ['orders:list', 'orders:view', 'qc:submit', 'audit:view', 'comments:read'],
    admin:    ['orders:list', 'orders:create', 'orders:view', 'orders:transition', 'holds:create', 'holds:resolve', 'qc:submit', 'audit:view', 'users:manage', 'comments:read', 'comments:post', 'scan:use', 'pick:use', 'config:manage'],
  };

  const ROLES = ['viewer', 'operator', 'qc', 'admin'];

  const STATUS_FILTERS = [
    { value: 'All',        label: 'All' },
    { value: 'Received',   label: 'Received' },
    { value: 'Processing', label: 'Processing' },
    { value: 'QC_Review',  label: 'QC review' },
    { value: 'Completed',  label: 'Completed' },
    { value: 'On_Hold',    label: 'On hold' },
  ];

  const can = (perm) => !!state.user && (rolePerms[state.user.role] || []).includes(perm);

  /* ------------------------------------------------------------ utils */

  const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));

  const fmtTime = (iso) => {
    if (!iso) return '—';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '—';
    return d.toLocaleString(undefined, {
      day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit',
    });
  };

  /** Human relative time, e.g. "in 3h 20m" / "2d ago". */
  const relTime = (iso) => {
    if (!iso) return '';
    const t = new Date(iso).getTime();
    if (Number.isNaN(t)) return '';
    let diff = t - Date.now();
    const future = diff >= 0;
    diff = Math.abs(diff);
    const m = Math.round(diff / 60000);
    let out;
    if (m < 1) out = 'just now';
    else if (m < 60) out = m + 'm';
    else if (m < 1440) out = Math.floor(m / 60) + 'h ' + (m % 60) + 'm';
    else out = Math.floor(m / 1440) + 'd ' + Math.floor((m % 1440) / 60) + 'h';
    if (out === 'just now') return out;
    return future ? 'in ' + out : out + ' ago';
  };

  const initials = (u) => String(u.displayName || u.username || '?')
    .trim().split(/\s+/).slice(0, 2).map((p) => p[0]).join('').toUpperCase();

  const isHeld = (o) => typeof o.status === 'string' && o.status.startsWith('Held_');

  const statusLabel = (s) => (typeof s === 'string' && s.startsWith('Held_'))
    ? 'On Hold · ' + s.slice(5).replace(/_/g, ' ')
    : String(s).replace(/_/g, ' ');

  const statusClass = (s) => (typeof s === 'string' && s.startsWith('Held_')) ? 'On_Hold' : s;

  const slaOf = (o) => {
    const rem = new Date(o.targetCompletionAt).getTime() - Date.now();
    if (Number.isNaN(rem)) return 'ON_TIME';
    if (rem < 0) return 'BREACHED';
    if (rem <= 4 * 3600 * 1000) return 'WARNING';
    return 'ON_TIME';
  };

  const nextStates = (st) => {
    if (st.startsWith('Held_') || st === 'Completed') return [];
    switch (st) {
      case 'Received':   return ['Processing', 'QC_Review'];
      case 'Processing': return ['Received', 'QC_Review'];
      case 'QC_Review':  return ['Processing', 'Completed'];
      default: return [];
    }
  };

  const badge = (cls, text) => `<span class="badge ${cls}"><span class="dot"></span>${esc(text)}</span>`;

  /* ------------------------------------------------------------ toasts */

  const ICON_OK  = '<svg class="t-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>';
  const ICON_ERR = '<svg class="t-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 8v5M12 16.5v.01"/></svg>';

  function toast(msg, isErr) {
    const stack = $('#toast-stack');
    const el = document.createElement('div');
    el.className = 'toast' + (isErr ? ' err' : '');
    el.innerHTML = `${isErr ? ICON_ERR : ICON_OK}<span class="t-msg">${esc(msg)}</span>
      <button class="t-close" type="button" aria-label="Dismiss">&times;</button>`;
    const kill = () => {
      el.classList.add('leaving');
      setTimeout(() => el.remove(), 200);
    };
    el.querySelector('.t-close').addEventListener('click', kill);
    stack.appendChild(el);
    setTimeout(kill, isErr ? 6000 : 3500);
    while (stack.children.length > 4) stack.firstElementChild.remove();
  }

  /* ------------------------------------------------------------ modal */

  let modalCleanup = null;

  /**
   * Promise-based modal. Resolves with an object of field values, or null when cancelled.
   * fields: [{ name, label, type:'text'|'password'|'select'|'textarea', options, value, placeholder, hint, required, autofocus }]
   */
  function modal({ title, description, fields = [], confirmLabel = 'Confirm', cancelLabel = 'Cancel', danger = false, wide = false, bodyHtml = '', hideConfirm = false }) {
    return new Promise((resolve) => {
      const root = $('#modal-root');

      const fieldHtml = fields.map((f) => {
        const id = 'mf-' + f.name;
        let control;
        if (f.type === 'select') {
          control = `<select id="${id}" name="${f.name}">${
            (f.options || []).map((o) => {
              const val = typeof o === 'string' ? o : o.value;
              const lbl = typeof o === 'string' ? o : o.label;
              return `<option value="${esc(val)}"${val === f.value ? ' selected' : ''}>${esc(lbl)}</option>`;
            }).join('')
          }</select>`;
        } else if (f.type === 'textarea') {
          control = `<textarea id="${id}" name="${f.name}" rows="3" placeholder="${esc(f.placeholder || '')}">${esc(f.value || '')}</textarea>`;
        } else {
          control = `<input id="${id}" name="${f.name}" type="${f.type || 'text'}" value="${esc(f.value || '')}"
            placeholder="${esc(f.placeholder || '')}"${f.required ? ' required' : ''}
            ${f.type === 'password' ? 'autocomplete="new-password"' : 'autocomplete="off"'} spellcheck="false">`;
        }
        return `<div class="field ${f.half ? 'half' : ''}">
            <label for="${id}">${esc(f.label)}</label>${control}
            ${f.hint ? `<span class="field-hint">${esc(f.hint)}</span>` : ''}
          </div>`;
      }).join('');

      root.innerHTML = `
        <div class="modal${wide ? ' wide' : ''}" role="dialog" aria-modal="true" aria-labelledby="modal-title">
          <form id="modal-form">
            <div class="modal-head">
              ${danger ? `<div class="modal-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/></svg></div>` : ''}
              <h2 id="modal-title">${esc(title)}</h2>
              ${description ? `<p>${esc(description)}</p>` : ''}
            </div>
            <div class="modal-body">${bodyHtml}${fieldHtml}<p class="form-error hidden" id="modal-error"></p></div>
            <div class="modal-foot">
              <button type="button" class="btn btn-secondary" data-modal-cancel>${esc(cancelLabel)}</button>
              ${hideConfirm ? '' : `<button type="submit" class="btn ${danger ? 'btn-danger' : 'btn-primary'}">${esc(confirmLabel)}</button>`}
            </div>
          </form>
        </div>`;
      root.classList.remove('hidden');

      const form = $('#modal-form', root);
      const close = (result) => {
        document.removeEventListener('keydown', onKey, true);
        root.classList.add('hidden');
        root.innerHTML = '';
        modalCleanup = null;
        resolve(result);
      };
      modalCleanup = () => close(null);

      const onKey = (e) => {
        if (e.key === 'Escape') { e.stopPropagation(); close(null); }
      };
      document.addEventListener('keydown', onKey, true);

      root.addEventListener('mousedown', (e) => { if (e.target === root) close(null); });
      $('[data-modal-cancel]', root).addEventListener('click', () => close(null));

      form.addEventListener('submit', (e) => {
        e.preventDefault();
        const data = {};
        for (const f of fields) {
          const el = form.elements[f.name];
          data[f.name] = el ? el.value : '';
          if (f.required && !String(data[f.name]).trim()) {
            const err = $('#modal-error', root);
            err.textContent = f.label + ' is required.';
            err.classList.remove('hidden');
            if (el) el.focus();
            return;
          }
        }
        close(data);
      });

      const focusTarget = fields.find((f) => f.autofocus) || fields[0];
      const el = focusTarget && form.elements[focusTarget.name];
      if (el) el.focus(); else $('[data-modal-cancel]', root).focus();
    });
  }

  const confirmDialog = (title, description, confirmLabel) =>
    modal({ title, description, confirmLabel: confirmLabel || 'Confirm', danger: true })
      .then((r) => r !== null);

  /* ------------------------------------------------------------ api */

  async function api(method, path, body) {
    let res;
    try {
      res = await fetch(path, {
        method,
        headers: {
          'Content-Type': 'application/json',
          ...(state.token ? { Authorization: 'Bearer ' + state.token } : {}),
        },
        body: body ? JSON.stringify(body) : undefined,
      });
    } catch {
      // Network-level failure (API unreachable, DNS, CORS, offline).
      throw new Error('Cannot reach the API. Check that the server is running.');
    }
    const text = await res.text();
    let data = null;
    if (text) { try { data = JSON.parse(text); } catch { /* ignore */ } }
    if (res.status === 401 && !path.endsWith('/auth/login')) {
      logout(false);
      throw new Error('Session expired, please sign in again');
    }
    if (!res.ok) {
      throw new Error((data && data.error && data.error.message) || ('HTTP ' + res.status));
    }
    return data;
  }

  /* ------------------------------------------------------------ auth */

  function showLogin() {
    $('#view-app').classList.add('hidden');
    $('#view-login').classList.remove('hidden');
    $('#login-username').focus();
  }

  async function enterApp() {
    $('#view-login').classList.add('hidden');
    $('#view-app').classList.remove('hidden');
    const u = state.user || {};
    $('#user-name').textContent = u.displayName || u.username || '';
    $('#user-role').textContent = u.role || '';
    $('#user-avatar').textContent = initials(u);
    $('#nav-users').classList.toggle('hidden', !can('users:manage'));
    $('#nav-config').classList.toggle('hidden', !can('config:manage'));
    $$('[data-perm]').forEach((el) => el.classList.toggle('hidden', !can(el.dataset.perm)));
    loadConfig();
    await probeerAutomatischeConfigJSON();
    renderStatusChips();
    route();
    loadOrders();

    clearInterval(window.__refresh);
    window.__refresh = setInterval(() => {
      if (!state.token) return clearInterval(window.__refresh);
      if (location.hash.startsWith('#/users')) renderUsers(true);
      else if (location.hash.startsWith('#/home')) { loadAttention(true); loadNotifications(true); }
      else loadOrders(true);
    }, 20000);

    clearInterval(window.__bell);
    window.__bell = setInterval(() => { if (state.token) loadNotifications(true); }, 30000);

    clearInterval(window.__clock);
    window.__clock = setInterval(updateSyncLabel, 5000);
    loadNotifications(true);
  }

  async function submitLogin(e) {
    e.preventDefault();
    const errEl = $('#login-error');
    const btn = $('#login-submit');
    errEl.classList.add('hidden');
    const username = $('#login-username').value.trim();
    const password = $('#login-password').value;
    btn.disabled = true;
    btn.textContent = 'Signing in…';
    try {
      const d = await api('POST', '/api/v1/auth/login', { username, password });
      state.token = d.token;
      state.user = d.user;
      localStorage.setItem('cockpit_token', d.token);
      localStorage.setItem('cockpit_user', JSON.stringify(d.user));
      $('#login-password').value = '';
      state.loadingOrders = true;
      enterApp();
    } catch (err) {
      errEl.textContent = err.message;
      errEl.classList.remove('hidden');
    } finally {
      btn.disabled = false;
      btn.textContent = 'Sign in';
    }
  }

  async function logout(notify) {
    const tk = state.token;
    if (notify && tk) {
      try {
        await fetch('/api/v1/auth/logout', { method: 'POST', headers: { Authorization: 'Bearer ' + tk } });
      } catch { /* ignore */ }
    }
    state.token = null;
    state.user = null;
    state.orders = [];
    state.users = [];
    state.detail = null;
    state.selectedId = null;
    state.loadingOrders = true;
    state.loadingUsers = true;
    state.notifications = [];
    state.unread = 0;
    state.attention = null;
    clearInterval(window.__refresh);
    clearInterval(window.__clock);
    clearInterval(window.__bell);
    $('#notif-panel').classList.add('hidden');
    localStorage.removeItem('cockpit_token');
    localStorage.removeItem('cockpit_user');
    if (modalCleanup) modalCleanup();
    location.hash = '';
    showLogin();
  }

  /* ------------------------------------------------------------ routing */

  function showPane(name) {
    $('#view-home').classList.toggle('hidden', name !== 'home');
    $('#view-orders').classList.toggle('hidden', name !== 'orders');
    $('#view-users').classList.toggle('hidden', name !== 'users');
    $('#view-config').classList.toggle('hidden', name !== 'config');
    $$('.nav-link').forEach((a) => {
      const active = a.dataset.nav === name;
      a.classList.toggle('active', active);
      if (active) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
    });
  }

  function route() {
    if (!state.token) return showLogin();
    if (can('users:manage') && location.hash.startsWith('#/users')) {
      showPane('users');
      renderUsers(true);
      return;
    }
    if (can('config:manage') && location.hash.startsWith('#/config')) {
      showPane('config');
      renderConfig();
      return;
    }
    if (location.hash.startsWith('#/orders')) {
      showPane('orders');
      return;
    }
    if (location.hash === '' || location.hash === '#' || location.hash === '#/') {
      history.replaceState(null, '', '#/home');
    }
    showPane('home');
    loadAttention(true);
    loadNotifications(true);
  }

  /* ------------------------------------------------------------ my work / attention */

  const REASON_LABEL = {
    SLA_BREACHED: 'SLA breached',
    SLA_WARNING:  'SLA at risk',
    ON_HOLD:      'On hold',
    AWAITING_QC:  'Awaiting QC',
    FRESH:        'New order',
  };

  async function loadAttention(silent) {
    try {
      const d = await api('GET', '/api/v1/attention');
      state.attention = d.attention;
      state.lastHomeSync = Date.now();
      renderAttention();
      const note = $('#home-refresh-text');
      if (note) note.textContent = 'Live · just synced';
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  function renderAttention() {
    const p = state.attention;
    if (!p) return;
    const chip = (num, cls, lbl) =>
      `<div class="stat ${cls}"><span class="num">${num}</span><span class="lbl">${lbl}</span></div>`;
    $('#attention-summary').innerHTML = [
      chip(p.slaBreached, p.slaBreached ? 'bad' : 'good', 'Breached'),
      chip(p.slaWarning, 'warn', 'At risk'),
      chip(p.onHold, 'hold', 'On hold'),
      chip(p.awaitingQc, 'accent', 'Awaiting QC'),
    ].join('');

    const grid = $('#attention-cards');
    const cards = p.cards || [];
    $('#home-empty').classList.toggle('hidden', cards.length > 0);
    grid.innerHTML = cards.map((c) => `
      <article class="acard sev-${esc(c.severity)}" data-id="${c.orderId}" tabindex="0" role="button"
               aria-label="Open order ${esc(c.orderNumber)}">
        <div class="acard-top">
          <span class="strong mono">${esc(c.orderNumber)}</span>
          ${badge('sev-' + esc(c.severity), esc(REASON_LABEL[c.reason] || c.reason))}
        </div>
        <div class="acard-mid">
          <span>${badge('status-' + esc(statusClass(c.status)), statusLabel(c.status))}</span>
          <span class="rel">${esc(c.reasonDetail || '')}</span>
        </div>
        <div class="acard-foot">
          <span>Target ${fmtTime(c.targetAt)}<span class="sub">${esc(relTime(c.targetAt))}</span></span>
          <span class="muted">Updated ${esc(relTime(c.updatedAt))}</span>
        </div>
      </article>`).join('');
  }

  function goToOrder(id) {
    if (!location.hash.startsWith('#/orders')) location.hash = '#/orders';
    openDetail(id);
  }

  /* ------------------------------------------------------------ scanner */

  async function runScan(code, feedbackEl, onHit) {
    if (!can('scan:use')) {
      feedbackEl.textContent = 'Your role does not permit scanning.';
      feedbackEl.className = 'scan-feedback bad';
      return null;
    }
    feedbackEl.textContent = 'Looking up…';
    feedbackEl.className = 'scan-feedback';
    try {
      const d = await api('POST', '/api/v1/scan', { code });
      feedbackEl.textContent = `Matched ${d.order.orderNumber}`;
      feedbackEl.className = 'scan-feedback ok';
      if (onHit) onHit(d);
      return d;
    } catch (err) {
      feedbackEl.textContent = err.status === 404 ? `No order matches “${code}”` : err.message;
      feedbackEl.className = 'scan-feedback bad';
      return null;
    }
  }

  function renderScanHit(d) {
    const o = d.order;
    const sla = o.sla ? o.sla.status : 'ON_TIME';
    $('#scan-result').innerHTML = `
      <div class="scan-hit" data-id="${o.id}" tabindex="0" role="button" aria-label="Open order ${esc(o.orderNumber)}">
        <div class="acard-top">
          <span class="strong mono">${esc(o.orderNumber)}</span>
          ${badge('sla-' + esc(sla), sla.replace('_', ' '))}
        </div>
        <div class="acard-mid">
          <span>${badge('status-' + esc(statusClass(o.status)), statusLabel(o.status))}</span>
          <span class="muted">Scanned by ${esc(d.event.scannedBy)} · ${esc(relTime(d.event.scannedAt))}</span>
        </div>
      </div>`;
  }

  async function doScan(code) {
    await runScan(code, $('#scan-feedback'), renderScanHit);
  }

  async function doScanInto(code, feedbackEl) {
    await runScan(code, feedbackEl, (d) => {
      toast(`Matched ${d.order.orderNumber}`);
      goToOrder(d.order.id);
    });
  }

  /* ------------------------------------------------------------ notifications */

  function renderNotifications() {
    const badge = $('#bell-badge');
    if (state.unread > 0) {
      badge.textContent = state.unread > 99 ? '99+' : String(state.unread);
      badge.classList.remove('hidden');
    } else {
      badge.classList.add('hidden');
    }

    const list = $('#notif-list');
    const notes = state.notifications || [];
    list.innerHTML = notes.length
      ? notes.map((n) => `
          <div class="notif-item${n.readAt ? '' : ' unread'}" data-id="${n.orderId}" tabindex="0" role="button">
            <div class="notif-title">${esc(n.title)}</div>
            <div class="notif-meta">${esc(n.orderNumber)} · ${esc(relTime(n.createdAt))}${n.readAt ? '' : ' · <strong>new</strong>'}</div>
          </div>`).join('')
      : '<p class="empty-inline">Nothing here yet. Mentions of your username will show up.</p>';
  }

  async function loadNotifications(silent) {
    try {
      const d = await api('GET', '/api/v1/notifications');
      state.notifications = d.notifications || [];
      state.unread = d.unread || 0;
      renderNotifications();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  async function markNotificationsRead() {
    const ids = (state.notifications || []).filter((n) => !n.readAt).map((n) => n.id);
    if (!ids.length) return;
    try {
      await api('POST', '/api/v1/notifications/read', { ids });
      await loadNotifications(true);
    } catch (err) {
      toast(err.message, true);
    }
  }

  /* ------------------------------------------------------------ orders */

  async function loadOrders(silent) {
    try {
      const d = await api('GET', '/api/v1/orders');
      state.orders = d.orders || [];
      state.loadingOrders = false;
      state.lastSync = Date.now();
      renderStats();
      renderStatusChips();
      renderTable();
      updateSyncLabel();
    } catch (err) {
      state.loadingOrders = false;
      renderTable();
      if (!silent) toast(err.message, true);
    }
  }

  function updateSyncLabel() {
    const note = $('#refresh-note');
    const text = $('#refresh-text');
    if (!note || !text) return;
    if (!state.lastSync) { text.textContent = 'Not synced'; note.classList.add('stale'); return; }
    const secs = Math.round((Date.now() - state.lastSync) / 1000);
    text.textContent = secs < 10 ? 'Live · just synced' : `Live · synced ${secs}s ago`;
    note.classList.toggle('stale', secs > 60);
  }

  function counts() {
    const c = { All: state.orders.length, Received: 0, Processing: 0, QC_Review: 0, Completed: 0, On_Hold: 0 };
    for (const o of state.orders) {
      if (isHeld(o)) c.On_Hold++;
      else if (c[o.status] !== undefined) c[o.status]++;
    }
    return c;
  }

  function renderStats() {
    const c = counts();
    let breached = 0, warning = 0, onTime = 0;
    for (const o of state.orders) {
      const s = slaOf(o);
      if (s === 'BREACHED') breached++;
      else if (s === 'WARNING') warning++;
      else onTime++;
    }
    const card = (num, cls, lbl) =>
      `<div class="stat ${cls}"><span class="num">${num}</span><span class="lbl">${lbl}</span></div>`;

    $('#stats').innerHTML = [
      '<div class="stat-divider">Pipeline</div>',
      card(c.All, 'accent', 'Total'),
      card(c.Received, 'info', 'Received'),
      card(c.Processing, 'warn', 'Processing'),
      card(c.QC_Review, 'accent', 'QC review'),
      card(c.Completed, 'good', 'Completed'),
      card(c.On_Hold, 'hold', 'On hold'),
      '<div class="stat-divider">Service level</div>',
      card(onTime, 'good', 'On time'),
      card(warning, 'warn', 'At risk'),
      card(breached, 'bad', 'Breached'),
    ].join('');

    const sub = $('#orders-subtitle');
    if (sub) {
      sub.textContent = breached
        ? `${breached} order${breached === 1 ? '' : 's'} past target — needs attention.`
        : 'Live view of the fulfilment pipeline. No SLA breaches.';
    }
  }

  function renderStatusChips() {
    const c = counts();
    $('#status-chips').innerHTML = STATUS_FILTERS.map((f) => `
      <button type="button" role="tab" aria-selected="${state.filter === f.value}"
              class="chip${state.filter === f.value ? ' active' : ''}" data-filter="${f.value}">
        ${esc(f.label)}<span class="cnt">${c[f.value] ?? 0}</span>
      </button>`).join('');
  }

  function filteredOrders() {
    const q = state.search.trim().toLowerCase();
    const rows = state.orders.filter((o) => {
      if (state.filter !== 'All' && !(o.status === state.filter || (state.filter === 'On_Hold' && isHeld(o)))) return false;
      if (q && !(String(o.id).includes(q) || String(o.orderNumber).toLowerCase().includes(q))) return false;
      return true;
    });

    const SLA_RANK = { BREACHED: 0, WARNING: 1, ON_TIME: 2 };
    const { key, dir } = state.sort;
    const mul = dir === 'asc' ? 1 : -1;

    return rows.sort((a, b) => {
      let va, vb;
      if (key === 'sla') { va = SLA_RANK[slaOf(a)]; vb = SLA_RANK[slaOf(b)]; }
      else if (key === 'targetCompletionAt' || key === 'createdAt') {
        va = new Date(a[key]).getTime() || 0; vb = new Date(b[key]).getTime() || 0;
      } else if (key === 'status') {
        va = statusLabel(a.status); vb = statusLabel(b.status);
      } else if (key === 'id') { va = Number(a.id); vb = Number(b.id); }
      else { va = String(a[key] ?? '').toLowerCase(); vb = String(b[key] ?? '').toLowerCase(); }
      if (va < vb) return -1 * mul;
      if (va > vb) return 1 * mul;
      return (Number(a.id) - Number(b.id)) * mul;
    });
  }

  function skeletonRows(n, cols) {
    return Array.from({ length: n }, () =>
      `<tr>${Array.from({ length: cols }, () => '<td><span class="sk"></span></td>').join('')}</tr>`
    ).join('');
  }

  function renderTable() {
    const tbody = $('#orders-table tbody');
    const countEl = $('#row-count');

    $$('#orders-table th.sortable').forEach((th) => {
      const on = th.dataset.sort === state.sort.key;
      th.classList.toggle('sorted', on);
      th.classList.toggle('asc', on && state.sort.dir === 'asc');
      th.classList.toggle('desc', on && state.sort.dir === 'desc');
      th.setAttribute('aria-sort', on ? (state.sort.dir === 'asc' ? 'ascending' : 'descending') : 'none');
    });

    if (state.loadingOrders) {
      tbody.innerHTML = skeletonRows(6, 6);
      countEl.textContent = '';
      return;
    }

    const rows = filteredOrders();

    if (!rows.length) {
      const filtering = state.search || state.filter !== 'All';
      tbody.innerHTML = `<tr><td colspan="6">
        <div class="empty">
          <div class="empty-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/></svg></div>
          <h3>${filtering ? 'No matching orders' : 'No orders yet'}</h3>
          <p>${filtering ? 'Try a different status filter or search term.' : 'Orders will appear here as soon as they are received.'}</p>
          ${filtering ? '<button type="button" class="btn btn-secondary" id="clear-filters">Clear filters</button>' : ''}
        </div></td></tr>`;
      countEl.textContent = state.orders.length ? `0 of ${state.orders.length} orders` : '';
      return;
    }

    tbody.innerHTML = rows.map((o) => {
      const sla = slaOf(o);
      return `
      <tr class="row${state.selectedId === o.id ? ' selected' : ''}" data-id="${o.id}" tabindex="0">
        <td class="mono">${o.id}</td>
        <td class="strong">${esc(o.orderNumber)}</td>
        <td>${badge('status-' + esc(statusClass(o.status)), statusLabel(o.status))}</td>
        <td class="time">${fmtTime(o.targetCompletionAt)}<span class="rel">${esc(relTime(o.targetCompletionAt))}</span></td>
        <td>${badge('sla-' + sla, sla.replace('_', ' '))}</td>
        <td class="time">${fmtTime(o.createdAt)}</td>
      </tr>`;
    }).join('');

    countEl.textContent = rows.length === state.orders.length
      ? `${rows.length} order${rows.length === 1 ? '' : 's'}`
      : `${rows.length} of ${state.orders.length} orders`;
  }

  /* ------------------------------------------------------------ detail */

  function closeDetail() {
    state.selectedId = null;
    state.detail = null;
    state.audit = [];
    $('#detail-panel').classList.add('hidden');
    $('#orders-split').classList.add('no-detail');
    renderTable();
  }

  async function openDetail(id) {
    state.selectedId = id;
    state.comments = [];
    renderTable();
    renderDetail();
    try {
      const [d, a] = await Promise.all([
        api('GET', '/api/v1/orders/' + id),
        api('GET', '/api/v1/orders/' + id + '/audit'),
      ]);
      state.detail = d.order;
      state.audit = a.auditLogs || [];
      renderDetail();
      renderTable();
      loadComments(id);
    } catch (err) {
      toast(err.message, true);
    }
  }

  function commentsListHtml(list) {
    if (!list.length) return '<p class="empty-inline">No comments yet.</p>';
    return list.map(renderCommentHtml).join('');
  }

  function renderCommentHtml(c) {
    return `
      <div class="list-item comment-item${c.user === state.user.username ? ' mine' : ''}">
        <div>
          <div class="reason">${esc(c.body)}</div>
          <div class="meta-line">${esc(c.user)} · ${fmtTime(c.createdAt)}</div>
          ${(c.mentions || []).length ? `<div class="meta-line mentions">→ ${c.mentions.map((m) => '@' + esc(m)).join(' ')}</div>` : ''}
        </div>
      </div>`;
  }

  async function loadComments(id) {
    try {
      const d = await api('GET', `/api/v1/orders/${id}/comments`);
      state.comments = d.comments || [];
      const box = $('#comments-container');
      if (box) box.innerHTML = commentsListHtml(state.comments);
      const h = $('#comments-section .count');
      if (h) h.textContent = state.comments.length;
      if (!state.comments.length) {
        const sec = $('#comments-section');
        if (sec) {
          const empty = sec.querySelector('.empty-inline');
          if (empty) empty.textContent = 'No comments yet.';
        }
      }
    } catch (err) {
      toast(err.message, true);
    }
  }

  function renderDetail() {
    const panel = $('#detail-panel');
    const o = state.detail;
    if (!o) return;

    const onHold = o.onHold;
    const held = String(o.status).startsWith('Held_');
    const completed = o.status === 'Completed';
    const targets = nextStates(o.status);
    const sla = o.sla || { status: slaOf(o) };
    const holds = o.activeHolds || [];
    const qcs = o.qcChecks || [];

    const holdsHtml = holds.length
      ? holds.map((h) => `
          <div class="list-item hold-item">
            <div>
              <div class="reason">${esc(h.reason)}</div>
              <div class="meta-line">${esc(h.createdBy)} · ${fmtTime(h.createdAt)}</div>
            </div>
            ${can('holds:resolve') ? `<button class="btn btn-secondary btn-sm" data-act="resolve" data-id="${esc(h.id)}">Resolve</button>` : ''}
          </div>`).join('')
      : '<p class="empty-inline">No active holds.</p>';

    const qcHtml = qcs.length
      ? qcs.map((c) => `
          <div class="list-item qc-item">
            <div class="qc-top">
              ${badge('status-' + esc(c.status), c.status)}
              <span class="meta-line" style="margin:0">${esc(c.inspectorId)} · ${fmtTime(c.createdAt)}</span>
            </div>
            ${c.notes ? `<div class="notes">${esc(c.notes)}</div>` : ''}
          </div>`).join('')
      : '<p class="empty-inline">No QC checks yet.</p>';

    const auditHtml = state.audit.length
      ? `<div class="timeline">${state.audit.map((l) => `
          <div class="audit-item">
            <div class="action">${esc(l.action)}</div>
            <div class="when"><span class="by">${esc(l.performedBy)}</span> · ${fmtTime(l.timestamp)}</div>
          </div>`).join('')}</div>`
      : '<p class="empty-inline">No audit entries.</p>';

    const commentsHtml = `
      <div id="comments-container">${commentsListHtml(state.comments || [])}</div>
      ${can('comments:post') ? `
        <form class="comment-composer" id="comment-form">
          <textarea id="comment-input" rows="2" maxlength="2000" placeholder="Add a comment. Use @username to mention someone…" aria-label="Comment"></textarea>
          <div class="composer-foot">
            <span class="muted">@username to mention</span>
            <button type="submit" class="btn btn-secondary btn-sm">Comment</button>
          </div>
        </form>` : ''}`;

    const scanHtml = can('scan:use') ? `
      <div class="scan-inline">
        <input id="drawer-scan-input" class="mono" placeholder="Scan barcode → order number" autocomplete="off">
        <div id="drawer-scan-feedback" class="scan-feedback" aria-live="polite"></div>
      </div>` : '';

    const actions = [];
    if (can('orders:transition') && !held && !completed && targets.length) {
      actions.push(`
        <form class="action-block" id="transition-form">
          <span class="label">Move to next state</span>
          <div class="action-row">
            <select id="transition-target" aria-label="Target status">${targets.map((s) => `<option value="${s}">${statusLabel(s)}</option>`).join('')}</select>
            <button type="submit" class="btn btn-primary btn-sm">Transition</button>
          </div>
        </form>`);
    }
    if (can('holds:create') && !completed && !onHold) {
      actions.push(`
        <form class="action-block" id="hold-form">
          <span class="label">Place a hold</span>
          <div class="action-row">
            <input id="hold-reason" placeholder="Reason for hold" required autocomplete="off">
            <button type="submit" class="btn btn-secondary btn-sm">Hold</button>
          </div>
        </form>`);
    }
    if (can('qc:submit') && !completed) {
      actions.push(`
        <form class="action-block" id="qc-form">
          <span class="label">Record QC check</span>
          <div class="action-row">
            <select id="qc-status" aria-label="QC result" style="flex:0 0 96px"><option value="PASS">PASS</option><option value="FAIL">FAIL</option></select>
            <input id="qc-notes" placeholder="Notes (optional)" autocomplete="off">
            <button type="submit" class="btn btn-secondary btn-sm">Submit</button>
          </div>
        </form>`);
    }

    panel.innerHTML = `
      <div class="detail-head">
        <div class="detail-head-text">
          <div class="eyebrow">Order #${o.id}</div>
          <h2>${esc(o.orderNumber)}</h2>
          <div class="detail-badges">
            ${badge('status-' + esc(statusClass(o.status)), statusLabel(o.status))}
            ${badge('sla-' + esc(sla.status), String(sla.status).replace('_', ' '))}
          </div>
        </div>
        <button type="button" class="icon-btn" id="detail-close" aria-label="Close detail" title="Close (Esc)">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
        </button>
      </div>
      <div class="detail-scroll">
        <div class="meta">
          <div class="item"><span class="k">Target completes</span><span class="v">${fmtTime(o.targetCompletionAt)}<span class="sub">${esc(relTime(o.targetCompletionAt))}</span></span></div>
          <div class="item"><span class="k">Created</span><span class="v">${fmtTime(o.createdAt)}<span class="sub">${esc(relTime(o.createdAt))}</span></span></div>
          <div class="item"><span class="k">Last updated</span><span class="v">${fmtTime(o.updatedAt)}<span class="sub">${esc(relTime(o.updatedAt))}</span></span></div>
          <div class="item"><span class="k">Holds</span><span class="v">${holds.length ? holds.length + ' active' : 'None'}</span></div>
        </div>
        ${actions.length ? `<section class="detail-section"><h3>Actions</h3>${actions.join('')}</section>` : ''}
        <section class="detail-section"><h3>Active holds ${holds.length ? `<span class="count">${holds.length}</span>` : ''}</h3>${holdsHtml}</section>
        <section class="detail-section"><h3>QC checks ${qcs.length ? `<span class="count">${qcs.length}</span>` : ''}</h3>${qcHtml}</section>
        <section class="detail-section" id="comments-section"><h3>Comments ${state.comments && state.comments.length ? `<span class="count">${state.comments.length}</span>` : ''}</h3>${commentsHtml}</section>
        ${can('scan:use') ? `<section class="detail-section"><h3>Scan</h3>${scanHtml}</section>` : ''}
        <section class="detail-section"><h3>Audit trail ${state.audit.length ? `<span class="count">${state.audit.length}</span>` : ''}</h3>${auditHtml}</section>
      </div>`;

    panel.classList.remove('hidden');
    $('#orders-split').classList.remove('no-detail');
  }

  /* ------------------------------------------------------------ actions */

  async function act(method, path, body, okMsg) {
    try {
      await api(method, path, body);
      toast(okMsg || 'Done');
      await loadOrders(true);
      if (state.selectedId) await openDetail(state.selectedId);
      return true;
    } catch (err) {
      toast(err.message, true);
      return false;
    }
  }

  async function openCreateOrder() {
    const now = new Date(Date.now() + 24 * 3600 * 1000 - new Date().getTimezoneOffset() * 60000);
    const res = await modal({
      title: 'Create order',
      description: 'Add a new order to the pipeline.',
      confirmLabel: 'Create order',
      fields: [
        { name: 'orderNumber', label: 'Order number', placeholder: 'e.g. ORD-10432', required: true, autofocus: true },
        { name: 'target', label: 'Target completion', type: 'datetime-local', value: now.toISOString().slice(0, 16), hint: 'Drives the SLA state. Leave blank to use the server default.' },
      ],
    });
    if (!res) return;
    const body = { orderNumber: res.orderNumber.trim() };
    if (res.target) {
      const dt = new Date(res.target);
      if (!Number.isNaN(dt.getTime())) body.targetCompletionAt = dt.toISOString();
    }
    await act('POST', '/api/v1/orders', body, 'Order created');
  }

  /* ------------------------------------------------------------ users */

  function renderRoleLegend() {
    const byRole = { viewer: 0, operator: 0, qc: 0, admin: 0 };
    for (const u of state.users) if (byRole[u.role] !== undefined) byRole[u.role]++;
    $('#role-legend').innerHTML = ROLES.map((r) => `
      <div class="role-card">
        <div class="head">${badge('role-' + r, r)}<span class="n">${byRole[r]} user${byRole[r] === 1 ? '' : 's'}</span></div>
        <div class="perm-list">${rolePerms[r].map((p) => `<span class="perm">${esc(p)}</span>`).join('')}</div>
      </div>`).join('');
  }

  async function renderUsers(reload) {
    if (!can('users:manage')) return;
    if (reload) {
      try {
        const d = await api('GET', '/api/v1/users');
        state.users = d.users || [];
        state.loadingUsers = false;
      } catch (err) {
        state.loadingUsers = false;
        toast(err.message, true);
        return;
      }
    }

    const tbody = $('#users-table tbody');
    if (state.loadingUsers) { tbody.innerHTML = skeletonRows(4, 6); return; }

    if (!state.users.length) {
      tbody.innerHTML = `<tr><td colspan="6"><div class="empty">
        <div class="empty-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="7.5" r="3.5"/><path d="M16 20v-1.5a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4V20"/></svg></div>
        <h3>No users</h3><p>Add your first operator to get started.</p></div></td></tr>`;
      $('#user-count').textContent = '';
      renderRoleLegend();
      return;
    }

    tbody.innerHTML = state.users.map((u) => {
      const isMe = u.id === (state.user && state.user.id);
      return `
      <tr>
        <td class="mono">${u.id}</td>
        <td class="strong">${esc(u.username)}${isMe ? ' <span class="muted">(you)</span>' : ''}</td>
        <td>${esc(u.displayName || '—')}</td>
        <td>${badge('role-' + esc(u.role), u.role)}</td>
        <td class="time">${fmtTime(u.createdAt)}</td>
        <td class="actions"><span class="row-actions">
          <button class="btn btn-secondary btn-sm" data-act="role" data-id="${u.id}">Role</button>
          <button class="btn btn-secondary btn-sm" data-act="pwd" data-id="${u.id}">Password</button>
          ${isMe ? '' : `<button class="btn btn-secondary btn-sm danger-ghost" data-act="del" data-id="${u.id}">Delete</button>`}
        </span></td>
      </tr>`;
    }).join('');

    $('#user-count').textContent = `${state.users.length} user${state.users.length === 1 ? '' : 's'}`;
    renderRoleLegend();
  }

  async function openCreateUser() {
    const res = await modal({
      title: 'Add user',
      description: 'Create an account and assign its role.',
      confirmLabel: 'Add user',
      fields: [
        { name: 'username', label: 'Username', required: true, autofocus: true, placeholder: 'jdoe' },
        { name: 'displayName', label: 'Display name', placeholder: 'Jane Doe' },
        { name: 'role', label: 'Role', type: 'select', options: ROLES, value: 'viewer' },
        { name: 'password', label: 'Password', type: 'password', required: true, hint: 'Minimum 8 characters.' },
      ],
    });
    if (!res) return;
    const ok = await act('POST', '/api/v1/users', {
      username: res.username.trim(),
      displayName: res.displayName.trim(),
      role: res.role,
      password: res.password,
    }, 'User created');
    if (ok) renderUsers(true);
  }

  function openShortcuts() {
    const rows = [
      ['My Work', ['h']],
      ['Focus search', ['/']],
      ['New order', ['n']],
      ['Go to My Work', ['g', 'h']],
      ['Go to orders', ['g', 'o']],
      ['Go to users', ['g', 'u']],
      ['Go to config (admin)', ['g', 'c']],
      ['Refresh data', ['r']],
      ['Close panel / dialog', ['Esc']],
      ['This dialog', ['?']],
    ];
    modal({
      title: 'Keyboard shortcuts',
      hideConfirm: true,
      cancelLabel: 'Close',
      bodyHtml: `<div class="shortcut-table">${rows.map(([label, keys]) =>
        `<div class="shortcut-row"><span>${esc(label)}</span><span class="keys">${keys.map((k) => `<kbd>${esc(k)}</kbd>`).join('')}</span></div>`
      ).join('')}</div>`,
    });
  }


  const CONFIG_PERSIST_KEY = 'cockpit_config';

  function hasConfigMeta() {
    const m = state.config.meta || {};
    return ['artikelweergave', 'klantenlijst', 'pqRegels', 'excelRegels'].some((k) => !!m[k]);
  }

  function loadConfig() {
    try {
      const raw = localStorage.getItem(CONFIG_PERSIST_KEY);
      if (raw) {
        state.config = JSON.parse(raw);
        if (!state.configBron && hasConfigMeta()) state.configBron = 'deze browser (opgeslagen)';
      }
    } catch (e) { /* corrupte localStorage — negeer */ }
  }

  async function saveConfig() {
    try {
      localStorage.setItem(CONFIG_PERSIST_KEY, JSON.stringify(state.config));
    } catch (e) {
      console.error(e);
    }
  }

  function isXlsxBeschikbaar() { return typeof window.XLSX !== 'undefined'; }

  function readWorkbook(file) {
    if (!isXlsxBeschikbaar()) {
      return Promise.reject(new Error('Excel-library kon niet worden geladen. Open het portaal in een browser met internetverbinding.'));
    }
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        try { resolve(XLSX.read(new Uint8Array(e.target.result), { type: 'array', cellDates: true })); }
        catch (err) { reject(err); }
      };
      reader.onerror = reject;
      reader.readAsArrayBuffer(file);
    });
  }

  function fileInput(accept, onFile) {
    const inp = document.createElement('input');
    inp.type = 'file';
    inp.accept = accept;
    inp.onchange = async (e) => {
      if (!e.target.files[0]) return;
      try {
        await onFile(e.target.files[0]);
      } catch (err) {
        console.error(err);
        toast(err && err.message ? err.message : 'Bestand kon niet worden verwerkt.', true);
      }
    };
    return inp;
  }

  async function handleArtikelweergave(file) {
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rows = XLSX.utils.sheet_to_json(ws, { defval: null });
    const map = {};
    let metLocatie = 0;
    for (const r of rows) {
      const code = String(r['Artikelcode'] ?? '').trim();
      const loc = r['Locatie DBC'];
      if (code && loc) { map[code] = String(loc).trim(); metLocatie++; }
    }
    state.config.artikellocaties = map;
    state.config.meta.artikelweergave = { bestand: file.name, aantal: rows.length, metLocatie, tijd: Date.now() };
    await saveConfig();
    toast(`Artikelweergave verwerkt: ${rows.length} artikelen, ${metLocatie} met locatie`);
    renderConfig();
  }

  async function handleKlantenlijst(file) {
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rows = XLSX.utils.sheet_to_json(ws, { range: 1, defval: null });
    const map = {};
    for (const r of rows) {
      const nr = r['Klantnummer'];
      if (nr == null) continue;
      map[String(nr).trim()] = r['Groep'] || 'Overig';
    }
    state.config.klanten = map;
    state.config.meta.klantenlijst = { bestand: file.name, aantal: Object.keys(map).length, tijd: Date.now() };
    await saveConfig();
    toast(`Klantenlijst verwerkt: ${Object.keys(map).length} klanten`);
    renderConfig();
  }

  async function handlePQRegels(file) {
    const wb = await readWorkbook(file);
    const artikelenSet = new Set();
    if (wb.SheetNames.includes('Filter artikelen')) {
      const ws = wb.Sheets['Filter artikelen'];
      const rows = XLSX.utils.sheet_to_json(ws, { defval: null });
      for (const r of rows) {
        for (const k of Object.keys(r)) {
          if (r[k]) artikelenSet.add(String(r[k]).trim());
        }
      }
    }
    const klantenSet = new Set();
    if (wb.SheetNames.includes('Filter klanten')) {
      const ws = wb.Sheets['Filter klanten'];
      const rows = XLSX.utils.sheet_to_json(ws, { header: 1, defval: null });
      for (const row of rows) {
        for (let i = 1; i < row.length; i++) {
          if (row[i] !== null && row[i] !== '') klantenSet.add(String(row[i]).trim());
        }
      }
    }
    state.config.uitgeslotenArtikelen = [...artikelenSet];
    state.config.uitgeslotenKlanten = [...klantenSet];
    state.config.meta.pqRegels = { bestand: file.name, artikelen: artikelenSet.size, klanten: klantenSet.size, tijd: Date.now() };
    await saveConfig();
    toast(`Uitsluitingsregels verwerkt: ${artikelenSet.size} artikelen, ${klantenSet.size} klanten`);
    renderConfig();
  }

  async function handleExcelRegels(file) {
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rows = XLSX.utils.sheet_to_json(ws, { header: 1, defval: null });
    const regels = [];
    for (const row of rows) {
      const kolomCel = row[1], condCel = row[2], kleurCel = row[3];
      if (typeof kolomCel !== 'string' || !kolomCel.startsWith('Kolom:')) continue; // titelrij overslaan
      const kolom = kolomCel.replace('Kolom:', '').trim();
      const kleur = String(kleurCel || '').replace('Kleur:', '').trim();
      const condStr = String(condCel || '').replace('Celwaarde', '').trim();
      let operator = null, waarde = null;
      if (condStr.startsWith('bevat')) { operator = 'bevat'; waarde = condStr.replace('bevat', '').trim(); }
      else if (condStr.startsWith('=')) { operator = '='; waarde = condStr.replace('=', '').trim(); }
      if (kolom && operator && waarde) regels.push({ kolom, operator, waarde, kleur });
    }
    state.config.kleurregels = regels;
    state.config.meta.excelRegels = { bestand: file.name, aantal: regels.length, tijd: Date.now() };
    await saveConfig();
    toast(`Kleurregels verwerkt: ${regels.length} regels`);
    renderConfig();
  }

  /* ------------------------------------------------------------ picklijst (dagelijkse verkoopregels) */

  // Geporte uit test.html: evalueert de kleurregels tegen een verkoopregel.

  function evalRegel(row, regel) {
    const veldwaarde = row[regel.kolom];
    if (regel.operator === 'bevat') {
      return String(veldwaarde ?? '').toLowerCase().includes(regel.waarde.toLowerCase());
    }
    // operator '='
    const num = Number(regel.waarde);
    if (!Number.isNaN(num) && veldwaarde !== null && veldwaarde !== undefined && veldwaarde !== '') {
      return Number(veldwaarde) === num;
    }
    return String(veldwaarde ?? '' ).trim().toLowerCase() === regel.waarde.trim().toLowerCase();
  }

  function regelStatus(row, kleurregels) {
    const regels = kleurregels || [];
    const aantal = row['Aantal te leveren'] ?? 0;

    // Retourregel: negatief aantal betekent dat hardware teruggeboekt moet
    // worden op voorraad. Dit is geen pickregel - nooit meenemen.

    if (aantal < 0) {
      return { ok: true, retour: true, label: `Retour: ${Math.abs(aantal)} stuks terugboeken op voorraad` };
    }

    const groenMatch = regels.find(r => r.kleur.toLowerCase() === 'groen' && evalRegel(row, r));
    if (groenMatch) {
      return { ok: true, retour: false, label: `Uitzondering (${groenMatch.kolom} ${groenMatch.operator === 'bevat' ? 'bevat' : '='} "${groenMatch.waarde}")` };
    }

    const vrdRuw = row['Vrd.'];
    const vrd = (vrdRuw === null || vrdRuw === undefined) ? 0 : vrdRuw;
    return { ok: vrd >= aantal, retour: false, label: '' };
  }

  async function handleVerkoopregels(file) {
    if (!state.config.meta.artikelweergave || !state.config.meta.klantenlijst) {
      toast('Upload eerst Artikelweergave en Klantenlijst bij Configuratie.', true);
      return;
    }
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rawRows = XLSX.utils.sheet_to_json(ws, { defval: null });

    const uitArt = new Set(state.config.uitgeslotenArtikelen || []);
    const uitKlant = new Set(state.config.uitgeslotenKlanten || []);
    const voor = rawRows.length;

    const regels = rawRows
      .filter(r => !uitArt.has(String(r['Artikel'] ?? '')))
      .filter(r => !uitKlant.has(String(r['Vrk.rel.'] ?? '')))
      .map(r => {
        const klantnr = String(r['Vrk.rel.'] ?? '');
        const groep = state.config.klanten[klantnr] || 'Overig';
        const locatie = state.config.artikellocaties[String(r['Artikel'] ?? '')] || 'Geen locatie gekoppeld';
        const status = regelStatus(r, state.config.kleurregels);
        return {
          orderNr: r['OrderNr.'], artikel: r['Artikel'], naam: r['Naam'],
          omschrijving: r['Omschrijving'], aantal: r['Aantal te leveren'] ?? 0,
          locatie, ok: status.ok, retour: status.retour,
        };
      });

    // groepeer per order en bouw pickregels (alleen aantal > 0; retouren
    // horen niet bij het picken).
    const orderMap = {};
    for (const r of regels) {
      if (!r.ok) continue; // enkel volledig leverbare regels picken
      if (!orderMap[r.orderNr]) orderMap[r.orderNr] = { orderNr: r.orderNr, regels: [] };
      orderMap[r.orderNr].regels.push(r);
    }
    const orderLines = Object.values(orderMap).map(o => ({
      orderNr: String(o.orderNr).trim(),
      items: o.regels
        .filter(r => (Number(r.aantal) || 0) > 0)
        .sort((a, b) => String(a.locatie).localeCompare(String(b.locatie), 'nl', { numeric: true, sensitivity: 'base' })))
        .map(r => ({
          artikel: String(r.artikel ?? ''), omschrijving: String(r.omschrijving ?? ''),
          locatie: String(r.locatie ?? ''), aantal: Number(r.aantal) || 0,
        })),
    })).filter(o => o.items.length > 0);

    if (!orderLines.length) {
      toast('Geen leverbare pickregels gevonden in het bestand.', true);
      return;
    }

    const byNumber = {};
    for (const ord of state.orders) byNumber[String(ord.orderNumber).trim().toLowerCase()] = ord;

    let matched = 0;
    const missed = [];
    for (const line of orderLines) {
      const ord = byNumber[line.orderNr.toLowerCase()];
      if (!ord) { missed.push(line.orderNr); continue; }
      try {
        await api('POST', `/api/v1/orders/${ord.id}/checklist`, { items: line.items });
        matched++;
      } catch (err) {
        missed.push(line.orderNr + ' ('+ (err && err.message ? err.message : 'fout') + ')');
      }
    }

    state.picklistMeta = { bestand: file.name, orders: matched, totaal: orderLines.length, missed: missed.length, tijd: Date.now() };
    try { localStorage.setItem('cockpit_picklist_meta', JSON.stringify(state.picklistMeta)); } catch (e) { /* negeer */ }

    const missedTxt = missed.length ? `; ${missed.length} niet gevonden (${missed.slice(0, 4).join(', ')}${missed.length > 4 ? ', …' : ''})` : '';
    toast(`Picklijst verwerkt: ${matched}/${orderLines.length} orders bijgewerkt${missedTxt}`);
    renderConfig();
  }

  async function probeerAutomatischeConfigJSON() {
    if (state.configBron) return;
    try {
      const res = await fetch('./orderpick-config.json', { cache: 'no-store' });
      if (!res.ok) return;
      const parsed = JSON.parse(await res.text());
      state.config = parsed;
      state.configBron = 'orderpick-config.json (automatisch geladen)';
      await saveConfig();
    } catch (e) {
      /* stil negeren: verwacht bij file:// of als het bestand er nog niet is */
    }
  }

  function downloadConfigJSON() {
    const blob = new Blob([JSON.stringify(state.config, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'orderpick-config.json';
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    toast('Gedownload als orderpick-config.json - plaats dit bestand in de gedeelde map.');
  }

  async function laadConfigVanuitJSON(file) {
    try {
      const parsed = JSON.parse(await file.text());
      state.config = parsed;
      state.configBron = 'JSON-bestand (handmatig geladen)';
      await saveConfig();
      toast('Configuratie geladen vanuit JSON-bestand');
      renderConfig();
    } catch (e) {
      toast('Kon JSON-bestand niet lezen: ' + e.message, true);
    }
  }

  function configCompatibiliteitsWaarschuwing() {
    if (isXlsxBeschikbaar()) return '';
    return `<div class="card config-card" style="border-color:var(--warn-line);background:var(--warn-soft);">
      <h3 style="margin:0 0 4px;font-size:13px;font-weight:650;color:var(--warn);">Excel-library geblokkeerd</h3>
      <p class="muted" style="margin:0;font-size:12.5px;">
        SheetJS (xlsx) wordt van CDN geladen. In sommige previews of offline-omgevingen is dat geblokkeerd ---
        dan werkt upload niet. Open het portaal in een browser met internetverbinding. De opgeslagen
        configuratie blijft daarna ook zonder netwerk werken.
      </p>
    </div>`;
  }

  function renderConfig() {
    const m = state.config.meta || {};
    const sources = [
      { key: 'artikelweergave', label: 'Artikelweergave', desc: 'Artikelcode -> magazijnlocatie', handler: handleArtikelweergave },
      { key: 'klantenlijst',    label: 'Klantenlijst',    desc: "Klantnummer -> groep (Prio's / InControl / Overig)", handler: handleKlantenlijst },
      { key: 'pqRegels',        label: 'Uitsluitingsregels', desc: 'Artikelen en klanten die altijd worden uitgesloten (uit PQ_regels)', handler: handlePQRegels },
      { key: 'excelRegels',     label: 'Kleurregels',     desc: 'Uitzonderingen/aandachtspunten per artikel (uit Excel_regels)', handler: handleExcelRegels },
    ];
    const alleVier = sources.every((s) => !!m[s.key]);
    const metaTxt = (meta) => meta
      ? `${esc(meta.bestand)} · bijgewerkt ${fmtTime(meta.tijd)}`
      : 'nog niet geüupload';

    let html = '';
    if (!isXlsxBeschikbaar()) html += configCompatibiliteitsWaarschuwing();

    html += `<div class="card config-card">
      <h3 class="config-card-title">Excel-bronnen</h3>
      <p class="muted config-card-sub">Upload de vier one-time Excel-bronnen. Elke brons wordt omgezet naar de gedeelde orderpick-config.json.</p>
      <div class="config-section">`;
    for (const s of sources) {
      const meta = m[s.key];
      html += `<div class="config-row">
        <div class="config-row-main">
          <div class="config-row-title">${esc(s.label)}</div>
          <div class="config-row-desc muted">${esc(s.desc)}</div>
          <div class="config-meta muted">${metaTxt(meta)}</div>
        </div>
        <button type="button" class="btn ${meta ? 'btn-ghost' : 'btn-primary'}" data-cfg-src="${s.key}">${meta ? 'Vervangen' : 'Uploaden'}</button>
      </div>`;
    }
    html += `</div></div>`;

    html += `<div class="card config-card">
      <h3 class="config-card-title">Configuratie bundelen</h3>
      <p class="muted config-card-sub">
        Download orderpick-config.json en plaats het in de gedeelde map naast deze portal-pagina — dan wordt
        het bij het openen automatisch geladen, zodat medewerkers elke dag alleen nog de verkoopregels hoeven te importeren.
      </p>
      <div class="config-bundle">
        <button type="button" id="cfg-download" class="btn btn-primary" ${alleVier ? '' : 'disabled'}>⬇ Downloaden als orderpick-config.json</button>
        <button type="button" id="cfg-upload" class="btn btn-ghost">⬆ Configuratie laden vanuit JSON</button>
      </div>
      ${alleVier ? '' : '<p class="muted config-card-sub">Upload eerst alle vier bronnen hierboven voordat je kunt bundelen.</p>'}
      ${state.configBron ? `<p class="muted config-meta">Actieve configuratie geladen via: ${esc(state.configBron)}</p>` : ''}
    </div>`;

    $('#config-content').innerHTML = html;

    const downloadBtn = $('#cfg-download');
    if (downloadBtn) downloadBtn.addEventListener('click', downloadConfigJSON);
    const uploadBtn = $('#cfg-upload');
    if (uploadBtn) uploadBtn.addEventListener('click', () => fileInput('.json', laadConfigVanuitJSON).click());
    $$('[data-cfg-src]').forEach((b) => {
      b.addEventListener('click', () => {
        const s = sources.find((x) => x.key === b.dataset.cfgSrc);
        if (s) fileInput('.xlsx', s.handler).click();
      });
    });
  }

  /* ------------------------------------------------------------ wiring */

  // login
  $('#login-form').addEventListener('submit', submitLogin);
  $('#toggle-password').addEventListener('click', (e) => {
    const input = $('#login-password');
    const show = input.type === 'password';
    input.type = show ? 'text' : 'password';
    e.currentTarget.textContent = show ? 'Hide' : 'Show';
    e.currentTarget.setAttribute('aria-label', show ? 'Hide password' : 'Show password');
    input.focus();
  });

  $('#logout-btn').addEventListener('click', () => logout(true));
  $('#shortcuts-btn').addEventListener('click', openShortcuts);

  // orders toolbar
  $('#new-order-btn').addEventListener('click', openCreateOrder);
  $('#refresh-btn').addEventListener('click', () => {
    loadOrders();
    if (location.hash.startsWith('#/users')) renderUsers(true);
  });

  $('#status-chips').addEventListener('click', (e) => {
    const chip = e.target.closest('.chip');
    if (!chip) return;
    state.filter = chip.dataset.filter;
    renderStatusChips();
    renderTable();
  });

  let searchTimer = null;
  $('#search-input').addEventListener('input', (e) => {
    const v = e.target.value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => { state.search = v; renderTable(); }, 120);
  });

  $('#orders-table thead').addEventListener('click', (e) => {
    const th = e.target.closest('th.sortable');
    if (!th) return;
    const key = th.dataset.sort;
    if (state.sort.key === key) state.sort.dir = state.sort.dir === 'asc' ? 'desc' : 'asc';
    else state.sort = { key, dir: key === 'orderNumber' || key === 'status' ? 'asc' : 'desc' };
    renderTable();
  });

  $('#orders-table tbody').addEventListener('click', (e) => {
    if (e.target.closest('#clear-filters')) {
      state.filter = 'All';
      state.search = '';
      $('#search-input').value = '';
      renderStatusChips();
      renderTable();
      return;
    }
    const tr = e.target.closest('tr[data-id]');
    if (tr) openDetail(Number(tr.dataset.id));
  });

  $('#orders-table tbody').addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const tr = e.target.closest('tr[data-id]');
    if (!tr) return;
    e.preventDefault();
    openDetail(Number(tr.dataset.id));
  });

  // detail panel
  $('#detail-panel').addEventListener('submit', async (e) => {
    e.preventDefault();
    const id = state.selectedId;
    if (e.target.id === 'transition-form') {
      await act('POST', `/api/v1/orders/${id}/transition`, { status: $('#transition-target').value }, 'Status updated');
    } else if (e.target.id === 'hold-form') {
      const reason = $('#hold-reason').value.trim();
      if (!reason) return;
      await act('POST', `/api/v1/orders/${id}/holds`, { reason }, 'Hold placed');
    } else if (e.target.id === 'qc-form') {
      await act('POST', `/api/v1/orders/${id}/qc`, {
        status: $('#qc-status').value,
        notes: $('#qc-notes').value,
      }, 'QC check recorded');
    } else if (e.target.id === 'comment-form') {
      const ta = $('#comment-input');
      const body = ta.value.trim();
      if (!body) return;
      ta.disabled = true;
      const ok = await act('POST', `/api/v1/orders/${id}/comments`, { body }, 'Comment added');
      ta.disabled = false;
      if (ok) {
        ta.value = '';
        await loadComments(id);
      }
    }
  });

  $('#detail-panel').addEventListener('keydown', (e) => {
    if (e.target.id !== 'drawer-scan-input' || e.key !== 'Enter') return;
    e.preventDefault();
    const v = e.target.value.trim();
    if (v) doScanInto(v, $('#drawer-scan-feedback'));
  });

  $('#detail-panel').addEventListener('click', async (e) => {
    if (e.target.closest('#detail-close')) return closeDetail();
    const btn = e.target.closest('button[data-act]');
    if (!btn) return;
    if (btn.dataset.act === 'resolve') {
      await act('POST', `/api/v1/holds/${btn.dataset.id}/resolve`, {}, 'Hold resolved');
    }
  });

  $('#new-user-btn').addEventListener('click', openCreateUser);

  $('#users-table tbody').addEventListener('click', async (e) => {
    const btn = e.target.closest('button[data-act]');
    if (!btn) return;
    const id = btn.dataset.id;
    const target = state.users.find((u) => String(u.id) === String(id)) || {};

    if (btn.dataset.act === 'role') {
      const res = await modal({
        title: 'Change role',
        description: `Update permissions for ${target.username || 'this user'}.`,
        confirmLabel: 'Update role',
        fields: [{ name: 'role', label: 'Role', type: 'select', options: ROLES, value: target.role || 'viewer' }],
      });
      if (!res) return;
      const ok = await act('POST', `/api/v1/users/${id}/role`, { role: res.role }, 'Role updated');
      if (ok) renderUsers(true);

    } else if (btn.dataset.act === 'pwd') {
      const res = await modal({
        title: 'Reset password',
        description: `Set a new password for ${target.username || 'this user'}.`,
        confirmLabel: 'Reset password',
        fields: [{ name: 'password', label: 'New password', type: 'password', required: true, autofocus: true, hint: 'Minimum 8 characters.' }],
      });
      if (!res) return;
      await act('POST', `/api/v1/users/${id}/password`, { password: res.password }, 'Password reset');

    } else if (btn.dataset.act === 'del') {
      const ok = await confirmDialog(
        `Delete ${target.username || 'user'}?`,
        'This removes the account and revokes all of their active tokens immediately. This cannot be undone.',
        'Delete user'
      );
      if (!ok) return;
      const done = await act('DELETE', `/api/v1/users/${id}`, undefined, 'User deleted');
      if (done) renderUsers(true);
    }
  });

  // scanner + attention + notifications
  $('#scan-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const input = $('#scan-input');
    const code = input.value.trim();
    if (code) doScan(code);
    input.focus();
    input.select();
  });

  $('#attention-cards').addEventListener('click', (e) => {
    const card = e.target.closest('.acard[data-id]');
    if (card) goToOrder(Number(card.dataset.id));
  });
  $('#attention-cards').addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const card = e.target.closest('.acard[data-id]');
    if (card) { e.preventDefault(); goToOrder(Number(card.dataset.id)); }
  });

  $('#scan-result').addEventListener('click', (e) => {
    const hit = e.target.closest('.scan-hit[data-id]');
    if (hit) goToOrder(Number(hit.dataset.id));
  });

  const notifPanel = $('#notif-panel');
  $('#bell-btn').addEventListener('click', () => {
    const open = notifPanel.classList.toggle('hidden');
    $('#bell-btn').setAttribute('aria-expanded', String(!open));
    if (!open) loadNotifications(true);
  });
  $('#notif-close').addEventListener('click', () => notifPanel.classList.add('hidden'));
  $('#notif-mark').addEventListener('click', markNotificationsRead);
  notifPanel.addEventListener('click', (e) => {
    const item = e.target.closest('.notif-item[data-id]');
    if (!item) return;
    notifPanel.classList.add('hidden');
    goToOrder(Number(item.dataset.id));
  });
  document.addEventListener('click', (e) => {
    if (notifPanel.classList.contains('hidden')) return;
    if (e.target.closest('#notif-panel') || e.target.closest('#bell-btn')) return;
    notifPanel.classList.add('hidden');
  });

  window.addEventListener('hashchange', route);

  /* ------------------------------------------------------------ keyboard */

  let gPending = false;
  document.addEventListener('keydown', (e) => {
    const typing = /^(INPUT|SELECT|TEXTAREA)$/.test(e.target.tagName) || e.target.isContentEditable;
    const modalOpen = !$('#modal-root').classList.contains('hidden');

    if (e.key === 'Escape' && !modalOpen) {
      if (typing && e.target.id === 'search-input') {
        e.target.value = ''; state.search = ''; renderTable(); e.target.blur();
      } else if (state.selectedId) closeDetail();
      return;
    }

    if (typing || modalOpen || !state.token) return;
    if (e.metaKey || e.ctrlKey || e.altKey) return;

    if (gPending) {
      gPending = false;
      if (e.key === 'o') { location.hash = '#/orders'; return; }
      if (e.key === 'h') { location.hash = '#/home'; return; }
      if (e.key === 'u' && can('users:manage')) { location.hash = '#/users'; return; }
      if (e.key === 'c' && can('config:manage')) { location.hash = '#/config'; return; }
    }

    switch (e.key) {
      case '/':
        e.preventDefault();
        $('#search-input').focus();
        break;
      case 'h':
        e.preventDefault();
        location.hash = '#/home';
        break;
      case 'n':
        if (can('orders:create') && !location.hash.startsWith('#/users')) { e.preventDefault(); openCreateOrder(); }
        else if (can('users:manage') && location.hash.startsWith('#/users')) { e.preventDefault(); openCreateUser(); }
        break;
      case 'r':
        e.preventDefault();
        if (location.hash.startsWith('#/users')) renderUsers(true); else loadOrders();
        break;
      case 'g':
        gPending = true;
        setTimeout(() => { gPending = false; }, 900);
        break;
      case '?':
        e.preventDefault();
        openShortcuts();
        break;
      default:
        break;
    }
  });

  /* ------------------------------------------------------------ boot */

  $('#orders-split').classList.add('no-detail');
  if (state.token && state.user) enterApp();
  else showLogin();
})();
