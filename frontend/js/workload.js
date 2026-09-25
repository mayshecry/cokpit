'use strict';

/* Workload board — drag orders between people to (re)assign work. */

const Workload = {
  users: [],
  sessions: {},
  dragging: null,

  async load(silent) {
    if (!can('orders:transition')) return;
    const board = $('#workload-board');
    if (!Workload.loaded && board) {
      board.innerHTML = Array.from({ length: 4 }, () => `
        <section class="wl-col">
          <header class="wl-col-head"><span class="sk sk-av"></span><span class="sk sk-line"></span></header>
          <div class="wl-cards"><span class="sk sk-card"></span><span class="sk sk-card"></span><span class="sk sk-card"></span></div>
        </section>`).join('');
    }
    try {
      const [u, w] = await Promise.all([
        api('GET', '/api/v1/users/brief'),
        can('users:manage') ? api('GET', '/api/v1/work/sessions') : Promise.resolve({ sessions: [] }),
      ]);
      if (!state.orders || !state.orders.length) {
        const d = await api('GET', '/api/v1/orders?limit=2000');
        state.orders = d.orders || [];
      }
      Workload.users = (u.users || []).filter((x) => ['operator', 'qc', 'sc'].includes(x.role));
      Workload.sessions = {};
      (w.sessions || []).forEach((s) => { Workload.sessions[s.orderId] = s; });
      Workload.loaded = true;
      Workload.render();
      const note = $('#workload-text');
      if (note) note.textContent = `${state.orders.filter((o) => o.status !== 'Completed').length} open orders`;
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  },

  openOrdersFor(username) {
    return (state.orders || []).filter((o) =>
      o.status !== 'Completed' && (username === '' ? !o.assignee : o.assignee === username));
  },

  render() {
    const board = $('#workload-board');
    if (!board) return;
    const cols = [{ username: '', displayName: 'Unassigned' }, ...Workload.users];
    board.innerHTML = cols.map((col) => {
      const list = Workload.openOrdersFor(col.username);
      const breached = list.filter((o) => o.sla && o.sla.status === 'BREACHED').length;
      const clocked = list.filter((o) => Workload.sessions[o.id]).length;
      return `
      <section class="wl-col" data-user="${esc(col.username)}">
        <header class="wl-col-head">
          <span class="wl-av${col.username ? '' : ' none'}">${col.username ? esc((col.displayName || col.username).slice(0, 1).toUpperCase()) : '—'}</span>
          <div class="wl-col-titles">
            <strong>${esc(col.displayName || col.username || 'Unassigned')}</strong>
            <span class="rel">${list.length} open${breached ? ` · <span class="wl-bad">${breached} breached</span>` : ''}${clocked ? ` · <span class="wl-live">● ${clocked} clocked in</span>` : ''}</span>
          </div>
        </header>
        <div class="wl-cards">
          ${list.length ? list.map((o) => Workload.card(o)).join('') : '<div class="wl-empty">drop orders here</div>'}
        </div>
      </section>`;
    }).join('');
    Workload.bind(board);
  },

  card(o) {
    const sess = Workload.sessions[o.id];
    return `
      <article class="wl-card${sess ? ' live' : ''}" draggable="true" data-order="${o.id}" tabindex="0"
               role="button" aria-label="Open ${esc(o.orderNumber)}" title="${esc(o.orderNumber)} — ${esc(statusLabel(o.status))}">
        <div class="wl-card-top">
          <span class="mono strong">${esc(o.orderNumber)}</span>
          ${(() => { const sla = o.sla ? o.sla.status : 'ON_TIME'; return badge('sla-' + esc(sla), sla.replace('_', ' ')); })()}
        </div>
        <div class="wl-card-mid">
          ${badge('status-' + esc(statusClass(o.status)), statusLabel(o.status))}
          ${o.customerName ? `<span class="rel">${esc(o.customerName)}</span>` : ''}
        </div>
        <div class="wl-card-foot rel">
          <span>${esc(relTime(o.targetCompletionAt))}</span>
          ${sess ? `<span class="wl-timer mono" data-work-timer="${sess.startedAt}">${WorkFlow.fmtElapsed(Date.now() - sess.startedAt)}</span>` : ''}
        </div>
      </article>`;
  },

  bind(board) {
    if (board._wlBound) return;
    board._wlBound = true;

    board.addEventListener('click', (e) => {
      const card = e.target.closest('.wl-card');
      if (!card || Workload.dragging) return;
      if (typeof goToOrder === 'function') goToOrder(Number(card.dataset.order));
    });

    board.addEventListener('dragstart', (e) => {
      const card = e.target.closest('.wl-card');
      if (!card) return;
      Workload.dragging = Number(card.dataset.order);
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', card.dataset.order);
      card.classList.add('dragging');
    });
    board.addEventListener('dragend', () => {
      Workload.dragging = null;
      board.querySelectorAll('.dragging').forEach((el) => el.classList.remove('dragging'));
      board.querySelectorAll('.over').forEach((el) => el.classList.remove('over'));
    });
    board.addEventListener('dragover', (e) => {
      const col = e.target.closest('.wl-col');
      if (!col || !Workload.dragging) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
      col.classList.add('over');
    });
    board.addEventListener('dragleave', (e) => {
      const col = e.target.closest('.wl-col');
      if (col && !col.contains(e.relatedTarget)) col.classList.remove('over');
    });
    board.addEventListener('drop', async (e) => {
      const col = e.target.closest('.wl-col');
      if (!col || !Workload.dragging) return;
      e.preventDefault();
      const orderId = Workload.dragging;
      Workload.dragging = null;
      col.classList.remove('over');
      const user = col.dataset.user;
      const o = (state.orders || []).find((x) => x.id === orderId);
      if (!o || (o.assignee || '') === user) { Workload.render(); return; }
      const prev = o.assignee || '';
      try {
        await api('POST', `/api/v1/orders/${orderId}/assign`, { assignee: user });
        o.assignee = user;
        Workload.render();
        toast(user ? `${o.orderNumber} → @${user}` : `${o.orderNumber} unassigned`, false,
          { label: 'Undo', fn: async () => {
            try {
              await api('POST', `/api/v1/orders/${orderId}/assign`, { assignee: prev });
              o.assignee = prev;
              Workload.render();
              if (typeof renderTable === 'function') renderTable();
              toast('Assignment reverted');
            } catch (err2) { toast(err2.message, true); }
          } });
        if (window.WorkFlow) WorkFlow.refreshMine();
        if (typeof renderTable === 'function') renderTable();
      } catch (err) { toast(err.message, true); Workload.load(true); }
    });

    const refresh = $('#workload-refresh');
    if (refresh) refresh.addEventListener('click', () => Workload.load());
  },
};

window.Workload = Workload;
