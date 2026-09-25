'use strict';

/* Day plan — QC and above set daily goals per person and assign today's work. */

const Dayplan = {
  day: null,
  goals: {},
  users: [],

  today() {
    return new Date().toISOString().slice(0, 10);
  },

  async load() {
    const host = $('#dayplan');
    if (!host) return;
    Dayplan.day = Dayplan.today();
    const manage = can('qc:submit');
    try {
      const [g, u] = await Promise.all([
        api('GET', '/api/v1/dayplan?day=' + Dayplan.day),
        (!Dayplan.users.length ? api('GET', '/api/v1/users/brief') : Promise.resolve({ users: Dayplan.users })),
      ]);
      Dayplan.users = u.users || Dayplan.users;
      Dayplan.goals = {};
      (g.goals || []).forEach((x) => { Dayplan.goals[x.username] = x; });
      if (!state.orders || !state.orders.length) {
        const d = await api('GET', '/api/v1/orders?limit=2000');
        state.orders = d.orders || [];
      }
      Dayplan.render(manage);
    } catch (err) {
      host.innerHTML = `<p class="muted">Day plan unavailable: ${esc(err.message)}</p>`;
    }
  },

  people() {
    return Dayplan.users.filter((u) => ['operator', 'qc', 'sc'].includes(u.role));
  },

  openCount(username) {
    return (state.orders || []).filter((o) => o.status !== 'Completed' && (o.assignee || '') === username).length;
  },

  render(manage) {
    const host = $('#dayplan');
    if (!host) return;
    const people = Dayplan.people();
    const open = (state.orders || []).filter((o) => o.status !== 'Completed');
    const dateLabel = new Date(Dayplan.day + 'T00:00:00').toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' });
    host.innerHTML = `
      <header class="dp-head">
        <div>
          <h3>Day plan</h3>
          <span class="muted">${esc(dateLabel)}${manage ? ' — set goals, send them out, assign the day' : ' — your goal for today'}</span>
        </div>
        ${manage ? `<span class="dp-legend muted">${people.length} people · ${open.length} open orders</span>` : ''}
      </header>
      <div class="dp-rows">
        ${people.map((u) => {
          const g = Dayplan.goals[u.username] || {};
          return `
          <div class="dp-row" data-user="${esc(u.username)}">
            <span class="wl-av">${esc((u.displayName || u.username).slice(0, 1).toUpperCase())}</span>
            <div class="dp-who">
              <strong>${esc(u.displayName || u.username)}</strong>
              <span class="rel">${Dayplan.openCount(u.username)} open today</span>
            </div>
            ${manage
              ? `<input class="dp-goal" data-user="${esc(u.username)}" maxlength="300" placeholder="Goal for today… e.g. clear all breached ORD-98xx before 16:00" value="${esc(g.goal || '')}">
                 <button type="button" class="btn btn-secondary btn-sm dp-save" data-user="${esc(u.username)}">Save</button>
                 <button type="button" class="btn btn-primary btn-sm dp-send" data-user="${esc(u.username)}" ${(g.goal || '').trim() ? '' : 'disabled'}>Send goal</button>
                 <span class="dp-sent rel">${g.sentAt ? 'sent ' + esc(relTime(new Date(g.sentAt).toISOString())) : ''}</span>`
              : `<span class="dp-goal-text${(g.goal || '') ? '' : ' muted'}">${esc(g.goal || 'No goal set yet today.')}</span>`}
          </div>`;
        }).join('')}
      </div>
      ${manage ? `
      <div class="dp-assign">
        <span class="dp-assign-label">Assign for today</span>
        <select id="dp-order" aria-label="Order to assign">
          <option value="">pick an order…</option>
          ${open.sort((a, b) => a.id - b.id).map((o) => `<option value="${o.id}">${esc(o.orderNumber)} · ${esc(o.customerName || '—')} · ${esc(statusLabel(o.status))}${o.assignee ? ' · @' + esc(o.assignee) : ''}</option>`).join('')}
        </select>
        <select id="dp-user" aria-label="Assign to">
          ${people.map((u) => `<option value="${esc(u.username)}">${esc(u.displayName || u.username)}</option>`).join('')}
        </select>
        <button type="button" class="btn btn-primary btn-sm" id="dp-assign-btn">Assign</button>
      </div>` : ''}`;
    Dayplan.bind(host, manage);
  },

  bind(host, manage) {
    if (host._dpBound) return;
    host._dpBound = true;
    host.addEventListener('input', (e) => {
      if (!e.target.classList.contains('dp-goal')) return;
      const btn = host.querySelector(`.dp-send[data-user="${e.target.dataset.user}"]`);
      if (btn) btn.disabled = !e.target.value.trim();
    });
    host.addEventListener('click', async (e) => {
      const save = e.target.closest('.dp-save');
      const send = e.target.closest('.dp-send');
      const assign = e.target.closest('#dp-assign-btn');
      if (save) {
        const user = save.dataset.user;
        const val = host.querySelector(`.dp-goal[data-user="${user}"]`).value;
        try {
          const d = await api('PUT', '/api/v1/dayplan/goal', { day: Dayplan.day, username: user, goal: val });
          Dayplan.goals[user] = d.goal;
          toast('Goal saved for @' + user);
          Dayplan.render(true);
        } catch (err) { toast(err.message, true); }
        return;
      }
      if (send) {
        const user = send.dataset.user;
        try {
          await api('POST', '/api/v1/dayplan/send', { day: Dayplan.day, username: user });
          toast('Goal sent to @' + user + '’s bell');
          Dayplan.load();
        } catch (err) { toast(err.message, true); }
        return;
      }
      if (assign) {
        const orderId = Number($('#dp-order', host).value);
        const user = $('#dp-user', host).value;
        if (!orderId) { toast('Pick an order first', true); return; }
        if (typeof assignOrder === 'function') assignOrder(orderId, user);
        else {
          try {
            await api('POST', `/api/v1/orders/${orderId}/assign`, { assignee: user });
            toast('Assigned');
          } catch (err) { toast(err.message, true); }
        }
        Dayplan.load();
        if (window.Workload) Workload.load(true);
      }
    });
  },
};

window.Dayplan = Dayplan;
