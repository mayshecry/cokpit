'use strict';

                                                                  
                                                                           
                                                                            
                                                                        

function qcbLoadFilters() {
  try {
    const f = JSON.parse(localStorage.getItem('cockpit_qcb_filters') || '{}');
    return { product: f.product || '', customer: f.customer || '' };
  } catch (e) { return { product: '', customer: '' }; }
}

const QCBench = {
  queue: [],
  reasons: [],
                                                        
  idx: 0,
  step: 0,
  checks: {},                                             
  verdict: null,                        
  reason: '',
  note: '',
  stepResults: {},
  customerImpact: false,
  sit: { n: 0, pass: 0, fail: 0, streak: 0, best: 0, startedAt: Date.now() },
  poll: null,
  list: [],                                                  
  filters: { product: '', customer: '' },
  ctxFor: 0,

  STEPS: [
    {
      key: 'paper', title: 'Paperwork & identity',
      checks: [
        ['p_debit', 'Debit number and customer match the order'],
        ['p_ticket', 'Omnitracker ticket present and linked'],
        ['p_asset', 'Asset tag on the device matches the order'],
      ],
    },
    {
      key: 'tech', title: 'Config & firmware',
      checks: [
        ['t_config', 'Configuration template applied as specified'],
        ['t_fw', 'Firmware at current baseline'],
        ['t_data', 'Customer data migrated / clean state verified'],
      ],
    },
    {
      key: 'test', title: 'Test evidence',
      checks: [
        ['s_self', 'Self-test script ran to completion'],
        ['s_res', 'Results attached and plausible'],
        ['s_open', 'No open failures left by the tech'],
      ],
    },
    {
      key: 'verdict', title: 'Verdict',
      checks: [],
    },
  ],

  async load() {
    const box = $('#qcbench-content');
    QCBench.filters = qcbLoadFilters();
    if (!QCBench.reasons.length) {
      try { QCBench.reasons = (await api('GET', '/api/v1/qc/reasons')).reasons || []; } catch (e) { QCBench.reasons = []; }
    }
    QCBench.bindOnce();
    await QCBench.refresh(false);
                                                                                    
                                                                                
    try {
    if (QCBench.queue.length) {
      const f = QCBench.filters;
      const prodOpts = new Set(QCBench.filterOptions('product').map(([v]) => v));
      const custOpts = new Set(QCBench.filterOptions('customer').map(([v]) => v));
      let dirty = false;
      if (f.product && !prodOpts.has(f.product)) { f.product = ''; dirty = true; }
      if (f.customer && !custOpts.has(f.customer)) { f.customer = ''; dirty = true; }
      if (dirty) {
        try { localStorage.setItem('cockpit_qcb_filters', JSON.stringify(f)); } catch (e) {  }
        QCBench.applyFilter(null);
        QCBench.render();
      }
    }
    } catch (e) { console.warn('[qcbench] filter hygiene skipped:', e); }
    if (!QCBench.poll) {
      QCBench.poll = setInterval(() => {
        const pane = $('#view-qcbench');
        if (pane && !pane.classList.contains('hidden')) QCBench.refresh(true);
      }, 20000);
    }
  },

  async refresh(quiet) {
    try {
      const d = await api('GET', '/api/v1/qc/queue');
      const prevId = (QCBench.list[QCBench.idx] || QCBench.cur() || {}).id;
      const before = new Set(QCBench.queue.map((q) => q.id));
      QCBench.queue = d.queue || [];
                                                                             
                                                               
      const f = QCBench.filters;
      if ((f.product || f.customer) && QCBench.queue.length) {
        const kept = QCBench.queue.filter((it) =>
          (!f.product || (it.productCode || '').split(' + ').includes(f.product)) &&
          (!f.customer || (it.customer || '') === f.customer));
        if (!kept.length) {
          QCBench.filters = { product: '', customer: '' };
          try { localStorage.setItem('cockpit_qcb_filters', '{}'); } catch (e) {  }
          if (!quiet || QCBench._sseKick) toast('Stale queue filter cleared — showing everything waiting');
        }
      }
      QCBench.applyFilter(prevId);
                                                                            
      const fresh = QCBench.queue.filter((q) => !before.has(q.id));
      if (QCBench._seenOnce && fresh.length) {
        QCBench._newIds = new Set(fresh.map((q) => q.id));
        const visible = new Set(QCBench.list.map((q) => q.id));
        const hiddenByFilter = fresh.filter((q) => !visible.has(q.id));
        toast(`${fresh.length === 1 ? fresh[0].orderNumber + ' entered' : fresh.length + ' orders entered'} the QC queue${hiddenByFilter.length === fresh.length ? ' — hidden by your filters' : hiddenByFilter.length ? ` (${hiddenByFilter.length} hidden by filters)` : ''}`);
        setTimeout(() => { QCBench._newIds = null; }, 5200);
      }
      QCBench._seenOnce = true;
      QCBench.render();
    } catch (err) {
      if (!quiet) $('#qcbench-content').innerHTML = `<p class="muted">Queue unavailable: ${esc(err.message)}</p>`;
    }
  },

  sseRefresh() {
    const pane = $('#view-qcbench');
    if (!pane || pane.classList.contains('hidden')) return;
    const now = Date.now();
    if (QCBench._lastLive && now - QCBench._lastLive < 1200) return;
    QCBench._lastLive = now;
    QCBench._sseKick = true;
    QCBench.refresh(true).finally(() => { QCBench._sseKick = false; });
  },

  resetOrderState() {
    QCBench.step = 0;
    QCBench.checks = {};
    QCBench.verdict = null;
    QCBench.reason = '';
    QCBench.note = '';
    QCBench.customerImpact = false;
  },

  async recordCurrentStep() {
    const q = QCBench.cur();
    if (!q || QCBench.step >= 3) return;
    const st = QCBench.STEPS[QCBench.step];
    const allDone = st.checks.every(([k]) => QCBench.checks[k]);
    const key = st.key + ':' + QCBench.step;
    QCBench.stepResults[key] = {
      orderId: q.id,
      stepKey: st.key,
      stepIndex: QCBench.step,
      correct: allDone,
      customerImpact: QCBench.customerImpact,
      performedBy: (state && state.user && state.user.username) || '',
      notes: '',
    };
    try {
      await api('POST', '/api/v1/qcbench/step-result', QCBench.stepResults[key]);
    } catch {  }
  },

  cur() { return QCBench.list[QCBench.idx] || null; },

  applyFilter(prevId) {
    const f = QCBench.filters;
    QCBench.list = QCBench.queue.filter((it) =>
      (!f.product || (it.productCode || '').split(' + ').includes(f.product)) &&
      (!f.customer || (it.customer || '') === f.customer));
                                                                              
    if (prevId != null) {
      const at = QCBench.list.findIndex((q) => q.id === prevId);
      QCBench.idx = at >= 0 ? at : Math.min(QCBench.idx, Math.max(0, QCBench.list.length - 1));
    } else {
      QCBench.idx = Math.min(QCBench.idx, Math.max(0, QCBench.list.length - 1));
    }
  },

  setFilter(key, val) {
    QCBench.filters[key] = val;
    localStorage.setItem('cockpit_qcb_filters', JSON.stringify(QCBench.filters));
    const keep = QCBench.cur() && QCBench.cur().id;
    QCBench.applyFilter(keep);
    if (!QCBench.cur() && QCBench.list.length) QCBench.idx = 0;
    QCBench.resetOrderState();
    QCBench.render();
  },

  filterOptions(key) {
    const counts = new Map();
    for (const it of QCBench.queue) {
      const vals = key === 'product' ? (it.productCode || '').split(' + ').filter(Boolean) : [it.customer || ''];
      for (const v of vals) {
        if (!v) continue;
        counts.set(v, (counts.get(v) || 0) + 1);
      }
    }
    return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  },

  sitLabel() {
    const s = QCBench.sit;
    const rate = s.n ? ((s.pass / s.n) * 100).toFixed(1) + '%' : '—';
    return `${s.n} checked · ${rate} pass · streak ${s.streak}`;
  },

  render() {
    try { QCBench.renderCore(); } catch (err) {
      console.error('[qcbench] render failed:', err);
      const box = $('#qcbench-content');
      if (box) box.innerHTML = `<div class="qcb-clear"><h2>QC bench hit a renderer error</h2><p class="muted">${esc(err && err.message || String(err))}</p><p class="muted">The queue itself is fine — please reload once, and tell development this message if it stays.</p><button type="button" class="btn btn-secondary" id="qcb-again2">Reload queue</button></div>`;
      const again = $('#qcb-again2');
      if (again) again.addEventListener('click', () => QCBench.refresh(false));
    }
  },

  renderCore() {
    const box = $('#qcbench-content');
    if (!box) return;
    const sit = $('#qcb-sit');
    if (sit) sit.textContent = QCBench.sitLabel();
    const q = QCBench.cur();
    if (!q) {
      const s = QCBench.sit;
      box.innerHTML = `
        <div class="qcb-clear">
          <div class="qcb-clear-ico">✓</div>
          <h2>Queue clear</h2>
          <p class="muted">Nothing waiting for QC right now.</p>
          ${s.n ? `<p class="qcb-clear-sit mono">${s.n} checks this sitting · ${(s.pass / s.n * 100).toFixed(1)}% passed · best streak ${s.best}</p>` : ''}
          <button type="button" class="btn btn-secondary" id="qcb-again">Check for new orders</button>
        </div>`;
      const again = $('#qcb-again');
      if (again) again.addEventListener('click', () => QCBench.refresh(false));
      return;
    }
    const waitH = (Date.now() - q.updatedAt) / 3600000;
    const breach = q.targetAt < Date.now();
    box.innerHTML = `
      <div class="qcb-wrap">
        <aside class="qcb-rail">
          <header class="qcb-rail-head">
            <div class="qcb-rail-title">
              <span>Queue</span>
              <strong class="mono">${QCBench.queue.length + ' waiting'}</strong>
              ${QCBench.filters.product || QCBench.filters.customer
                ? `<button type="button" class="qcb-fchip mono" id="qcb-freset" title="Clear queue filters">filtered: ${QCBench.list.length} of ${QCBench.queue.length} · reset ×</button>`
                : ''}
            </div>
            <div class="qcb-filters">
              <label class="qcb-f"><span>Product</span>
                <select id="qcb-f-product">
                  <option value="">All products</option>
                  ${QCBench.filterOptions('product').map(([v, n]) => `<option value="${esc(v)}" ${QCBench.filters.product === v ? 'selected' : ''}>${esc(v)} · ${n}</option>`).join('')}
                </select>
              </label>
              <label class="qcb-f"><span>Customer</span>
                <select id="qcb-f-customer">
                  <option value="">All customers</option>
                  ${QCBench.filterOptions('customer').map(([v, n]) => `<option value="${esc(v)}" ${QCBench.filters.customer === v ? 'selected' : ''}>${esc(v)} · ${n}</option>`).join('')}
                </select>
              </label>
            </div>
          </header>
          <ul class="qcb-list">
            ${QCBench.list.map((it, i) => `
              <li class="qcb-item ${i === QCBench.idx ? 'active' : ''} ${it.targetAt < Date.now() ? 'breached' : ''} ${QCBench._newIds && QCBench._newIds.has(it.id) ? 'qcb-new' : ''} ${it.isSplit ? 'split-from' : ''}" data-i="${i}" ${it.isSplit ? 'title="Split from another order"' : ''}>
                <span class="mono qcb-item-num">${esc(it.orderNumber)}${it.isSplit ? ' ↬' : ''}</span>
                ${it.productCode ? `<span class="qcb-pchip mono">${esc(it.productCode)}</span>` : ''}
                <span class="qcb-item-meta">${esc(it.customer || '—')} · ${esc(it.device || '—')}</span>
                <span class="qcb-item-foot mono">${it.tech ? '@' + esc(it.tech) : 'unassigned'} · ${waitLabel(it.updatedAt)} in queue</span>
              </li>`).join('')}
          </ul>
        </aside>
        <section class="qcb-bench" data-order="${q.id}">
          <header class="qcb-head">
            <div>
              <div class="qcb-ord mono">${esc(q.orderNumber)}</div>
              <div class="qcb-meta">${q.productCode ? `<span class="qcb-pchip mono">${esc(q.productCode)}</span>` : ''}${esc(q.customer || '—')} · ${esc(q.device || '—')} · tech ${q.tech ? '@' + esc(q.tech) : '—'}</div>
            </div>
            <div class="qcb-head-right">
              <span class="qcb-wait mono ${breach ? 'bad-t' : ''}">${waitLabel(q.updatedAt)} in queue${breach ? ' · past target' : ''}</span>
              <span class="qcb-pos mono">${QCBench.idx + 1} / ${QCBench.list.length}</span>
            </div>
          </header>
          <ol class="qcb-steps">
            ${QCBench.STEPS.map((st, i) => `
              <li class="qcb-step ${i === QCBench.step ? 'active' : ''} ${i < QCBench.step ? 'done' : ''}" data-step="${i}">
                <span class="qcb-step-n">${i < QCBench.step ? '✓' : i + 1}</span><span>${esc(st.title)}</span>
              </li>`).join('')}
          </ol>
          <div class="qcb-panel">
            ${QCBench.step < 3 ? `
              <h3>${esc(QCBench.STEPS[QCBench.step].title)}</h3>
              <p class="muted qcb-hint">Tick everything before moving on — keys 1-3 toggle, Enter continues.</p>
              <ul class="qcb-checks">
                ${QCBench.STEPS[QCBench.step].checks.map(([k, label], i) => `
                  <li><label class="qcb-check ${QCBench.checks[k] ? 'on' : ''}">
                    <input type="checkbox" data-k="${k}" ${QCBench.checks[k] ? 'checked' : ''}>
                    <span class="qcb-box" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg></span>
                    <span class="qcb-keycap mono" aria-hidden="true">${i + 1}</span>
                    <span class="qcb-check-t">${esc(label)}</span>
                  </label></li>`).join('')}
              </ul>
              <div class="qcb-step-impact">
                <label class="checkbox-label">
                  <input type="checkbox" id="qcb-customer-impact" ${QCBench.customerImpact ? 'checked' : ''}>
                  <span>No customer impact — mistake does not count toward score</span>
                </label>
              </div>` : QCBench.verdictHtml()}
          </div>
          <footer class="qcb-foot">
            <button type="button" class="btn btn-ghost" id="qcb-back" ${QCBench.step === 0 ? 'disabled' : ''}>← Back</button>
            <span class="qcb-keys muted">1-3 tick · P pass · F fail · Enter submit · ↑/↓ queue</span>
            ${QCBench.step < 3
              ? `<button type="button" class="btn btn-primary" id="qcb-next" ${QCBench.stepDone() ? '' : 'disabled'}>Next step →</button>`
              : `<button type="button" class="btn btn-primary" id="qcb-submit" ${QCBench.canSubmit() ? '' : 'disabled'}>Submit & next →</button>
              <button type="button" class="btn btn-ghost btn-sm" id="qcb-rerun" title="Rerun workflow">↻</button>`}
          </footer>
        </section>
        <aside class="qcb-ctx">
          <div class="qcb-ctx-card">
            <header>Order context</header>
            <dl>
              <div><dt>Customer</dt><dd>${esc(q.customer || '—')}</dd></div>
              <div><dt>Device</dt><dd>${esc(q.device || '—')}</dd></div>
              <div><dt>Tech</dt><dd>${q.tech ? '@' + esc(q.tech) : 'unassigned'}</dd></div>
              <div><dt>In queue</dt><dd class="mono">${waitLabel(q.updatedAt)}</dd></div>
              <div><dt>Target</dt><dd class="mono ${breach ? 'bad-t' : ''}">${targetLabel(q.targetAt)}</dd></div>
              <div><dt>Product</dt><dd class="mono">${esc(q.productCode || '—')}${q.productName ? ' · ' + esc(q.productName) : ''}</dd></div>
              <div><dt>Position</dt><dd class="mono">${QCBench.idx + 1} of ${QCBench.list.length}</dd></div>
            </dl>
          </div>
          <div class="qcb-ctx-card">
            <header>History · same customer / product</header>
            <div class="qcb-hist" id="qcb-hist"><span class="muted">Loading…</span></div>
          </div>
          <div class="qcb-ctx-card">
            <header>This sitting</header>
            <div class="qcb-sitrow mono"><span>${QCBench.sit.n} checked</span><span>${QCBench.sit.n ? ((QCBench.sit.pass / QCBench.sit.n) * 100).toFixed(0) + '% pass' : '—'}</span></div>
            <div class="qcb-sitbar" aria-hidden="true"><i style="width:${QCBench.sit.n ? (QCBench.sit.pass / QCBench.sit.n) * 100 : 0}%"></i></div>
            <div class="qcb-sitrow mono"><span>streak ${QCBench.sit.streak}</span><span>best ${QCBench.sit.best}</span></div>
          </div>
        </aside>
      </div>`;
    QCBench.loadContext(q.id);
  },

  async loadContext(id) {
    QCBench.ctxFor = id;
    const box = $('#qcb-hist');
    if (!box) return;
    try {
      const [d, splits, workedBy] = await Promise.all([
        api('GET', '/api/v1/qc/context/' + id).catch(() => ({ recent: [] })),
        api('GET', '/api/v1/orders/' + id + '/splits').catch(() => ({ splits: [] })),
        api('GET', '/api/v1/orders/' + id + '/worked-by').catch(() => ({ users: [] })),
      ]);
      if (QCBench.ctxFor !== id) return;
      const rows = d.recent || [];
      const splitList = splits.splits || [];
      const currentWorkedBy = workedBy.users || [];

      let originalWorkedBy = [];
      const q = QCBench.cur();
      if (q && q.isSplit && q.splitFrom) {
        const ob = await api('GET', '/api/v1/orders/' + q.splitFrom + '/worked-by').catch(() => ({ users: [] }));
        originalWorkedBy = ob.users || [];
      }

      let html = '';
      if (!rows.length) {
        html += '<span class="muted">First QC for this customer / product.</span>';
      } else {
        const fails = rows.filter((r) => r.status === 'FAIL').length;
        html += `\n
          <div class="qcb-hist-sum mono"><span>${rows.length} recent</span><span class="${fails ? 'bad-t' : 'good-t'}">${fails} failed</span></div>\n
          ${rows.map((r) => `\n
            <div class="qcb-hist-row" title="${esc(r.notes || r.status)}">\n
              <span class="qcb-dot ${r.status === 'PASS' ? 'pass' : 'fail' } aria-hidden=\"true\"></span>\n
              <span class="mono">${esc(r.orderNumber)}</span>\n
              <span class="qcb-hist-why">${esc(r.notes || (r.status === 'PASS' ? 'passed' : 'failed'))}</span>\n
              ${r.sameProduct ? '<span class="qcb-pchip mono">product</span>' : ''}\n
              <span class="mono muted">${waitLabel(r.at)}</span>\n
            </div>`).join('')}`;
      }

      if (splitList.length) {
        html += `\n
          <div class="qcb-split-hist">
            <div class="qcb-hist-sum mono" style="margin-top:10px"><span>Splits · ${splitList.length}</span></div>\n
            ${splitList.map((s) => `\n
              <div class="qcb-hist-row" title="${esc(s.reason || 'split')}">\n
                <span class="qcb-dot ${s.status === 'accepted' ? 'pass' : 'fail' } aria-hidden=\"true\"></span>\n
                <span class="mono">#${s.splitOrderId || s.originalOrderId}</span>\n
                <span class="qcb-hist-why">${esc(s.status)} · ${esc(s.requestedBy || '—')}</span>\n
                ${s.acceptedBy ? `<span class="mono muted">accepted by ${esc(s.acceptedBy)}</span>` : ''}\n
              </div>`).join('')}\n
          </div>`;
      }

      const allWorkedBy = [...new Set([...originalWorkedBy, ...currentWorkedBy])];
      if (allWorkedBy.length) {
        const labels = [];
        if (originalWorkedBy.length) labels.push('original: ' + originalWorkedBy.join(', '));
        if (currentWorkedBy.length) labels.push('split: ' + currentWorkedBy.join(', '));
        html += `\n
          <div class="qcb-worked-by" style="margin-top:10px;padding-top:10px;border-top:1px solid var(--border);">
            <div class="qcb-hist-sum mono"><span>Worked by · ${allWorkedBy.length}</span></div>\n
            <div class="qcb-hist-row">${labels.join(' · ')}</div>\n
          </div>`;
      }

      // Shared order info (parent/child relationship)
      const sharedInfo = await api('GET', '/api/v1/orders/' + id + '/shared-info').catch(() => null);
      if (sharedInfo && sharedInfo.shared) {
        const si = sharedInfo.shared;
        const isParent = si.SplitFromOrderID === 0 && si.Shared;
        const isChild = si.SplitFromOrderID > 0;
        html += `\n
          <div class="qcb-shared-info" style="margin-top:10px;padding-top:10px;border-top:1px solid var(--border);background:var(--brand-soft);border-radius:var(--r-md);">
            <div class="qcb-hist-sum mono" style="color:var(--brand)">${isParent ? 'PARENT ORDER (shared)' : isChild ? 'CHILD ORDER (shared)' : 'SHARED ORDER'}</div>
            ${isParent ? `<div class="qcb-hist-row"><span>Children:</span> <span>${si.Children ? si.Children.map((c) => '#'+c.OrderNumber+' '+c.Status).join(', ') : 'none'}</span></div>` : ''}
            ${isChild ? `<div class="qcb-hist-row"><span>Parent:</span> <span>#${si.ParentOrderNumber} (${si.ParentStatus}) · ${si.ParentAssignee || '—'}</span></div>` : ''}
            <div class="qcb-hist-row"><span>Worked by:</span> <span>${si.WorkedBy ? si.WorkedBy.join(', ') : '—'}</span></div>
          </div>`;
      }

      box.innerHTML = html || '<span class="muted">No history.</span>';
    } catch (e) {
      if (QCBench.ctxFor === id) box.innerHTML = '<span class="muted">History unavailable.</span>';
    }
  },


  doneUpto(k) {
    for (let i = 0; i < k && i < 3; i++) {
      if (!QCBench.STEPS[i].checks.every(([ck]) => QCBench.checks[ck])) return false;
    }
    return true;
  },

  stepDone() {
    if (QCBench.step >= 3) return true;
    return QCBench.STEPS[QCBench.step].checks.every(([k]) => QCBench.checks[k]);
  },

  canSubmit() {
    return QCBench.verdict === 'PASS' || (QCBench.verdict === 'FAIL' && QCBench.reason);
  },

  verdictHtml() {
    const v = QCBench.verdict;
    return `
      <h3>Call it</h3>
      <p class="muted qcb-hint">Pass closes the order. Fail sends it back to the tech with your reason.</p>
      <div class="qcb-verdicts">
        <button type="button" class="qcb-v pass ${v === 'PASS' ? 'sel' : ''}" data-v="PASS">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>
          <span class="qcb-v-t">Pass</span><span class="qcb-v-s">order completes</span><span class="qcb-keycap mono" aria-hidden="true">P</span>
        </button>
        <button type="button" class="qcb-v fail ${v === 'FAIL' ? 'sel' : ''}" data-v="FAIL">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>
          <span class="qcb-v-t">Fail</span><span class="qcb-v-s">back to rework</span><span class="qcb-keycap mono" aria-hidden="true">F</span>
        </button>
      </div>
      ${v === 'FAIL' ? `
        <div class="qcb-reasons" role="radiogroup" aria-label="Fail reason">
          ${QCBench.reasons.map((r, i) => `
            <button type="button" class="chip qcb-reason ${QCBench.reason === r ? 'active' : ''}" data-r="${esc(r)}" title="key ${i + 1}"><span class="qcb-keycap mono" aria-hidden="true">${i + 1}</span>${esc(r)}</button>`).join('')}
        </div>
        <textarea class="qcb-note" id="qcb-note" rows="2" placeholder="Extra note for the tech (optional) — what exactly was wrong?">${esc(QCBench.note)}</textarea>` : ''}
      ${v === 'PASS' ? `<textarea class="qcb-note" id="qcb-note" rows="2" placeholder="Note (optional) — anything worth remembering?">${esc(QCBench.note)}</textarea>` : ''}`;
  },

  bindOnce() {
    if (QCBench.bound) return;
    QCBench.bound = true;
    const box = $('#qcbench-content');
    box.addEventListener('click', (e) => {
      const item = e.target.closest('.qcb-item');
      if (item) {
        QCBench.idx = Number(item.dataset.i);
        QCBench.resetOrderState();
        QCBench.render();
        return;
      }
      const step = e.target.closest('.qcb-step');
      if (step) {
        const k = Number(step.dataset.step);
        if (k <= QCBench.step || QCBench.doneUpto(k)) { QCBench.step = k; QCBench.render(); }
        return;
      }
      const v = e.target.closest('.qcb-v');
      if (v) {
        QCBench.verdict = v.dataset.v;
        if (QCBench.verdict === 'PASS') QCBench.reason = '';
        QCBench.rerenderPanel();
        return;
      }
      const rs = e.target.closest('.qcb-reason');
      if (rs) {
        QCBench.reason = rs.dataset.r;
        box.querySelectorAll('.qcb-reason').forEach((x) => x.classList.toggle('active', x === rs));
        const sub = $('#qcb-submit');
        if (sub) sub.disabled = !QCBench.canSubmit();
        return;
      }
      if (e.target.closest('#qcb-back')) { QCBench.step = Math.max(0, QCBench.step - 1); QCBench.render(); return; }
      if (e.target.closest('#qcb-next')) {
        QCBench.recordCurrentStep();
        QCBench.step = Math.min(3, QCBench.step + 1);
        QCBench.render();
        return;
      }
      if (e.target.closest('#qcb-submit')) {
        QCBench.recordCurrentStep();
        QCBench.submit();
        return;
      }
      if (e.target.closest('#qcb-rerun')) { QCBench.rerun(); return; }
      if (e.target.closest('#qcb-again')) { QCBench.refresh(false); return; }
      if (e.target.closest('#qcb-freset')) {
        QCBench.filters = { product: '', customer: '' };
        try { localStorage.setItem('cockpit_qcb_filters', '{}'); } catch (err) {  }
        QCBench.applyFilter(null);
        QCBench.render();
      }
    });
    box.addEventListener('change', (e) => {
      if (e.target.id === 'qcb-f-product') { QCBench.setFilter('product', e.target.value); return; }
      if (e.target.id === 'qcb-f-customer') { QCBench.setFilter('customer', e.target.value); return; }
      if (!e.target.matches('.qcb-checks input')) return;
      QCBench.checks[e.target.dataset.k] = e.target.checked;
      e.target.closest('.qcb-check').classList.toggle('on', e.target.checked);
      const nxt = $('#qcb-next');
      if (nxt) nxt.disabled = !QCBench.stepDone();
    });
    box.addEventListener('input', (e) => {
      if (e.target.id === 'qcb-note') QCBench.note = e.target.value;
    });
    box.addEventListener('change', (e) => {
      if (e.target.id === 'qcb-customer-impact') {
        QCBench.customerImpact = e.target.checked;
      }
    });
  },

  rerenderPanel() {
    const bench = $('#qcbench-content .qcb-bench');
    if (!bench) return;
    bench.querySelector('.qcb-panel').innerHTML = QCBench.verdictHtml();
    const sub = $('#qcb-submit');
    if (sub) sub.disabled = !QCBench.canSubmit();
  },

  async submit() {
    const q = QCBench.cur();
    if (!q || !QCBench.canSubmit()) return;
    const sub = $('#qcb-submit');
    if (sub) sub.disabled = true;
    try {
      await api('POST', `/api/v1/orders/${q.id}/qc`, {
        status: QCBench.verdict,
        notes: QCBench.note.trim(),
        failReason: QCBench.verdict === 'FAIL' ? QCBench.reason : '',
      });
      await api('POST', `/api/v1/orders/${q.id}/transition`, {
        status: QCBench.verdict === 'PASS' ? 'Completed' : 'Processing',
      });
      const s = QCBench.sit;
      s.n++;
      if (QCBench.verdict === 'PASS') { s.pass++; s.streak++; s.best = Math.max(s.best, s.streak); }
      else { s.fail++; s.streak = 0; }
      const sit = $('#qcb-sit');
      if (sit) sit.textContent = QCBench.sitLabel();
      toast(`${q.orderNumber} ${QCBench.verdict === 'PASS' ? 'passed — completed' : 'failed — ' + QCBench.reason}`, QCBench.verdict !== 'PASS');
      QCBench.advance();
    } catch (err) {
      toast(err.message || 'submit failed', true);
      if (sub) sub.disabled = false;
    }
  },

  async rerun() {
    const q = QCBench.cur();
    if (!q) return;
    if (!confirm('Rerun workflow on this order? This resets QC progress.')) return;
    const btn = $('#qcb-rerun');
    if (btn) { btn.disabled = true; btn.textContent = '…'; }
    try {
      const d = await api('POST', `/api/v1/qcbench/rerun/${q.id}`, {});
      toast(`${q.orderNumber} workflow rerun`);
      QCBench.resetOrderState();
      QCBench.advance();
    } catch (err) {
      toast(err.message || 'rerun failed', true);
      if (btn) { btn.disabled = false; btn.textContent = '↻'; }
    }
  },

  advance() {
    const bench = $('#qcbench-content .qcb-bench');
    if (bench) bench.classList.add('qcb-out');
    setTimeout(async () => {
      QCBench.resetOrderState();
      const gone = QCBench.list[QCBench.idx];
      if (gone) QCBench.queue = QCBench.queue.filter((x) => x.id !== gone.id);
      QCBench.applyFilter(null);
      if (QCBench.idx >= QCBench.list.length) QCBench.idx = 0;
      QCBench.render();
      const nb = $('#qcbench-content .qcb-bench');
      if (nb) nb.classList.add('qcb-in');
      QCBench.refresh(true);
    }, 240);
  },
};

function targetLabel(ms) {
  const d = ms - Date.now();
  if (d < 0) return 'past by ' + waitLabel(ms);
  return 'due in ' + waitLabel(Date.now() - d);
}

function waitLabel(ms) {
  const h = (Date.now() - ms) / 3600000;
  if (h < 1) return Math.max(1, Math.round(h * 60)) + 'm';
  if (h < 48) return Math.round(h) + 'h';
  return Math.round(h / 24) + 'd';
}

document.addEventListener('keydown', (e) => {
  const pane = $('#view-qcbench');
  if (!pane || pane.classList.contains('hidden')) return;
  const t = document.activeElement;
  if (t && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)) return;
  if (!$('#modal-root').classList.contains('hidden')) return;
  if (e.key === 'p' || e.key === 'P') {
    if (QCBench.step < 3) return;
    QCBench.verdict = 'PASS'; QCBench.reason = ''; QCBench.rerenderPanel(); e.preventDefault();
  } else if (e.key === 'f' || e.key === 'F') {
    if (QCBench.step < 3) return;
    QCBench.verdict = 'FAIL'; QCBench.rerenderPanel(); e.preventDefault();
  } else if (e.key === 'Enter') {
    if (QCBench.step < 3) { if (QCBench.stepDone()) { QCBench.step++; QCBench.render(); } }
    else if (QCBench.canSubmit()) QCBench.submit();
    e.preventDefault();
  } else if (e.key >= '1' && e.key <= '9') {
    if (QCBench.step < 3 && e.key <= '3') {
      const ck = QCBench.STEPS[QCBench.step].checks[Number(e.key) - 1];
      if (ck) {
        QCBench.checks[ck[0]] = !QCBench.checks[ck[0]];
        QCBench.render();
        e.preventDefault();
      }
      return;
    }
    if (QCBench.verdict === 'FAIL') {
      const r = QCBench.reasons[Number(e.key) - 1];
      if (r) {
        QCBench.reason = r;
        document.querySelectorAll('.qcb-reason').forEach((x) => x.classList.toggle('active', x.dataset.r === r));
        const sub = $('#qcb-submit');
        if (sub) sub.disabled = !QCBench.canSubmit();
      }
    }
  } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    if (!QCBench.list.length) return;
    QCBench.idx = (QCBench.idx + (e.key === 'ArrowDown' ? 1 : -1) + QCBench.list.length) % QCBench.list.length;
    QCBench.resetOrderState();
    QCBench.render();
    e.preventDefault();
  }
});

$('#qcb-refresh').addEventListener('click', () => QCBench.refresh(false));

window.QCBench = QCBench;
