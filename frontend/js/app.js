'use strict';

  

  function showPane(name) {
    $('#view-home').classList.toggle('hidden', name !== 'home');
    $('#view-orders').classList.toggle('hidden', name !== 'orders');
    $('#view-users').classList.toggle('hidden', name !== 'users');
    $('#view-config').classList.toggle('hidden', name !== 'config');
    $('#view-checklists').classList.toggle('hidden', name !== 'checklists');
    $('#view-products').classList.toggle('hidden', name !== 'products');
    $('#view-manuals').classList.toggle('hidden', name !== 'manuals');
    $$('.nav-link').forEach((a) => {
      const active = a.dataset.nav === name;
      a.classList.toggle('active', active);
      if (active) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
    });
  }

  
  
  
  function parseHash() {
    const h = location.hash || '#/home';
    const [path, query] = h.slice(1).split('?');
    const segs = path.split('/').filter(Boolean);
    const page = segs[0] || 'home';
    const orderId = segs[0] === 'orders' && segs[1] && /^\d+$/.test(segs[1]) ? Number(segs[1]) : null;
    return { page, orderId, params: new URLSearchParams(query || '') };
  }

  function hashFor(page, orderId, params) {
    let out = '#/' + page + (orderId ? '/' + orderId : '');
    const qs = params ? params.toString() : '';
    if (qs) out += '?' + qs;
    return out;
  }

  let applyingFromURL = false;

  function stateParams() {
    const p = new URLSearchParams();
    if (state.filter && state.filter !== 'All') p.set('status', state.filter);
    if (state.search) p.set('q', state.search);
    if (state.sort.key !== 'id' || state.sort.dir !== 'desc') {
      p.set('sort', state.sort.key);
      p.set('dir', state.sort.dir);
    }
    if (state.mine) p.set('mine', '1');
    return p;
  }

  function syncURL() {
    if (applyingFromURL || !state.token) return;
    const cur = parseHash();
    const page = cur.page === 'orders' || cur.page === 'users' || cur.page === 'config' || cur.page === 'checklists' || cur.page === 'products' || cur.page === 'manuals' ? cur.page : 'home';
    const target = page === 'orders'
      ? hashFor('orders', cur.orderId, stateParams())
      : hashFor(page, null, null);
    if (('#' + location.hash.replace(/^#/, '')) !== target) {
      history.replaceState(null, '', target);
    }
  }

  function applyURLToState(params) {
    applyingFromURL = true;
    try {
      const status = params.get('status');
      state.filter = status && STATUS_FILTERS.some((f) => f.value === status) ? status : 'All';
      state.search = params.get('q') || '';
      const sortKey = params.get('sort');
      const dir = params.get('dir') === 'asc' ? 'asc' : 'desc';
      state.sort = sortKey ? { key: sortKey, dir } : { key: 'id', dir: 'desc' };
      state.mine = params.get('mine') === '1';
      const searchInput = $('#search-input');
      if (searchInput && searchInput.value !== state.search) searchInput.value = state.search;
      const mineChip = $('#mine-chip');
      if (mineChip) mineChip.setAttribute('aria-pressed', String(state.mine));
      renderStatusChips();
    } finally {
      applyingFromURL = false;
    }
  }

  function route() {
    if (!state.token) return showLogin();
    const { page, orderId, params } = parseHash();

    if (can('users:manage') && page === 'users') {
      showPane('users');
      renderUsers(true);
      return;
    }
    if (can('config:manage') && page === 'config') {
      showPane('config');
      renderConfig();
      return;
    }
    if (page === 'checklists') {
      showPane('checklists');
      loadChecklistsView();
      startChecklistsPolling();
      return;
    }
    if (page === 'products') {
      showPane('products');
      loadProducts();
      return;
    }
    if (page === 'manuals') {
      showPane('manuals');
      loadManualsView();
      startManualsPolling();
      return;
    }
    if (page === 'orders') {
      showPane('orders');
      applyURLToState(params);
      renderTable();
      if (orderId) {
        if (state.selectedId !== orderId) openDetail(orderId);
      } else if (state.selectedId) {
        closeDetail();
      }
      return;
    }
    if (location.hash === '' || location.hash === '#' || location.hash === '#/') {
      history.replaceState(null, '', '#/home');
    }
    showPane('home');
    loadAttention(true);
    loadNotifications(true);
  }

  

  function connectEvents() {
    if (!state.token || typeof EventSource === 'undefined') return;
    disconnectEvents();
    try {
      const es = new EventSource('/api/v1/events?token=' + encodeURIComponent(state.token));
      state.sse = es;
      es.onopen = () => { state.sseOk = true; updateSyncLabel(); };
      es.onerror = () => { state.sseOk = false; updateSyncLabel(); };
      es.addEventListener('orders', async (e) => {
        let msg = {};
        try { msg = JSON.parse(e.data); } catch {  }
        await loadOrders(true);
        if (location.hash.startsWith('#/home')) loadAttention(true);
        if (msg.orderId && state.selectedId === msg.orderId && !drawerInputActive()) {
          await openDetail(msg.orderId);
        }
      });
      es.addEventListener('checklist', async (e) => {
        let msg = {};
        try { msg = JSON.parse(e.data); } catch {  }
        if (location.hash.startsWith('#/checklists')) await loadChecklistsView(true);
        if (msg.orderId && state.selectedId === msg.orderId) await loadChecklist(msg.orderId, true);
      });
      es.addEventListener('manual', async (e) => {
        let msg = {};
        try { msg = JSON.parse(e.data); } catch {  }
        if (msg.orderId && state.selectedId === msg.orderId) await loadOrderManuals(msg.orderId, true);
        if (location.hash.startsWith('#/manuals')) await loadManualsView(true);
      });
      es.addEventListener('comment', async (e) => {
        let msg = {};
        try { msg = JSON.parse(e.data); } catch {  }
        if (msg.orderId && state.selectedId === msg.orderId) await loadComments(msg.orderId);
      });
      es.addEventListener('notification', () => { loadNotifications(true); });
    } catch {  }
  }

  function disconnectEvents() {
    if (state.sse) {
      try { state.sse.close(); } catch {  }
      state.sse = null;
      state.sseOk = false;
    }
  }

  

  let paletteSel = 0;
  let paletteItems = [];

  function paletteMatches(q) {
    const needle = q.trim().toLowerCase();
    const items = [];
    const pages = [
      { title: t('nav.home'), hash: '#/home', kind: t('palette.pages') },
      { title: t('nav.orders'), hash: '#/orders', kind: t('palette.pages') },
      { title: t('nav.pick'), hash: '#/checklists', kind: t('palette.pages') },
      { title: t('nav.products'), hash: '#/products', kind: t('palette.pages') },
      { title: t('nav.manuals'), hash: '#/manuals', kind: t('palette.pages') },
      { title: t('nav.users'), hash: '#/users', kind: t('palette.pages') },
      { title: t('nav.config'), hash: '#/config', kind: t('palette.pages') },
    ].filter((p) => p.hash !== '#/users' || can('users:manage'))
     .filter((p) => p.hash !== '#/config' || can('config:manage'))
     .filter((p) => p.hash !== '#/checklists' || can('orders:view'))
     .filter((p) => p.hash !== '#/products' || can('orders:view'))
     .filter((p) => p.hash !== '#/manuals' || can('orders:view'));
    const actions = [
      { title: t('act.newOrder'), fn: () => openCreateOrder(), kind: t('palette.actions'), show: can('orders:create') },
      { title: t('act.export'), fn: () => exportCSV(), kind: t('palette.actions'), show: true },
      { title: t('act.refresh'), fn: () => { const p = parseHash().page; if (p === 'checklists') loadChecklistsView(); else if (p === 'users') renderUsers(true); else if (p !== 'config') loadOrders(); }, kind: t('palette.actions'), show: true },
      { title: t('act.theme'), fn: () => { localStorage.setItem('cockpit_theme', (localStorage.getItem('cockpit_theme') || 'system') === 'dark' ? 'light' : 'dark'); applyPrefs(); }, kind: t('palette.actions'), show: true },
      { title: t('act.lang'), fn: () => { localStorage.setItem('cockpit_lang', lang() === 'nl' ? 'en' : 'nl'); location.reload(); }, kind: t('palette.actions'), show: true },
      { title: t('act.markRead'), fn: () => markNotificationsRead(), kind: t('palette.actions'), show: state.unread > 0 },
      { title: t('act.backup'), fn: () => downloadBackup(), kind: t('palette.actions'), show: can('users:manage') },
    ];
    for (const p of pages) if (!needle || p.title.toLowerCase().includes(needle)) items.push(p);
    for (const a of actions) if (a.show && (!needle || a.title.toLowerCase().includes(needle))) items.push(a);
    for (const o of state.orders) {
      if (needle && !(String(o.id).includes(needle) || o.orderNumber.toLowerCase().includes(needle))) continue;
      items.push({ title: o.orderNumber, sub: `#${o.id} · ${statusLabel(o.status)}`, kind: t('palette.orders'), fn: () => goToOrder(o.id) });
      if (items.length > 24) break;
    }
    if (can('users:manage')) {
      for (const u of state.userBriefs) {
        if (needle && !(u.username.toLowerCase().includes(needle) || (u.displayName || '').toLowerCase().includes(needle))) continue;
        items.push({ title: u.username, sub: u.displayName || '', kind: t('palette.users'), fn: () => { location.hash = '#/users'; } });
      }
    }
    return items.slice(0, 24);
  }

  function renderPalette() {
    const q = $('#palette-input').value;
    paletteItems = paletteMatches(q);
    if (paletteSel >= paletteItems.length) paletteSel = Math.max(0, paletteItems.length - 1);
    $('#palette-list').innerHTML = paletteItems.length
      ? paletteItems.map((it, i) => `
          <div class="palette-item${i === paletteSel ? ' active' : ''}" data-idx="${i}" role="option" aria-selected="${i === paletteSel}">
            <span class="pi-title">${esc(it.title)}</span>
            ${it.sub ? `<span class="pi-sub">${esc(it.sub)}</span>` : ''}
            <span class="pi-kind">${esc(it.kind || '')}</span>
          </div>`).join('')
      : `<div class="palette-empty">${esc(t('palette.none'))}</div>`;
  }

  function openPalette() {
    if (!state.token) return;
    $('#palette-root').classList.remove('hidden');
    const inp = $('#palette-input');
    inp.value = '';
    paletteSel = 0;
    renderPalette();
    inp.focus();
  }

  function closePalette() {
    $('#palette-root').classList.add('hidden');
  }

  function runPaletteItem(i) {
    const it = paletteItems[i];
    closePalette();
    if (!it) return;
    if (it.hash) location.hash = it.hash;
    else if (it.fn) it.fn();
  }

  

  
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

  
  $('#new-order-btn').addEventListener('click', openCreateOrder);
  $('#refresh-btn').addEventListener('click', () => {
    if (location.hash.startsWith('#/checklists')) loadChecklistsView();
    else if (location.hash.startsWith('#/users')) renderUsers(true);
    else loadOrders();
  });

  $('#status-chips').addEventListener('click', (e) => {
    const chip = e.target.closest('.chip');
    if (!chip) return;
    state.filter = chip.dataset.filter;
    state.rowLimit = 100;
    renderStatusChips();
    renderTable();
    syncURL();
  });

  let searchTimer = null;
  $('#search-input').addEventListener('input', (e) => {
    const v = e.target.value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => { state.search = v; state.rowLimit = 100; renderTable(); syncURL(); }, 120);
  });

  $('#orders-table thead').addEventListener('click', (e) => {
    const th = e.target.closest('th.sortable');
    if (!th) return;
    const key = th.dataset.sort;
    if (state.sort.key === key) state.sort.dir = state.sort.dir === 'asc' ? 'desc' : 'asc';
    else state.sort = { key, dir: key === 'orderNumber' || key === 'status' ? 'asc' : 'desc' };
    renderTable();
    syncURL();
  });

  $('#orders-table tbody').addEventListener('click', (e) => {
    if (e.target.closest('#clear-filters')) {
      state.filter = 'All';
      state.search = '';
      state.mine = false;
      state.rowLimit = 100;
      $('#search-input').value = '';
      $('#mine-chip').setAttribute('aria-pressed', 'false');
      renderStatusChips();
      renderTable();
      syncURL();
      return;
    }
    const chk = e.target.closest('input[data-sel]');
    if (chk) {
      const id = Number(chk.dataset.sel);
      if (chk.checked) state.sel.add(id); else state.sel.delete(id);
      chk.closest('tr').classList.toggle('selected-row', chk.checked);
      const all = $$('#orders-table tbody input[data-sel]');
      $('#sel-all').checked = all.length > 0 && all.every((c) => c.checked);
      updateBulkBar();
      return;
    }
    const tr = e.target.closest('tr[data-id]');
    if (tr && !e.target.closest('input[data-sel]')) openDetail(Number(tr.dataset.id));
  });

  $('#orders-table tbody').addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    if (e.target.closest('input[data-sel]')) return;
    const tr = e.target.closest('tr[data-id]');
    if (!tr) return;
    e.preventDefault();
    openDetail(Number(tr.dataset.id));
  });

  $('#sel-all').addEventListener('change', (e) => {
    const ids = filteredOrders().slice(0, state.rowLimit).map((o) => o.id);
    if (e.target.checked) ids.forEach((id) => state.sel.add(id));
    else ids.forEach((id) => state.sel.delete(id));
    renderTable();
  });

  $('#mine-chip').addEventListener('click', () => {
    state.mine = !state.mine;
    state.rowLimit = 100;
    $('#mine-chip').setAttribute('aria-pressed', String(state.mine));
    renderStatusChips();
    renderTable();
    syncURL();
  });

  $('#load-more').addEventListener('click', () => {
    state.rowLimit += 100;
    renderTable();
  });

  $('#export-btn').addEventListener('click', exportCSV);
  $('#bulk-apply').addEventListener('click', runBulkTransition);
  $('#bulk-resolve').addEventListener('click', runBulkResolve);
  $('#bulk-clear').addEventListener('click', () => {
    state.sel.clear();
    $('#sel-all').checked = false;
    renderTable();
  });
  $('#backup-btn').addEventListener('click', downloadBackup);
  $('#palette-btn').addEventListener('click', openPalette);

  
  $('#detail-panel').addEventListener('submit', async (e) => {
    e.preventDefault();
    const id = state.selectedId;
    if (e.target.id === 'transition-form') {
      const target = $('#transition-target').value;
      const cur = state.orders.find((o) => o.id === id);
      await act('POST', `/api/v1/orders/${id}/transition`, {
        status: target,
        expectedUpdatedAt: cur ? cur.updatedAt : undefined,
      }, t('toast.moved'), {
        orderId: id,
        applyLocal: { status: target, updatedAt: new Date().toISOString() },
        revertLocal: cur ? { status: cur.status === target ? undefined : cur.status } : undefined,
      });
    } else if (e.target.id === 'hold-form') {
      const reason = $('#hold-reason').value.trim();
      if (!reason) return;
      const cur = state.orders.find((o) => o.id === id);
      await act('POST', `/api/v1/orders/${id}/holds`, {
        reason,
        expectedUpdatedAt: cur ? cur.updatedAt : undefined,
      }, t('toast.holdPlaced'), {
        orderId: id,
        applyLocal: { status: 'Held_' + String(cur ? cur.status : '').replace('Held_', ''), updatedAt: new Date().toISOString() },
      });
    } else if (e.target.id === 'qc-form') {
      await act('POST', `/api/v1/orders/${id}/qc`, {
        status: $('#qc-status').value,
        notes: $('#qc-notes').value,
      }, t('toast.qc'));
    } else if (e.target.id === 'comment-form') {
      const ta = $('#comment-input');
      const body = ta.value.trim();
      if (!body) return;
      ta.disabled = true;
      const ok = await act('POST', `/api/v1/orders/${id}/comments`, { body }, t('toast.commented'));
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
    } else if (btn.dataset.act === 'checklist-toggle') {
      if (await doChecklistToggle(btn) && state.selectedId) await loadChecklist(state.selectedId);
    }
  });

  async function loadDemoData(btn) {
    btn.disabled = true;
    try {
      const d = await api('POST', '/api/v1/checklists/demo', {});
      toast(d.created ? `Test data loaded — ${d.created} pick lists` : 'Test data already present');
      await loadChecklistsView(true);
    } catch (err) {
      toast(err.message, true);
      btn.disabled = false;
    }
  }

  $('#checklists-table tbody').addEventListener('click', (e) => {
    const demoBtn = e.target.closest('button[data-act="cl-load-demo"]');
    if (demoBtn) {
      loadDemoData(demoBtn);
      return;
    }
    const tr = e.target.closest('tr[data-cl]');
    if (!tr) return;
    state.checklistSelId = Number(tr.dataset.cl);
    renderChecklistsList();
    renderChecklistPanel();
    focusPickScan();
  });

  
  $('#checklists-empty').addEventListener('click', (e) => {
    const demoBtn = e.target.closest('button[data-act="cl-load-demo"]');
    if (demoBtn) loadDemoData(demoBtn);
  });

  $('#checklist-panel').addEventListener('click', async (e) => {
    const openBtn = e.target.closest('button[data-act="cl-open-order"]');
    if (openBtn) {
      if (state.checklistSelId) goToOrder(state.checklistSelId);
      return;
    }
    const btn = e.target.closest('button[data-act="checklist-toggle"]');
    if (!btn) return;
    if (await doChecklistToggle(btn)) await loadChecklistsView(true);
    focusPickScan();
  });

  $('#pick-scan-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const input = $('#pick-scan-input');
    const v = input.value;
    input.value = '';
    if (v.trim()) doPickScan(v);
    focusPickScan();
  });

  $('#new-user-btn').addEventListener('click', openCreateUser);

  

  $('#new-product-btn').addEventListener('click', openCreateProduct);

  $('#products-table tbody').addEventListener('click', (e) => {
    const tr = e.target.closest('tr[data-product]');
    if (!tr) return;
    selectProduct(Number(tr.dataset.product));
  });

  $('#product-panel-body').addEventListener('click', async (e) => {
    const btn = e.target.closest('button[data-act]');
    if (!btn) return;
    const pid = state.productSelId;
    const bid = Number(btn.dataset.id);
    const act = btn.dataset.act;

    if (act === 'block-up' || act === 'block-down') {
      btn.disabled = true;
      try {
        await api('POST', `/api/v1/products/${pid}/blocks/${bid}/move`,
          { direction: act === 'block-up' ? 'up' : 'down' });
        await loadProductBlocks(pid, true);
      } catch (err) {
        toast(err.message, true);
        btn.disabled = false;
      }
      return;
    }
    if (act === 'block-del') {
      const ok = await confirmDialog('Delete this block?', 'The block disappears from the template. Manuals already attached to orders keep their copy.', 'Delete block');
      if (!ok) return;
      try {
        await api('DELETE', `/api/v1/products/${pid}/blocks/${bid}`);
        toast('Block deleted');
        await loadProductBlocks(pid, true);
        await loadProducts(true);
      } catch (err) {
        toast(err.message, true);
      }
      return;
    }
    if (act === 'product-edit') {
      const p = state.products.find((x) => x.id === pid);
      if (!p) return;
      const res = await modal({
        title: 'Edit product',
        confirmLabel: 'Save',
        fields: [
          { name: 'code', label: 'Code (artikel)', required: true, value: p.code },
          { name: 'name', label: 'Name', required: true, value: p.name },
          { name: 'description', label: 'Description', type: 'textarea', value: p.description || '' },
        ],
      });
      if (!res) return;
      try {
        await api('POST', `/api/v1/products/${pid}`, {
          code: res.code.trim(), name: res.name.trim(), description: res.description || '',
        });
        toast('Product updated');
        await loadProducts(true);
      } catch (err) {
        toast(err.message, true);
      }
      return;
    }
    if (act === 'product-del') {
      const p = state.products.find((x) => x.id === pid);
      const ok = await confirmDialog(`Delete ${p ? p.code : 'product'}?`,
        'This removes the product and its manual template. Manuals already attached to orders keep their snapshot.', 'Delete product');
      if (!ok) return;
      try {
        await api('DELETE', `/api/v1/products/${pid}`);
        toast('Product deleted');
        state.productSelId = null;
        state.productBlocks = [];
        await loadProducts(true);
        renderProductPanel();
      } catch (err) {
        toast(err.message, true);
      }
    }
  });

  $('#product-panel-body').addEventListener('submit', async (e) => {
    if (e.target.id !== 'block-form') return;
    e.preventDefault();
    const title = $('#block-title').value.trim();
    if (!title) return;
    const body = $('#block-body').value.trim();
    const assignee = $('#block-assignee').value;
    try {
      await api('POST', `/api/v1/products/${state.productSelId}/blocks`, { title, body, assignee });
      toast('Block added');
      await loadProductBlocks(state.productSelId, true);
      await loadProducts(true);
    } catch (err) {
      toast(err.message, true);
    }
  });

  

  $('#manuals-table tbody').addEventListener('click', (e) => {
    const tr = e.target.closest('tr[data-manual-row]');
    if (!tr) return;
    state.manualsSelId = Number(tr.dataset.manualRow);
    renderManualsList();
    renderManualPanel();
  });

  $('#manuals-mine').addEventListener('change', (e) => {
    state.manualsMine = e.target.checked;
    const mine = state.manuals.find((m) => (m.blocks || []).some((b) => b.assignee === state.user.username));
    if (mineOnlyMissing()) {
      state.manualsSelId = mine ? mine.manualId : (state.manuals[0] ? state.manuals[0].manualId : null);
    }
    renderManualsList();
    renderManualPanel();
  });
  function mineOnlyMissing() {
    const m = state.manuals.find((x) => x.manualId === state.manualsSelId);
    return !(m && (m.blocks || []).some((b) => b.assignee === state.user.username));
  }

  $('#manual-panel-body').addEventListener('click', async (e) => {
    const openBtn = e.target.closest('button[data-act="manual-open-order"]');
    if (openBtn) {
      const m = state.manuals.find((x) => x.manualId === state.manualsSelId);
      if (m) goToOrder(m.orderId);
      return;
    }
    const flagBtn = e.target.closest('button[data-act="manual-flag"]');
    if (flagBtn) {
      await doFlagManualBlock(flagBtn);
      return;
    }
    const unflagBtn = e.target.closest('button[data-act="manual-unflag"]');
    if (unflagBtn) {
      await doUnflagManualBlock(unflagBtn);
      return;
    }
    const btn = e.target.closest('button[data-act="manual-answer"]');
    if (btn) await doManualAnswer(btn);
  });

  
  $('#detail-panel').addEventListener('click', async (e) => {
    const ansBtn = e.target.closest('button[data-act="manual-answer"]');
    if (ansBtn) {
      await doManualAnswer(ansBtn);
      return;
    }
    const flagBtn = e.target.closest('button[data-act="manual-flag"]');
    if (flagBtn) {
      await doFlagManualBlock(flagBtn);
      return;
    }
    const unflagBtn = e.target.closest('button[data-act="manual-unflag"]');
    if (unflagBtn) {
      await doUnflagManualBlock(unflagBtn);
      return;
    }
    const logBtn = e.target.closest('button[data-act="manual-log"]');
    if (logBtn) {
      const box = document.querySelector(`#manuals-section .manual-log[data-log="${logBtn.dataset.id}"]`);
      if (box) box.classList.toggle('hidden');
      return;
    }
    const addBtn = e.target.closest('button[data-act="manual-add"]');
    if (addBtn) {
      openAddManual();
      return;
    }
    const rmBtn = e.target.closest('button[data-act="manual-remove"]');
    if (rmBtn) {
      const ok = await confirmDialog('Remove this manual?',
        'The manual and its tick log disappear from this order. The order audit trail keeps the history.', 'Remove manual');
      if (!ok) return;
      try {
        await api('DELETE', `/api/v1/manuals/${rmBtn.dataset.id}`);
        toast('Manual removed');
        await loadOrderManuals(state.selectedId, true);
      } catch (err) {
        toast(err.message, true);
      }
    }
  });

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

  

  let gPending = false;
  document.addEventListener('keydown', (e) => {
    const typing = /^(INPUT|SELECT|TEXTAREA)$/.test(e.target.tagName) || e.target.isContentEditable;
    const modalOpen = !$('#modal-root').classList.contains('hidden');
    const paletteOpen = !$('#palette-root').classList.contains('hidden');

    
    if ((e.ctrlKey || e.metaKey) && (e.key === 'k' || e.key === 'K')) {
      e.preventDefault();
      if (paletteOpen) closePalette();
      else openPalette();
      return;
    }
    if (paletteOpen) {
      if (e.key === 'Escape') { e.preventDefault(); closePalette(); return; }
      if (e.key === 'ArrowDown') { e.preventDefault(); paletteSel = Math.min(paletteSel + 1, paletteItems.length - 1); renderPalette(); return; }
      if (e.key === 'ArrowUp') { e.preventDefault(); paletteSel = Math.max(paletteSel - 1, 0); renderPalette(); return; }
      if (e.key === 'Enter') { e.preventDefault(); runPaletteItem(paletteSel); return; }
      return; 
    }

    if (e.key === 'Escape' && !modalOpen) {
      if (typing && e.target.id === 'search-input') {
        e.target.value = ''; state.search = ''; renderTable(); syncURL(); e.target.blur();
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
      if (e.key === 'p') { location.hash = '#/checklists'; return; }
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
      case 'e':
        e.preventDefault();
        exportCSV();
        break;
      case 'r':
        e.preventDefault();
        if (location.hash.startsWith('#/users')) renderUsers(true);
        else if (location.hash.startsWith('#/checklists')) loadChecklistsView();
        else if (location.hash.startsWith('#/products')) loadProducts();
        else if (location.hash.startsWith('#/manuals')) loadManualsView();
        else loadOrders();
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

  $('#palette-input').addEventListener('input', () => { paletteSel = 0; renderPalette(); });
  $('#palette-list').addEventListener('click', (e) => {
    const item = e.target.closest('.palette-item[data-idx]');
    if (item) runPaletteItem(Number(item.dataset.idx));
  });
  $('#palette-root').addEventListener('click', (e) => {
    if (e.target.id === 'palette-root') closePalette();
  });

  

  applyPrefs();
  $('#orders-split').classList.add('no-detail');
  if (state.token && state.user) enterApp();
  else showLogin();
