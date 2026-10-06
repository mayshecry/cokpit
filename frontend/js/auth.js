'use strict';

  

  // ---- QR sign-in (pair once + log in once, afterwards: scan) ----
  let qrTicket = null, qrPollTimer = null;

  function escapeHtmlHost(u) {
    try { return new URL(u).origin; } catch { return u.split('/').slice(0, 3).join('/'); }
  }

  // Renders the status line while KEEPING the scanned URL visible under it —
  // and flags the classic "localhost" misconfig, the #1 phone-pairing symptom.
  function setQrState(kind, url, hostHint) {
    const status = $('#qr-login-status');
    if (!status) return;
    if (!url) { const s = status.querySelector('.auth-qr-url'); url = s ? s.textContent : ''; }
    const msg = kind === 'rejected' ? 'Declined on your phone — new code incoming…<br>'
      : kind === 'refreshed' ? 'Code refreshed — scan the new one.<br>'
      : 'Waiting for your phone…<br>';
    const esc = (v) => v.replace(/&/g, '&amp;').replace(/</g, '&lt;');
    status.innerHTML = msg + (url ? '<span class="auth-qr-url">' + esc(url) + '</span>' : '') + (hostHint || '');
  }

  function stopQrFlow() {
    if (qrPollTimer) { clearInterval(qrPollTimer); qrPollTimer = null; }
  }

  async function startQrTicket() {
    const card = $('#qr-login-card');
    const status = $('#qr-login-status');
    try {
      const d = await api('GET', '/api/v1/qr-login');
      qrTicket = { id: d.ticket, started: Date.now(), ttl: (d.expires || 150) * 1000 };
      $('#qr-login-img').src = d.dataUrl;
      card.classList.remove('hidden');
      const hostHint = /\blocalhost\b|127\.0\.0\.1/.test(d.url)
        ? ' ⚠️ this QR points at <b>' + escapeHtmlHost(d.url) + '</b> — a phone on WiFi cannot reach this machine\'s localhost. Set <code>publicUrl</code> in cockpit.json to the server\'s address, then reload.'
        : '';
      setQrState('waiting', d.url, hostHint);
    } catch (e) {
      card.classList.add('hidden');
      qrTicket = null;
    }
  }

  async function pollQrTicket() {
    if (!qrTicket || state.token) return;
    if (Date.now() - qrTicket.started > qrTicket.ttl) { qrTicket = null; startQrTicket(); return; }
    try {
      const d = await api('GET', '/api/v1/qr-login/status?ticket=' + encodeURIComponent(qrTicket.id));
      const status = $('#qr-login-status');
      if (d.status === 'approved') {
        stopQrFlow();
        status.textContent = 'Signed in as ' + (d.user.username || 'you') + '…';
        state.token = d.token;
        state.user = d.user;
        localStorage.setItem('cockpit_token', d.token);
        localStorage.setItem('cockpit_user', JSON.stringify(d.user));
        state.loadingOrders = true;
        enterApp();
      } else if (d.status === 'rejected') {
        setQrState('rejected');
        qrTicket = null; startQrTicket();
      } else if (d.status === 'expired') {
        setQrState('refreshed');
        qrTicket = null; startQrTicket();
      } else {
        setQrState('waiting');
      }
    } catch (e) { /* transient — next tick retries */ }
  }

  function startQrFlow() {
    stopQrFlow();
    if (state.token) return;
    startQrTicket();
    qrPollTimer = setInterval(pollQrTicket, 2000);
  }

  function showLogin() {
    document.body.classList.remove('app-shell');
    $('#view-app').classList.add('hidden');
    $('#view-login').classList.remove('hidden');
    $('#login-username').focus();
    startQrFlow();
  }

  async function enterApp() {
    stopQrFlow();
    document.body.classList.add('app-shell');
    $('#view-login').classList.add('hidden');
    $('#view-app').classList.remove('hidden');
    const u = state.user || {};
    $('#user-name').textContent = u.displayName || u.username || '';
    $('#user-role').textContent = u.role || '';
    $('#user-avatar').textContent = initials(u);
    $('#nav-home').classList.toggle('hidden', !can('orders:list'));
    $('#nav-orders').classList.toggle('hidden', !can('orders:list'));
    $('#nav-users').classList.toggle('hidden', !can('users:list'));
    $('#nav-config').classList.toggle('hidden', !can('config:manage'));
    $('#nav-checklists').classList.toggle('hidden', !can('orders:view'));
    const navWorkload = $('#nav-workload');
    if (navWorkload) navWorkload.classList.toggle('hidden', !can('orders:transition'));
    const navQuality = $('#nav-quality');
    if (navQuality) navQuality.classList.toggle('hidden', !can('qc:submit'));
  const navBench = $('#nav-qcbench');
  if (navBench) navBench.classList.toggle('hidden', !can('qc:submit'));
    $('#nav-products').classList.toggle('hidden', !can('orders:view'));
    $('#nav-manuals').classList.toggle('hidden', !can('orders:view'));
    updateSINavVisibility();
    $$('[data-perm]').forEach((el) => el.classList.toggle('hidden', !can(el.dataset.perm)));
    applyPrefs();
    loadConfig();
    probeerAutomatischeConfigJSON();
    if (can('orders:list')) loadUserBriefs();
    initSettingsPop();
    renderStatusChips();
    const savedHash = localStorage.getItem('cockpit_last_hash:' + ((state.user || {}).username || ''));
    if (savedHash && (!location.hash || location.hash === '#' || location.hash === '#/')) location.hash = savedHash;
    route();
    if (can('orders:list')) {
      loadOrders();
      connectEvents();
      if (window.WorkFlow) WorkFlow.refreshMine();
    }
    updateBadges();

    clearInterval(window.__refresh);
    window.__refresh = setInterval(() => {
      if (!state.token) return clearInterval(window.__refresh);
      if (location.hash.startsWith('#/users')) renderUsers(true);
      else if (location.hash.startsWith('#/checklists')) loadChecklistsView(true);
      else if (location.hash.startsWith('#/products')) loadProducts(true);
      else if (location.hash.startsWith('#/manuals')) loadManualsView(true);
      else if (location.hash.startsWith('#/si')) {  }
      else if (location.hash.startsWith('#/home')) { if (can('orders:list')) loadAttention(true); loadNotifications(true); }
      // SSE already pushes order changes; only poll as a safety net when the
      // event stream is down or silent, so the table never rebuilds for nothing.
      else if (can('orders:list') && (!state.sseOk || Date.now() - (state.lastSseAt || 0) > 120000)) loadOrders(true);
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
      } catch {  }
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
    state.sel = new Set();
    state.mine = false;
    state.userBriefs = [];
    state.si.customers = [];
    state.si.selectedCustomer = null;
    state.si.projects = [];
    state.si.selectedProject = null;
    state.si.sis = [];
    state.si.selectedSI = null;
    state.si.filter = 'All';
    state.si.loading = false;
    disconnectEvents();
    clearInterval(window.__refresh);
    clearInterval(window.__clock);
    clearInterval(window.__bell);
    $('#notif-panel').classList.add('hidden');
    localStorage.removeItem('cockpit_token');
    localStorage.removeItem('cockpit_user');
    if (modalCleanup) modalCleanup();
    closePalette();
    document.title = BASE_TITLE;
    updateBadges();
    location.hash = '';
    showLogin();
  }

