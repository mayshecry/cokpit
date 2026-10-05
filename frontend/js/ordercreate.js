/* OrderCreate — dedicated new-order modal.
 *
 * Replaces the generic field-grid modal: a real customer picker with a
 * searchable menu (existing customers as selectable rows, inline "use as
 * new customer" row, guided-flow badges), a suggested order number,
 * deadline quick-chips, and details collapsed until wanted.
 */
const OrderCreate = {
  sel: null,        // { name, meta|null } meta = customer directory row
  q: '',
  customers: [],
  actIdx: 0,
  submitting: false,

  open(opts) {
    OrderCreate.sel = null;
    OrderCreate.q = '';
    OrderCreate.actIdx = 0;
    OrderCreate.submitting = false;
    OrderCreate.customers = opts.customers || [];
    const root = document.getElementById('modal-root');
    const dc = (d) => {
      const x = new Date(d.getTime() - d.getTimezoneOffset() * 60000);
      return x.toISOString().slice(0, 16);
    };
    root.innerHTML = `
      <div class="modal wide oc-modal" role="dialog" aria-modal="true" aria-labelledby="oc-title">
        <div class="oc-head">
          <div>
            <h2 id="oc-title">New order</h2>
            <p class="muted">Pick the customer, name the order, set the deadline — the rest can wait.</p>
          </div>
          <button type="button" class="oc-x" data-oc-x aria-label="Close">×</button>
        </div>
        <div class="oc-body">
          <section class="oc-sec">
            <div class="oc-sec-t">Customer <span class="oc-req">*</span></div>
            <div class="oc-pick" id="oc-picked"></div>
            <div class="oc-custwrap" id="oc-custwrap">
              <div class="oc-search">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></svg>
                <input id="oc-q" type="text" placeholder="Search known customers — or type a new name"
                       autocomplete="off" spellcheck="false">
              </div>
              <div class="oc-menu" id="oc-menu" role="listbox"></div>
            </div>
          </section>
          <section class="oc-sec">
            <div class="oc-sec-t">Basics</div>
            <div class="oc-grid">
              <div class="field">
                <label for="oc-num">Order number <span class="oc-req">*</span></label>
                <input id="oc-num" type="text" value="${esc(opts.nextNum || '')}" placeholder="e.g. ORD-10432"
                       autocomplete="off" spellcheck="false">
              </div>
              <div class="field">
                <label for="oc-target">Target completion</label>
                <div class="oc-chips" id="oc-chips">
                  <button type="button" data-t="eod">Today · 17:00</button>
                  <button type="button" data-t="tmr" class="on">Tomorrow · 17:00</button>
                  <button type="button" data-t="d3">+3 days</button>
                  <button type="button" data-t="w1">+1 week</button>
                </div>
                <input id="oc-target" type="datetime-local" value="${esc(opts.defaultTargetVal || (opts.defaultTarget ? dc(opts.defaultTarget) : ''))}">
                <span class="field-hint">Feeds the SLA state — blank uses the server default of +24h.</span>
              </div>
            </div>
          </section>
          <details class="oc-details" id="oc-details">
            <summary>Order details <span class="muted">debit · device · asset · config · ticket — optional</span></summary>
            <div class="oc-grid">
              <div class="field"><label for="oc-debit">Debit number</label><input id="oc-debit" type="text" placeholder="DEB-0000" autocomplete="off" spellcheck="false"></div>
              <div class="field"><label for="oc-omni">Omnitracker ticket</label><input id="oc-omni" type="text" placeholder="OT-00000" autocomplete="off" spellcheck="false"></div>
              <div class="field"><label for="oc-device">Device</label><input id="oc-device" type="text" placeholder="ThinkCentre M90q" autocomplete="off" spellcheck="false"></div>
              <div class="field"><label for="oc-asset">Asset number</label><input id="oc-asset" type="text" placeholder="ASSET-00000" autocomplete="off" spellcheck="false"></div>
              <div class="field oc-wide"><label for="oc-config">Configuration</label><input id="oc-config" type="text" placeholder="i5 / 16GB / 512GB SSD" autocomplete="off" spellcheck="false"></div>
            </div>
          </details>
          <p class="oc-err hidden" id="oc-err"></p>
        </div>
        <div class="modal-foot oc-foot">
          <span class="muted oc-sum" id="oc-sum">Pick a customer to continue</span>
          <div class="oc-foot-r">
            <button type="button" class="btn btn-secondary" data-oc-x>Cancel</button>
            <button type="button" class="btn btn-primary" id="oc-create" disabled>Create order</button>
          </div>
        </div>
      </div>`;
    root.classList.remove('hidden');
    modalCleanup = () => OrderCreate.close(null);
    document.addEventListener('keydown', OrderCreate.escKey, true);
    OrderCreate.bind(root);
    OrderCreate.renderMenu();
    OrderCreate.sync();
    setTimeout(() => { const q = document.getElementById('oc-q'); if (q) q.focus(); }, 60);
  },

  close(result) {
    const root = document.getElementById('modal-root');
    if (root) { root.classList.add('hidden'); root.innerHTML = ''; }
    modalCleanup = null;
    document.removeEventListener('keydown', OrderCreate.escKey, true);
    if (OrderCreate.onDone) OrderCreate.onDone(result);
  },

  bind(root) {
    root.querySelectorAll('[data-oc-x]').forEach((b) => b.addEventListener('click', () => OrderCreate.close(null)));
    const qIn = document.getElementById('oc-q');
    qIn.addEventListener('input', () => { OrderCreate.q = qIn.value; OrderCreate.actIdx = 0; OrderCreate.renderMenu(); });
    qIn.addEventListener('keydown', (e) => OrderCreate.onKey(e));
    document.getElementById('oc-num').addEventListener('input', () => OrderCreate.sync());
    document.getElementById('oc-menu').addEventListener('click', (e) => {
      const row = e.target.closest('.oc-row');
      if (row) OrderCreate.choose(row.dataset.name, row.dataset.new === '1');
    });
    const chips = document.getElementById('oc-chips');
    chips.addEventListener('click', (e) => {
      const b = e.target.closest('button[data-t]');
      if (!b) return;
      chips.querySelectorAll('button').forEach((x) => x.classList.toggle('on', x === b));
      OrderCreate.applyChip(b.dataset.t);
    });
    document.getElementById('oc-target').addEventListener('input', () => {
      chips.querySelectorAll('button').forEach((x) => x.classList.remove('on'));
    });
    document.getElementById('oc-create').addEventListener('click', () => OrderCreate.submit());
  },

  escKey(e) {
    if (!document.getElementById('oc-modal') && !document.querySelector('.oc-modal')) return;
    if (e.key === 'Escape') {
      e.stopPropagation();
      OrderCreate.close(null);
    }
  },

  onKey(e) {
    const rows = document.querySelectorAll('#oc-menu .oc-row');
    if (!rows.length) {
      if (e.key === 'Enter') { e.preventDefault(); if (!document.getElementById('oc-create').disabled) OrderCreate.submit(); }
      return;
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      OrderCreate.actIdx = (OrderCreate.actIdx + (e.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length;
      rows.forEach((r, i) => r.classList.toggle('act', i === OrderCreate.actIdx));
      rows[OrderCreate.actIdx].scrollIntoView({ block: 'nearest' });
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const r = rows[OrderCreate.actIdx];
      if (r) OrderCreate.choose(r.dataset.name, r.dataset.new === '1');
    }
  },

  matches() {
    const q = OrderCreate.q.trim().toLowerCase();
    const list = OrderCreate.customers.filter((c) => !q || c.name.toLowerCase().includes(q));
    return list.slice(0, 60);
  },

  hue(name) { let h = 0; for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360; return h; },

  av(name, cls) {
    const h = OrderCreate.hue(name);
    return `<span class="oc-av ${cls || ''}" style="background:linear-gradient(140deg,hsl(${h} 55% 55%),hsl(${(h + 32) % 360} 55% 44%))">${esc(name.slice(0, 1).toUpperCase())}</span>`;
  },

  rowHtml(c) {
    const badges = [];
    if (c.hasFlow) badges.push('<span class="oc-tag flow">guided flow</span>');
    if (c.number) badges.push(`<span class="oc-tag mono">${esc(c.number)}</span>`);
    else if (c.custom && !c.managed) badges.push('<span class="oc-tag used">used before</span>');
    return `<div class="oc-row" data-name="${esc(c.name)}" role="option">
      ${OrderCreate.av(c.name)}
      <span class="oc-row-main"><strong>${esc(c.name)}</strong>
        <span class="oc-row-sub">${badges.join(' ')}${c.assignedTo ? ` <span class="oc-tag op">@${esc(c.assignedTo)}</span>` : ''}</span></span>
      <span class="oc-row-cue">select →</span>
    </div>`;
  },

  renderMenu() {
    const menu = document.getElementById('oc-menu');
    if (!menu) return;
    const list = OrderCreate.matches();
    const q = OrderCreate.q.trim();
    const exact = q && OrderCreate.customers.some((c) => c.name.toLowerCase() === q.toLowerCase());
    let html = list.map(OrderCreate.rowHtml).join('');
    if (q && !exact) {
      html += `<div class="oc-row oc-row-new" data-name="${esc(q)}" data-new="1" role="option">
        <span class="oc-av new">+</span>
        <span class="oc-row-main"><strong>Use “${esc(q)}”</strong>
          <span class="oc-row-sub"><span class="oc-tag new">new customer</span></span></span>
        <span class="oc-row-cue">create →</span>
      </div>`;
    }
    if (!html) html = `<div class="oc-menu-empty muted">Type to search customers, or type a full name to create a new one.</div>`;
    menu.innerHTML = html;
    if (OrderCreate.actIdx > 0) OrderCreate.actIdx = Math.min(OrderCreate.actIdx, menu.children.length - 1);
    const rows = menu.querySelectorAll('.oc-row');
    rows.forEach((r, i) => r.classList.toggle('act', i === Math.max(0, Math.min(OrderCreate.actIdx, rows.length - 1))));
  },

  choose(name, isNew) {
    const meta = OrderCreate.customers.find((c) => c.name === name) || null;
    OrderCreate.sel = { name, meta, isNew: !!isNew && !meta };
    const wrap = document.getElementById('oc-custwrap');
    if (wrap) wrap.classList.add('hidden');
    const picked = document.getElementById('oc-picked');
    picked.innerHTML = `
      <div class="oc-sel">
        ${OrderCreate.av(name, 'big')}
        <span class="oc-sel-main">
          <strong>${esc(name)}</strong>
          <span class="oc-sel-sub">${meta && meta.hasFlow ? '<span class="oc-tag flow">guided flow</span> ' : ''}${meta && meta.number ? `<span class="oc-tag mono">${esc(meta.number)}</span>` : OrderCreate.sel.isNew ? '<span class="oc-tag new">new customer</span>' : ''}</span>
        </span>
        <button type="button" class="btn btn-ghost btn-sm" id="oc-change">Change</button>
      </div>`;
    document.getElementById('oc-change').addEventListener('click', () => {
      OrderCreate.sel = null;
      picked.innerHTML = '';
      const w2 = document.getElementById('oc-custwrap');
      if (w2) w2.classList.remove('hidden');
      OrderCreate.renderMenu();
      OrderCreate.sync();
      const qIn = document.getElementById('oc-q');
      if (qIn) qIn.focus();
    });
    OrderCreate.sync();
    const num = document.getElementById('oc-num');
    if (num && !num.value.trim()) num.focus();
  },

  sync() {
    const ok = !!(OrderCreate.sel && OrderCreate.sel.name) && !!document.getElementById('oc-num').value.trim();
    const btn = document.getElementById('oc-create');
    btn.disabled = !ok;
    const sum = document.getElementById('oc-sum');
    if (!OrderCreate.sel) sum.textContent = 'Pick a customer to continue';
    else if (!document.getElementById('oc-num').value.trim()) sum.textContent = 'Give the order a number';
    else sum.textContent = `${OrderCreate.sel.name} · ${document.getElementById('oc-num').value.trim()}`;
  },

  applyChip(kind) {
    const t = document.getElementById('oc-target');
    const now = new Date();
    let d;
    if (kind === 'eod') { d = new Date(now); d.setHours(17, 0, 0, 0); if (d <= now) d.setTime(d.getTime() + 24 * 3600 * 1000); }
    else if (kind === 'tmr') { d = new Date(now.getTime() + 24 * 3600 * 1000); d.setHours(17, 0, 0, 0); }
    else if (kind === 'd3') { d = new Date(now.getTime() + 3 * 24 * 3600 * 1000); d.setHours(17, 0, 0, 0); }
    else { d = new Date(now.getTime() + 7 * 24 * 3600 * 1000); d.setHours(17, 0, 0, 0); }
    const x = new Date(d.getTime() - d.getTimezoneOffset() * 60000);
    t.value = x.toISOString().slice(0, 16);
  },

  err(msg, extraOrder) {
    const e = document.getElementById('oc-err');
    e.classList.remove('hidden');
    e.innerHTML = `${esc(msg)}${extraOrder ? ` <button type="button" class="oc-err-open" data-open="${extraOrder.id}">open existing</button>` : ''}`;
    const b = e.querySelector('.oc-err-open');
    if (b) b.addEventListener('click', () => { OrderCreate.close(null); goToOrder(Number(b.dataset.open)); });
  },

  async submit() {
    if (OrderCreate.submitting || !OrderCreate.sel) return;
    const val = (id) => document.getElementById(id).value.trim();
    const body = { orderNumber: val('oc-num'), customerName: OrderCreate.sel.name };
    const map = { 'oc-debit': 'debitNumber', 'oc-omni': 'omnitrackerTicket', 'oc-device': 'device', 'oc-asset': 'assetNumber', 'oc-config': 'configuration' };
    for (const [id, key] of Object.entries(map)) { const v = val(id); if (v) body[key] = v; }
    const target = document.getElementById('oc-target').value;
    if (target) { const dt = new Date(target); if (!Number.isNaN(dt.getTime())) body.targetCompletionAt = dt.toISOString(); }
    OrderCreate.submitting = true;
    const btn = document.getElementById('oc-create');
    btn.disabled = true;
    btn.textContent = 'Creating…';
    try {
      await api('POST', '/api/v1/orders', body);
      toast(t('toast.created') + (OrderCreate.sel.isNew ? ` · new customer “${body.customerName}”` : ''));
      OrderCreate.close({ created: true });
      await loadOrders(true);
    } catch (err) {
      OrderCreate.submitting = false;
      btn.disabled = false;
      btn.textContent = 'Create order';
      const existing = err.data && err.data.error && err.data.error.extra && err.data.error.extra.existingOrder;
      if (err.status === 409 && existing) {
        OrderCreate.err(`Order ${existing.orderNumber} already exists (${statusLabel(existing.status)}, #${existing.id}).`, existing);
      } else {
        OrderCreate.err(err.message || 'Could not create the order.');
      }
    }
  },
};
