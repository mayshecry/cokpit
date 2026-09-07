'use strict';

  

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
    $('#nav-home').classList.toggle('hidden', !can('orders:list'));
    $('#nav-orders').classList.toggle('hidden', !can('orders:list'));
    $('#nav-users').classList.toggle('hidden', !can('users:manage'));
    $('#nav-config').classList.toggle('hidden', !can('config:manage'));
    $('#nav-checklists').classList.toggle('hidden', !can('orders:view'));
    $('#nav-products').classList.toggle('hidden', !can('orders:view'));
    $('#nav-manuals').classList.toggle('hidden', !can('orders:view'));
    updateSINavVisibility();
    $$('[data-perm]').forEach((el) => el.classList.toggle('hidden', !can(el.dataset.perm)));
    applyPrefs();
    loadConfig();
    await probeerAutomatischeConfigJSON();
    if (can('orders:list')) loadUserBriefs();
    initSettingsPop();
    renderStatusChips();
    route();
    if (can('orders:list')) {
      loadOrders();
      connectEvents();
    }
    updateBadges();

    clearInterval(window.__refresh);
    window.__refresh = setInterval(() => {
      if (!state.token) return clearInterval(window.__refresh);
      if (location.hash.startsWith('#/users')) renderUsers(true);
      else if (location.hash.startsWith('#/checklists')) loadChecklistsView(true);
      else if (location.hash.startsWith('#/products')) loadProducts(true);
      else if (location.hash.startsWith('#/manuals')) loadManualsView(true);
      else if (location.hash.startsWith('#/si')) { /* SI view refreshes on demand */ }
      else if (location.hash.startsWith('#/home')) { if (can('orders:list')) loadAttention(true); loadNotifications(true); }
      else if (can('orders:list')) loadOrders(true);
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

