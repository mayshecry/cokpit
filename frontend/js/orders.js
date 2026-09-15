'use strict';

  

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
          ${c.assignee ? `<span class="rel" title="${esc(t('detail.assignee'))}">@${esc(c.assignee)}</span>` : ''}
        </div>
        <div class="acard-foot">
          <span>Target ${fmtTime(c.targetAt)}<span class="sub">${esc(relTime(c.targetAt))}</span></span>
          <span class="muted">Updated ${esc(relTime(c.updatedAt))}</span>
        </div>
        ${quickCardActions(c)}
      </article>`).join('');
    bindAttentionCards();
  }

  function goToOrder(id) {
    const cur = parseHash();
    const params = cur.page === 'orders' ? cur.params : stateParams();
    const target = hashFor('orders', id, params);
    if (location.hash === target) {
      if (state.selectedId !== id) openDetail(id);
    } else {
      location.hash = target;
    }
  }

  // One-click "move to the next state" on the dashboard attention cards.
  function quickCardActions(c) {
    if (!can('orders:transition')) return '';
    const next = nextStates(c.status);
    if (!next.length) return '';
    return `<div class="acard-actions">
      <button type="button" class="btn btn-primary btn-sm" data-quick-next="${c.orderId}" data-status="${esc(next[0])}">
        ${esc(statusLabel(next[0]))} <span aria-hidden="true">→</span>
      </button>
    </div>`;
  }

  function bindAttentionCards() {
    const grid = $('#attention-cards');
    if (!grid || grid._boundAttention) return;
    grid._boundAttention = true;
    grid.addEventListener('click', async (e) => {
      const nextBtn = e.target.closest('[data-quick-next]');
      if (nextBtn) {
        e.stopPropagation();
        const status = nextBtn.dataset.status;
        nextBtn.disabled = true;
        try {
          await doQuickTransition(Number(nextBtn.dataset.quickNext), status);
          await loadAttention(true);
        } catch { /* act() already toasts */ }
        finally {
          nextBtn.disabled = false;
        }
        return;
      }
      const card = e.target.closest('.acard[data-id]');
      if (card) goToOrder(Number(card.dataset.id));
    });
  }

  // Used by the inline "quick transition" selects in the orders table and by
  // the one-click buttons on the dashboard cards.
  async function doQuickTransition(id, status) {
    const cur = state.orders.find((o) => o.id === id) || state.detail;
    const body = { status };
    if (cur && cur.updatedAt) body.expectedUpdatedAt = cur.updatedAt;
    await act('POST', `/api/v1/orders/${id}/transition`, body, t('toast.moved'), {
      orderId: id,
      applyLocal: { status, updatedAt: new Date().toISOString() },
      revertLocal: cur ? { status: cur.status, updatedAt: cur.updatedAt } : undefined,
    });
  }

  

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

  

  async function loadOrders(silent) {
    try {
      const d = await api('GET', '/api/v1/orders?limit=2000');
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
    const mode = state.sseOk ? ' · push' : '';
    text.textContent = secs < 10 ? `Live${mode} · just synced` : `Live${mode} · synced ${secs}s ago`;
    note.classList.toggle('stale', secs > 60);
    const homeNote = $('#home-refresh-text');
    if (homeNote && location.hash.startsWith('#/home') && secs < 10) {
      homeNote.textContent = state.sseOk ? 'Live · push' : 'Live · just synced';
    }
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
      `<div class="stat-divider">${esc(t('stat.pipeline'))}</div>`,
      card(c.All, 'accent', t('stat.total')),
      card(c.Received, 'info', t('chip.received')),
      card(c.Processing, 'warn', t('chip.processing')),
      card(c.QC_Review, 'accent', t('chip.qc')),
      card(c.Completed, 'good', t('chip.completed')),
      card(c.On_Hold, 'hold', t('chip.hold')),
      `<div class="stat-divider">${esc(t('stat.service'))}</div>`,
      card(onTime, 'good', t('stat.onTime')),
      card(warning, 'warn', t('stat.risk')),
      card(breached, 'bad', t('stat.breached')),
    ].join('');

    const sub = $('#orders-subtitle');
    if (sub) {
      sub.textContent = breached
        ? t('orders.subtitle.bad', { n: breached, s: breached === 1 ? '' : 's' })
        : t('orders.subtitle.ok');
    }
  }

  function renderStatusChips() {
    const c = counts();
    const chipLabels = {
      All: t('chip.all'), Received: t('chip.received'), Processing: t('chip.processing'),
      QC_Review: t('chip.qc'), Completed: t('chip.completed'), On_Hold: t('chip.hold'),
    };
    $('#status-chips').innerHTML = STATUS_FILTERS.map((f) => `
      <button type="button" role="tab" aria-selected="${state.filter === f.value}"
              class="chip${state.filter === f.value ? ' active' : ''}" data-filter="${f.value}">
        ${esc(chipLabels[f.value] || f.label)}<span class="cnt">${c[f.value] ?? 0}</span>
      </button>`).join('');
    const mine = $('#mine-chip');
    if (mine) mine.classList.toggle('active', !!state.mine);
  }

  function filteredOrders() {
    const q = state.search.trim().toLowerCase();
    const rows = state.orders.filter((o) => {
      if (state.filter !== 'All' && !(o.status === state.filter || (state.filter === 'On_Hold' && isHeld(o)))) return false;
      if (state.mine && o.assignee !== (state.user && state.user.username)) return false;
      if (q && !(
        String(o.id).includes(q) ||
        String(o.orderNumber).toLowerCase().includes(q) ||
        String(o.assignee || '').toLowerCase().includes(q) ||
        String(o.customerName || '').toLowerCase().includes(q) ||
        String(o.debitNumber || '').toLowerCase().includes(q) ||
        String(o.device || '').toLowerCase().includes(q)
      )) return false;
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

  // Inline "move to next state" control rendered in every orders table row.
  function quickSelectHtml(o) {
    if (!can('orders:transition')) return '';
    const next = nextStates(o.status);
    if (!next.length) return '<span class="muted quick-dash">—</span>';
    return `<select class="row-quick" data-id="${o.id}" aria-label="${esc(t('orders.quickTitle'))}" title="${esc(t('orders.quick'))}">
      <option value="">${esc(t('orders.quick'))}</option>
      ${next.map((s) => `<option value="${esc(s)}">${esc(statusLabel(s))}</option>`).join('')}
    </select>`;
  }

  function renderTable() {
    const tbody = $('#orders-table tbody');
    const countEl = $('#row-count');
    const showQuick = can('orders:transition');
    const qHead = $('#orders-table th.quick-col');
    if (qHead) qHead.classList.toggle('hidden', !showQuick);

    $$('#orders-table th.sortable').forEach((th) => {
      const on = th.dataset.sort === state.sort.key;
      th.classList.toggle('sorted', on);
      th.classList.toggle('asc', on && state.sort.dir === 'asc');
      th.classList.toggle('desc', on && state.sort.dir === 'desc');
      th.setAttribute('aria-sort', on ? (state.sort.dir === 'asc' ? 'ascending' : 'descending') : 'none');
    });

    if (state.loadingOrders) {
      tbody.innerHTML = skeletonRows(6, 10);
      countEl.textContent = '';
      $('#load-more').classList.add('hidden');
      return;
    }

    const all = filteredOrders();
    const rows = all.slice(0, state.rowLimit);

    if (!rows.length) {
      const filtering = state.search || state.filter !== 'All' || state.mine;
      tbody.innerHTML = `<tr><td colspan="10">
        <div class="empty">
          <div class="empty-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/></svg></div>
          <h3>${filtering ? esc(t('orders.empty.filtered')) : esc(t('orders.empty.none'))}</h3>
          <p>${filtering ? esc(t('orders.empty.filteredSub')) : esc(t('orders.empty.noneSub'))}</p>
          ${filtering ? `<button type="button" class="btn btn-secondary" id="clear-filters">${esc(t('btn.clear'))}</button>` : ''}
        </div></td></tr>`;
      countEl.textContent = state.orders.length ? `0 ${t('orders.of', { n: 0, m: state.orders.length })}` : '';
      $('#load-more').classList.add('hidden');
      updateBulkBar();
      return;
    }

    tbody.innerHTML = rows.map((o) => {
      const sla = slaOf(o);
      const checked = state.sel.has(o.id);
      return `
      <tr class="row${state.selectedId === o.id ? ' selected' : ''}${checked ? ' selected-row' : ''}" data-id="${o.id}" tabindex="0">
        <td class="chk-cell"><input type="checkbox" data-sel="${o.id}" aria-label="Select order ${esc(o.orderNumber)}"${checked ? ' checked' : ''}></td>
        <td class="mono">${o.id}</td>
        <td class="strong">${esc(o.orderNumber)}</td>
        <td>${badge('status-' + esc(statusClass(o.status)), statusLabel(o.status))}</td>
        <td class="clip">${esc(o.customerName || '—')}</td>
        <td>${o.assignee ? '@' + esc(o.assignee) : '<span class="muted">—</span>'}</td>
        <td class="time">${fmtTime(o.targetCompletionAt)}<span class="rel">${esc(relTime(o.targetCompletionAt))}</span></td>
        <td>${badge('sla-' + sla, sla.replace('_', ' '))}</td>
        <td class="time">${fmtTime(o.createdAt)}</td>
        <td class="quick-cell">${quickSelectHtml(o)}</td>
      </tr>`;
    }).join('');

    countEl.textContent = all.length === state.orders.length
      ? t(all.length === 1 ? 'orders.total1' : 'orders.total', { n: all.length })
      : t('orders.of', { n: all.length, m: state.orders.length });

    const more = all.length - rows.length;
    const moreBtn = $('#load-more');
    if (more > 0) {
      moreBtn.textContent = `${t('loadMore')} (${more})`;
      moreBtn.classList.remove('hidden');
    } else {
      moreBtn.classList.add('hidden');
    }
    updateBulkBar();
  }

  

  async function act(method, path, body, okMsg, opts = {}) {
    const { orderId, applyLocal, revertLocal, quiet } = opts;
    
    if (orderId && applyLocal) {
      const row = state.orders.find((o) => o.id === orderId);
      if (row) Object.assign(row, applyLocal);
      if (state.detail && state.detail.id === orderId) Object.assign(state.detail, applyLocal);
      renderTable();
      if (state.selectedId === orderId && !drawerInputActive()) renderDetail();
    }
    try {
      await api(method, path, body);
      if (!quiet) toast(okMsg || 'Done');
      await loadOrders(true);
      if (state.selectedId) await openDetail(state.selectedId);
      return true;
    } catch (err) {
      if (orderId && revertLocal) {
        const row = state.orders.find((o) => o.id === orderId);
        if (row) Object.assign(row, revertLocal);
        if (state.detail && state.detail.id === orderId) Object.assign(state.detail, revertLocal);
        renderTable();
        if (state.selectedId === orderId && !drawerInputActive()) renderDetail();
      }
      toast(err.message, true, { label: t('toast.retry'), fn: () => act(method, path, body, okMsg, { ...opts, applyLocal: undefined, revertLocal: undefined }) });
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
    try {
      await api('POST', '/api/v1/orders', body);
      toast(t('toast.created'));
      await loadOrders(true);
    } catch (err) {
      const existing = err.data && err.data.error && err.data.error.extra && err.data.error.extra.existingOrder;
      if (err.status === 409 && existing) {
        
        await modal({
          title: 'Order number already in use',
          description: `${existing.orderNumber} already exists (status: ${statusLabel(existing.status)}, #${existing.id}).`,
          confirmLabel: 'Open existing order',
          cancelLabel: 'Close',
          hideConfirm: false,
        });
        goToOrder(existing.id);
      } else {
        toast(err.message, true, {
          label: t('toast.retry'),
          fn: () => { openCreateOrder(); },
        });
      }
    }
  }

  

  function csvCell(v) {
    const s = String(v ?? '');
    return /[",\n;]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  }

  function exportCSV() {
    const rows = filteredOrders();
    if (!rows.length) { toast('Nothing to export with the current filters', true); return; }
    const head = ['id', 'orderNumber', 'status', 'targetCompletionAt', 'sla', 'assignee', 'createdAt'];
    const lines = [head.join(',')];
    for (const o of rows) {
      lines.push([
        o.id, o.orderNumber, statusLabel(o.status), o.targetCompletionAt,
        slaOf(o), o.assignee || '', o.createdAt,
      ].map(csvCell).join(','));
    }
    const blob = new Blob(['\uFEFF' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'cockpit-orders-' + new Date().toISOString().slice(0, 10) + '.csv';
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    toast(`Exported ${rows.length} orders to CSV`);
  }

  

  function updateBulkBar() {
    const bar = $('#bulk-bar');
    if (!bar) return;
    const n = state.sel.size;
    bar.classList.toggle('hidden', n === 0);
    if (!n) return;
    $('#bulk-count').textContent = t('bulk.selected', { n });

    
    const selOrders = state.orders.filter((o) => state.sel.has(o.id));
    let options = null;
    for (const o of selOrders) {
      const next = nextStates(o.status);
      options = options === null ? next : options.filter((s) => next.includes(s));
    }
    const sel = $('#bulk-status');
    const opts = options || [];
    sel.innerHTML = opts.length
      ? opts.map((s) => `<option value="${s}">${statusLabel(s)}</option>`).join('')
      : `<option value="">—</option>`;
    sel.disabled = !opts.length;
    $('#bulk-apply').disabled = !opts.length;
    $('#bulk-progress').textContent = '';
  }

  function bulkReport(results) {
    const ok = results.filter((r) => r.ok).length;
    const failed = results.filter((r) => !r.ok);
    let msg = `${ok}/${results.length} done`;
    if (failed.length) {
      const byCode = {};
      for (const f of failed) byCode[f.code || 'error'] = (byCode[f.code || 'error'] || 0) + 1;
      msg += ' — skipped: ' + Object.entries(byCode).map(([c, n]) => `${c} ×${n}`).join(', ');
    }
    return msg;
  }

  async function runBulkTransition() {
    const status = $('#bulk-status').value;
    if (!status || !state.sel.size) return;
    $('#bulk-apply').disabled = true;
    $('#bulk-progress').textContent = '…';
    try {
      const d = await api('POST', '/api/v1/orders/bulk-transition', { ids: [...state.sel], status });
      toast(bulkReport(d.results || []));
      state.sel.clear();
      $('#sel-all').checked = false;
      await loadOrders(true);
    } catch (err) {
      toast(err.message, true);
      $('#bulk-apply').disabled = false;
      $('#bulk-progress').textContent = '';
    }
  }

  async function runBulkResolve() {
    if (!state.sel.size) return;
    $('#bulk-resolve').disabled = true;
    $('#bulk-progress').textContent = '…';
    try {
      const d = await api('POST', '/api/v1/holds/bulk-resolve', { orderIds: [...state.sel] });
      toast(bulkReport(d.results || []));
      state.sel.clear();
      $('#sel-all').checked = false;
      await loadOrders(true);
    } catch (err) {
      toast(err.message, true);
      $('#bulk-resolve').disabled = false;
      $('#bulk-progress').textContent = '';
    }
  }

