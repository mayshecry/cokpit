'use strict';

                                                                      
                                                      
const CAPA = {
  actions: [],
  summary: {},
  status: 'ACTIVE',
  mine: false,
  overdue: false,
  bound: false,
  loadSeq: 0,

  labels: {
    OPEN: 'Open',
    IN_PROGRESS: 'In progress',
    PENDING_VERIFICATION: 'Needs verification',
    CLOSED: 'Closed',
  },

  badge(status) {
    const cls = {
      OPEN: 'info', IN_PROGRESS: 'warn',
      PENDING_VERIFICATION: 'accent', CLOSED: 'good',
    }[status] || 'neutral';
    return badge(cls, CAPA.labels[status] || status);
  },

  canEdit(action) {
    if (!action || action.status === 'CLOSED') return false;
    return can('qc:submit') || (can('orders:transition') && action.owner === (state.user || {}).username);
  },

  bind() {
    if (CAPA.bound) return;
    CAPA.bound = true;
    $('#capa-new').addEventListener('click', () => CAPA.create());
    $('#capa-refresh').addEventListener('click', () => CAPA.load());
    $('#capa-status-filters').addEventListener('click', (e) => {
      const btn = e.target.closest('[data-capa-status]');
      if (!btn) return;
      CAPA.status = btn.dataset.capaStatus;
      $$('#capa-status-filters .chip').forEach((el) => el.classList.toggle('active', el === btn));
      CAPA.load();
    });
    $('#capa-mine').addEventListener('click', (e) => {
      CAPA.mine = !CAPA.mine;
      e.currentTarget.classList.toggle('active', CAPA.mine);
      e.currentTarget.setAttribute('aria-pressed', String(CAPA.mine));
      CAPA.load();
    });
    $('#capa-overdue').addEventListener('click', (e) => {
      CAPA.overdue = !CAPA.overdue;
      e.currentTarget.classList.toggle('active', CAPA.overdue);
      e.currentTarget.setAttribute('aria-pressed', String(CAPA.overdue));
      CAPA.load();
    });
    $('#capa-table tbody').addEventListener('click', (e) => {
      const btn = e.target.closest('button[data-capa-id]');
      if (!btn) return;
      const id = Number(btn.dataset.capaId);
      if (btn.dataset.capaAction === 'view') CAPA.detail(id);
      else if (btn.dataset.capaAction === 'edit') CAPA.edit(id);
      else if (btn.dataset.capaAction === 'verify') CAPA.verify(id);
    });
  },

  async load() {
    CAPA.bind();
    const seq = ++CAPA.loadSeq;
    const params = new URLSearchParams();
    if (!['ACTIVE', 'ALL'].includes(CAPA.status)) params.set('status', CAPA.status);
    if (CAPA.mine) params.set('owner', 'me');
    if (CAPA.overdue) params.set('overdue', 'true');
    try {
      const d = await api('GET', '/api/v1/capa' + (params.size ? '?' + params : ''));
      if (seq !== CAPA.loadSeq) return;
      CAPA.actions = (d.actions || []).filter((a) => CAPA.status !== 'ACTIVE' || a.status !== 'CLOSED');
      CAPA.summary = d.summary || {};
      CAPA.render();
    } catch (err) {
      if (seq !== CAPA.loadSeq) return;
      $('#capa-table tbody').innerHTML = `<tr><td colspan="8"><p class="form-error">${esc(err.message)}</p></td></tr>`;
    }
  },

  render() {
    $$('#capa-status-filters .chip').forEach((el) => el.classList.toggle('active', el.dataset.capaStatus === CAPA.status));
    const s = CAPA.summary;
    const card = (value, cls, label) => `<div class="stat ${cls}"><span class="num">${Number(value) || 0}</span><span class="lbl">${label}</span></div>`;
    $('#capa-stats').innerHTML = [
      card(s.open, 'info', 'Open'),
      card(s.inProgress, 'warn', 'In progress'),
      card(s.pendingVerification, 'accent', 'Needs verification'),
      card(s.overdue, 'bad', 'Overdue'),
      card(s.closed, 'good', 'Closed'),
    ].join('');

    const rows = CAPA.actions.map((a) => {
      const buttons = [`<button type="button" class="btn btn-ghost btn-sm" data-capa-action="view" data-capa-id="${a.id}">View</button>`];
      if (CAPA.canEdit(a)) buttons.push(`<button type="button" class="btn btn-ghost btn-sm" data-capa-action="edit" data-capa-id="${a.id}">Update</button>`);
      if (can('qc:submit') && a.status === 'PENDING_VERIFICATION') {
        buttons.push(`<button type="button" class="btn btn-primary btn-sm" data-capa-action="verify" data-capa-id="${a.id}">Verify</button>`);
      }
      return `<tr>
        <td class="mono num">CAPA-${a.id}</td>
        <td><strong>${esc(a.title)}</strong>${a.failReason ? `<br><span class="muted">${esc(a.failReason)}</span>` : ''}</td>
        <td><a href="#/orders/${a.orderId}" class="mono">${esc(a.orderNumber)}</a></td>
        <td>${CAPA.badge(a.status)}${a.overdue ? ' ' + badge('bad', 'Overdue') : ''}</td>
        <td>@${esc(a.owner)}</td>
        <td class="time">${esc(fmtCell(a.dueAt))}</td>
        <td class="capa-root">${esc(a.rootCause || '—')}</td>
        <td class="actions"><div class="row-actions">${buttons.join('')}</div></td>
      </tr>`;
    }).join('');
    $('#capa-table tbody').innerHTML = rows || `<tr><td colspan="8"><div class="empty"><h3>No corrective actions</h3><p>Nothing matches the current filters.</p></div></td></tr>`;
    $('#capa-count').textContent = `${CAPA.actions.length} action${CAPA.actions.length === 1 ? '' : 's'}`;
  },

  async prerequisites() {
    if (!state.userBriefs || !state.userBriefs.length) await loadUserBriefs();
    if (!state.orders || !state.orders.length) await loadOrders();
    return {
      users: (state.userBriefs || []).filter((u) => ['operator', 'qc', 'sc', 'admin'].includes(u.role)),
      orders: state.orders || [],
    };
  },

  async create() {
    const { users, orders } = await CAPA.prerequisites();
    if (!orders.length) { toast('Create an order before opening a corrective action.', true); return; }
    if (!users.length) { toast('No eligible CAPA owners found.', true); return; }
    const due = new Date(Date.now() + 3 * 86400000);
    due.setMinutes(due.getMinutes() - due.getTimezoneOffset());
    const res = await modal({
      title: 'New corrective action',
      description: 'Link the action to an order and assign an owner. QC can verify it after the owner submits a resolution.',
      confirmLabel: 'Create action', wide: true,
      fields: [
        { name: 'title', label: 'Title', required: true, autofocus: true, placeholder: 'Prevent recurring firmware mismatch' },
        { name: 'orderId', label: 'Order', type: 'select', required: true, options: orders.map((o) => ({ value: String(o.id), label: `${o.orderNumber} (${o.status})` })) },
        { name: 'qcCheckId', label: 'Failed QC check ID (optional)', type: 'number', placeholder: 'Link a specific FAIL check' },
        { name: 'owner', label: 'Owner', type: 'select', required: true, options: users.map((u) => ({ value: u.username, label: `${u.displayName || u.username} (@${u.username})` })) },
        { name: 'dueAt', label: 'Due date', type: 'datetime-local', required: true, value: due.toISOString().slice(0, 16) },
        { name: 'failReason', label: 'Failure / defect', placeholder: 'e.g. Firmware not at baseline' },
        { name: 'rootCause', label: 'Root cause', type: 'textarea', placeholder: 'Can be completed during the investigation' },
        { name: 'actionPlan', label: 'Corrective action plan', type: 'textarea' },
        { name: 'resolution', label: 'Resolution', type: 'textarea' },
      ],
    });
    if (!res) return;
    const body = {
      orderId: Number(res.orderId), title: res.title.trim(), owner: res.owner,
      dueAt: new Date(res.dueAt).toISOString(), failReason: res.failReason.trim(),
      rootCause: res.rootCause.trim(), actionPlan: res.actionPlan.trim(), resolution: res.resolution.trim(),
    };
    if (Number(res.qcCheckId) > 0) body.qcCheckId = Number(res.qcCheckId);
    try {
      await api('POST', '/api/v1/capa', body);
      toast('Corrective action created');
      CAPA.status = 'ACTIVE';
      await CAPA.load();
    } catch (err) { toast(err.message, true); }
  },

  async edit(id) {
    const a = CAPA.actions.find((item) => item.id === id);
    if (!a || !CAPA.canEdit(a)) return;
    const isQC = can('qc:submit');
    const fields = [];
    if (isQC) {
      const { users } = await CAPA.prerequisites();
      const due = new Date(a.dueAt);
      due.setMinutes(due.getMinutes() - due.getTimezoneOffset());
      const ownerOptions = users.map((u) => ({ value: u.username, label: `${u.displayName || u.username} (@${u.username})` }));
      if (!ownerOptions.some((u) => u.value === a.owner)) ownerOptions.push({ value: a.owner, label: '@' + a.owner });
      fields.push(
        { name: 'title', label: 'Title', required: true, value: a.title },
        { name: 'owner', label: 'Owner', type: 'select', required: true, value: a.owner,
          options: ownerOptions },
        { name: 'dueAt', label: 'Due date', type: 'datetime-local', required: true, value: due.toISOString().slice(0, 16) },
        { name: 'failReason', label: 'Failure / defect', value: a.failReason || '' },
      );
    }
    const transitions = {
      OPEN: ['OPEN', 'IN_PROGRESS', 'PENDING_VERIFICATION'],
      IN_PROGRESS: ['IN_PROGRESS', 'OPEN', 'PENDING_VERIFICATION'],
      PENDING_VERIFICATION: ['PENDING_VERIFICATION', 'IN_PROGRESS'],
    }[a.status] || [a.status];
    fields.push(
      { name: 'status', label: 'Status', type: 'select', value: a.status,
        options: transitions.map((v) => ({ value: v, label: CAPA.labels[v] })),
        hint: 'Root cause, action plan and resolution are required before verification.' },
      { name: 'rootCause', label: 'Root cause', type: 'textarea', value: a.rootCause || '' },
      { name: 'actionPlan', label: 'Corrective action plan', type: 'textarea', value: a.actionPlan || '' },
      { name: 'resolution', label: 'Resolution', type: 'textarea', value: a.resolution || '' },
    );
    const res = await modal({ title: `Update CAPA-${id}`, description: a.orderNumber, confirmLabel: 'Save update', wide: true, fields });
    if (!res) return;
    const body = {
      status: res.status, rootCause: res.rootCause.trim(),
      actionPlan: res.actionPlan.trim(), resolution: res.resolution.trim(),
    };
    if (isQC) {
      Object.assign(body, {
        title: res.title.trim(), owner: res.owner, failReason: res.failReason.trim(),
        dueAt: new Date(res.dueAt).toISOString(),
      });
    }
    try {
      await api('POST', `/api/v1/capa/${id}`, body);
      toast(body.status === 'PENDING_VERIFICATION' && a.status !== 'PENDING_VERIFICATION' ? 'Submitted for verification' : 'Corrective action updated');
      await CAPA.load();
    } catch (err) { toast(err.message, true); }
  },

  async verify(id) {
    const a = CAPA.actions.find((item) => item.id === id);
    if (!a || !can('qc:submit')) return;
    const res = await modal({
      title: `Verify CAPA-${id}`,
      description: `Confirm that “${a.title}” is effective. This closes the action and records your verification.`,
      confirmLabel: 'Verify and close', wide: true,
      bodyHtml: `<div class="capa-verify-summary"><strong>Resolution</strong><p>${esc(a.resolution || '—')}</p></div>`,
      fields: [{ name: 'note', label: 'Verification evidence / note', type: 'textarea', required: true, autofocus: true }],
    });
    if (!res) return;
    try {
      await api('POST', `/api/v1/capa/${id}/verify`, { note: res.note.trim() });
      toast('Corrective action verified and closed');
      await CAPA.load();
    } catch (err) { toast(err.message, true); }
  },

  async detail(id) {
    try {
      const d = await api('GET', `/api/v1/capa/${id}`);
      const a = d.action;
      const events = d.events || [];
      const row = (label, value) => `<div class="capa-detail-field"><span>${esc(label)}</span><div>${esc(value || '—')}</div></div>`;
      await modal({
        title: `CAPA-${a.id}: ${a.title}`, description: `${a.orderNumber} · ${CAPA.labels[a.status] || a.status}`,
        cancelLabel: 'Close', hideConfirm: true, wide: true,
        bodyHtml: `<div class="capa-detail-grid">
          ${row('Owner', '@' + a.owner)}${row('Due', fmtTime(a.dueAt))}
          ${row('Failure', a.failReason)}${row('Created by', '@' + a.createdBy)}
          ${row('Root cause', a.rootCause)}${row('Action plan', a.actionPlan)}
          ${row('Resolution', a.resolution)}${row('Verification', a.verificationNote ? `${a.verificationNote} — @${a.verifiedBy}, ${fmtTime(a.verifiedAt)}` : 'Not verified')}
        </div>
        <h3>History</h3><div class="capa-history">${events.map((e) => `<div class="capa-history-row">
          <strong>${esc(e.action)}</strong><span>@${esc(e.performedBy)} · ${esc(fmtTime(e.createdAt))}</span>
          ${e.note ? `<p>${esc(e.note)}</p>` : ''}
        </div>`).join('')}</div>`,
      });
    } catch (err) { toast(err.message, true); }
  },
};
