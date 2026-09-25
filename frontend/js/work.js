'use strict';

/* WorkFlow — guided "Start Work" sessions: clock in on an order assigned to
   you, tick off steps with manual checks, Next → Next → done. */

const DEFAULT_WORK_STEPS = [
  {
    key: 'intake',
    checks: ['c_data', 'c_device', 'c_asset'],
    next: (o) => (o.status === 'Received' ? 'Processing' : null),
  },
  {
    key: 'build',
    checks: ['c_config', 'c_firmware'],
    pick: true,
    next: () => null,
  },
  {
    key: 'test',
    checks: ['c_test', 'c_notes'],
    next: (o) => (o.status === 'Processing' ? 'QC_Review' : null),
  },
  {
    key: 'wrap',
    checks: ['c_clean', 'c_report'],
    qcGate: true,
    next: () => 'Completed',
  },
];

const WorkFlow = {
  session: null,      // active session inside the workbench
  flowLoaded: false,
  order: null,        // order object inside the workbench
  step: 0,
  checks: {},         // {stepKey: {checkKey: true}}
  pick: null,         // checklist items for step 2
  manuals: null,      // attached order manuals for step 2
  qcPassed: false,
  tickHandle: null,

  /* ---------- helpers ---------- */

  steps() {
    const src = (state.workFlow && state.workFlow.length) ? state.workFlow : DEFAULT_WORK_STEPS;
    return src.map((st) => ({
      ...st,
      checks: (st.checks || []).map((c) => (typeof c === 'string' ? { key: c } : c)),
    }));
  },

  async loadFlow(force) {
    if (WorkFlow.flowLoaded && !force) return;
    try {
      const d = await api('GET', '/api/v1/workflow');
      state.workFlow = d.steps || null;
      WorkFlow.flowLoaded = true;
    } catch { /* keep default */ }
  },

  stepTitle(st) { return st.title || t('work.step.' + st.key); },
  stepDesc(st) { return st.desc || t('work.step.' + st.key + 'd'); },
  checkLabel(c) { return c.label || t('work.check.' + c.key); },

  // Where should Next move the order? Functions (default flow) or a configured target.
  stepTarget(step, o) {
    let target = typeof step.next === 'function' ? step.next(o) : (step.next || null);
    if (!target || target === o.status) return null;
    return (typeof nextStates === 'function' && !nextStates(o.status).includes(target)) ? null : target;
  },

  mine(o) {
    return state.user && o && o.assignee === state.user.username;
  },

  activeFor(orderId) {
    return (state.myWork || {})[orderId] || null;
  },

  fmtElapsed(ms) {
    const s = Math.max(0, Math.floor(ms / 1000));
    const h = String(Math.floor(s / 3600)).padStart(2, '0');
    const m = String(Math.floor((s % 3600) / 60)).padStart(2, '0');
    const sec = String(s % 60).padStart(2, '0');
    return `${h}:${m}:${sec}`;
  },

  startTick() {
    if (WorkFlow.tickHandle) return;
    WorkFlow.tickHandle = setInterval(() => {
      document.querySelectorAll('[data-work-timer]').forEach((el) => {
        const started = Number(el.dataset.workTimer);
        if (!started) return;
        el.textContent = WorkFlow.fmtElapsed(Date.now() - started);
      });
    }, 1000);
  },

  async refreshMine() {
    if (!state.user) return;
    try {
      const d = await api('GET', '/api/v1/work/mine');
      const map = {};
      (d.sessions || []).forEach((s) => { map[s.orderId] = s; });
      state.myWork = map;
      WorkFlow.syncButtons();
      WorkFlow.startTick();
    } catch { /* offline-ish; ignore */ }
  },

  /* One consistent button: Start Work / Resume · timer / In progress · @user */
  buttonFor(o, size) {
    if (!o || o.status === 'Completed') return '';
    const sess = WorkFlow.activeFor(o.id);
    const sz = size === 'sm' ? ' btn-sm' : '';
    if (sess && sess.username === state.user.username) {
      return `<button type="button" class="btn btn-work${sz}" data-work-open="${o.id}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 6v6l4 2"/><circle cx="12" cy="12" r="9"/></svg>
        ${esc(t('work.resume'))} <span class="mono" data-work-timer="${sess.startedAt}">${WorkFlow.fmtElapsed(Date.now() - sess.startedAt)}</span>
      </button>`;
    }
    if (sess) {
      return `<button type="button" class="btn btn-work wb-busy${sz}" disabled title="${esc(t('work.busy'))}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 6v6l4 2"/><circle cx="12" cy="12" r="9"/></svg>
        ${esc(t('work.inprogress'))} @${esc(sess.username)}
      </button>`;
    }
    if (!WorkFlow.mine(o)) return '';
    return `<button type="button" class="btn btn-work${sz}" data-work-open="${o.id}">
      <svg viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M8 5.5v13l11-6.5z"/></svg>
      ${esc(t('work.start'))}
    </button>`;
  },

  syncButtons() {
    document.querySelectorAll('[data-work-slot]').forEach((slot) => {
      const id = Number(slot.dataset.workSlot);
      const o = (state.orders || []).find((x) => x.id === id);
      const html = WorkFlow.buttonFor(o, slot.dataset.workSize);
      if (html || slot.dataset.workKeep !== '1') slot.innerHTML = html;
    });
  },

  /* ---------- workbench overlay ---------- */

  ensureRoot() {
    let root = document.getElementById('workbench');
    if (!root) {
      root = document.createElement('div');
      root.id = 'workbench';
      root.className = 'wb-root hidden';
      document.body.appendChild(root);
      root.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && !root.classList.contains('hidden')) WorkFlow.close();
        if (e.key === 'Enter' && !e.target.closest('input, textarea, select')) {
          const next = root.querySelector('#wb-next');
          if (next && !next.disabled) { e.preventDefault(); next.click(); }
        }
      });
    }
    return root;
  },

  close() {
    const root = WorkFlow.ensureRoot();
    root.classList.add('hidden');
    root.innerHTML = '';
    WorkFlow.session = null;
    WorkFlow.order = null;
    WorkFlow.refreshMine();
  },

  async open(orderId) {
    await WorkFlow.loadFlow();
    const o = (state.orders || []).find((x) => x.id === orderId) || (await api('GET', `/api/v1/orders/${orderId}`)).order;
    WorkFlow.order = o;
    const root = WorkFlow.ensureRoot();
    root.classList.remove('hidden');
    const sess = WorkFlow.activeFor(o.id);
    if (sess && sess.username === state.user.username) {
      WorkFlow.session = sess;
      await WorkFlow.enterWizard();
    } else {
      WorkFlow.renderReady();
    }
    WorkFlow.startTick();
  },

  renderReady() {
    const o = WorkFlow.order;
    const root = WorkFlow.ensureRoot();
    root.innerHTML = `
      <div class="wb-backdrop" data-wb-close></div>
      <div class="wb-panel" role="dialog" aria-modal="true" aria-label="${esc(t('work.title'))}">
        <div class="wb-head">
          <div>
            <div class="eyebrow">${esc(t('work.title'))}</div>
            <h2 class="mono">${esc(o.orderNumber)}</h2>
            <p class="wb-sub">${esc(o.customerName || '')}${o.device ? ' · ' + esc(o.device) : ''}</p>
          </div>
          <button type="button" class="icon-btn" data-wb-close aria-label="Close">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
          </button>
        </div>
        <div class="wb-ready">
          <p>${esc(t('work.readySub'))}</p>
          <ol class="wb-steplist">
            ${WorkFlow.steps().map((s, i) => `<li><span class="wb-n">${i + 1}</span><div><strong>${esc(WorkFlow.stepTitle(s))}</strong><span>${esc(WorkFlow.stepDesc(s))}</span></div></li>`).join('')}
          </ol>
          <div class="wb-ready-foot">
            <span class="rel">${esc(t('work.target'))} ${fmtTime(o.targetCompletionAt)} <span class="sub">${esc(relTime(o.targetCompletionAt))}</span></span>
            <button type="button" class="btn btn-work btn-lg" id="wb-start">
              <svg viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M8 5.5v13l11-6.5z"/></svg>
              ${esc(t('work.start'))}
            </button>
          </div>
        </div>
      </div>`;
    WorkFlow.bindClose(root);
    root.querySelector('#wb-start').addEventListener('click', async (e) => {
      const b = e.currentTarget;
      b.disabled = true;
      const d = await api('POST', `/api/v1/orders/${o.id}/work/start`);
      if (d.error) { toast(d.error.message || d.error.code, true); b.disabled = false; return; }
      WorkFlow.session = d.session;
      await WorkFlow.enterWizard();
    });
  },

  async enterWizard() {
    WorkFlow.step = WorkFlow.session.step || 0;
    WorkFlow.pick = null;
    WorkFlow.manuals = null;
    try { WorkFlow.checks = JSON.parse(WorkFlow.session.checks || '{}'); } catch { WorkFlow.checks = {}; }
    await WorkFlow.loadStepData();
    WorkFlow.renderWizard();
  },

  async loadStepData() {
    const o = WorkFlow.order;
    const step = WorkFlow.steps()[WorkFlow.step];
    if (step.pick && WorkFlow.pick === null) {
      try {
        const d = await api('GET', `/api/v1/orders/${o.id}/checklist`);
        WorkFlow.pick = d.items || [];
      } catch { WorkFlow.pick = []; }
    }
    if (step.pick && WorkFlow.manuals === null) {
      try {
        const dm = await api('GET', `/api/v1/orders/${o.id}/manuals`);
        WorkFlow.manuals = dm.manuals || [];
      } catch { WorkFlow.manuals = []; }
    }
    if (step.qcGate) {
      try {
        const d = await api('GET', `/api/v1/orders/${o.id}`);
        WorkFlow.qcPassed = ((d.order && d.order.qcChecks) || []).some((q) => q.status === 'PASS');
        if (d.order) WorkFlow.order = d.order;
      } catch { /* keep last known */ }
    }
  },

  checksDone(stepKey) {
    const step = WorkFlow.steps().find((s) => s.key === stepKey);
    const mine = WorkFlow.checks[stepKey] || {};
    return step.checks.every((c) => mine[c.key]);
  },

  gateOk() {
    const step = WorkFlow.steps()[WorkFlow.step];
    if (!WorkFlow.checksDone(step.key)) return false;
    if (step.pick && WorkFlow.pick && WorkFlow.pick.length && !WorkFlow.pick.every((i) => i.checkedBy)) return false;
    if (step.pick && !WorkFlow.manualsDone()) return false;
    if (step.qcGate && !WorkFlow.qcPassed) return false;
    return true;
  },

  gateHint() {
    const step = WorkFlow.steps()[WorkFlow.step];
    if (step.qcGate && !WorkFlow.qcPassed) return t('work.qcwait');
    const left = [];
    const mine = WorkFlow.checks[step.key] || {};
    const n = step.checks.filter((c) => !mine[c.key]).length;
    if (n) left.push(`${n} ${t('work.checksLeft')}`);
    if (step.pick && WorkFlow.pick && WorkFlow.pick.length) {
      const p = WorkFlow.pick.filter((i) => !i.checkedBy).length;
      if (p) left.push(`${p} ${t('work.pickLeft')}`);
    }
    if (step.pick && !WorkFlow.manualsDone()) {
      const m = WorkFlow.manualsLeft();
      if (m) left.push(`${m} ${t('work.manualLeft')}`);
    }
    return left.join(' · ');
  },

  manualsDone() {
    const ms = WorkFlow.manuals || [];
    return ms.every((m) => !m.total || (m.answered === m.total && !m.failed && !m.flagged));
  },

  manualsLeft() {
    return (WorkFlow.manuals || []).reduce(
      (n, m) => n + (m.blocks || []).filter((b) => b.answer !== 'YES').length, 0);
  },

  manualsHtml() {
    const ms = WorkFlow.manuals || [];
    if (!ms.length) return '';
    const tot = ms.reduce((n, m) => n + (m.total || 0), 0);
    const ans = ms.reduce((n, m) => n + (m.answered || 0), 0);
    return `<div class="wb-pick wb-manuals">
      <div class="wb-pick-head">${esc(t('work.manuals'))} <span class="count">${ans}/${tot}</span></div>
      ${ms.map((m) => `
        <div class="wb-manual">
          <div class="wb-manual-head"><span class="mono">${esc(m.productCode)}</span> ${esc(m.productName)} <span class="count">${m.answered}/${m.total}</span></div>
          ${(m.blocks || []).map((b) => `
            <div class="wb-block${b.answer ? ' ans-' + String(b.answer).toLowerCase() : ''}">
              <span class="wb-ctext">${esc(b.title)}</span>
              <span class="wb-blockbtns">
                <button type="button" class="answer-btn yes${b.answer === 'YES' ? ' on' : ''}" data-wb-mblock="${m.id}" data-wb-block="${b.id}" data-answer="YES" title="Yes">✓</button>
                <button type="button" class="answer-btn no${b.answer === 'NO' ? ' on' : ''}" data-wb-mblock="${m.id}" data-wb-block="${b.id}" data-answer="NO" title="No">✗</button>
              </span>
            </div>`).join('')}
        </div>`).join('')}
    </div>`;
  },

  renderWizard() {
    const o = WorkFlow.order;
    const s = WorkFlow.session;
    const root = WorkFlow.ensureRoot();
    const step = WorkFlow.steps()[WorkFlow.step];
    const mine = WorkFlow.checks[step.key] || {};
    const all = WorkFlow.steps();
    const last = WorkFlow.step === all.length - 1;

    root.innerHTML = `
      <div class="wb-backdrop"></div>
      <div class="wb-panel" role="dialog" aria-modal="true" aria-label="${esc(t('work.title'))}">
        <div class="wb-head">
          <div>
            <div class="eyebrow">${esc(t('work.title'))} · #${o.id}</div>
            <h2 class="mono">${esc(o.orderNumber)}</h2>
            <p class="wb-sub">${esc(o.customerName || '')}${o.device ? ' · ' + esc(o.device) : ''} · ${badge('status-' + statusClass(o.status), statusLabel(o.status))}</p>
          </div>
          <div class="wb-head-right">
            <span class="wb-timer mono" title="${esc(t('work.elapsed'))}">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.2 1.8"/></svg>
              <span data-work-timer="${s.startedAt}">${WorkFlow.fmtElapsed(Date.now() - s.startedAt)}</span>
            </span>
            <button type="button" class="btn btn-ghost btn-sm" id="wb-clockout">${esc(t('work.clockout'))}</button>
            <button type="button" class="icon-btn" data-wb-close aria-label="Close" title="${esc(t('work.exitNow'))}">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
            </button>
          </div>
        </div>

        <ol class="wb-steps">
          ${all.map((st, i) => `
            <li class="${i < WorkFlow.step ? 'done' : i === WorkFlow.step ? 'current' : 'todo'}">
              <span class="wb-dot">${i < WorkFlow.step
                ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>'
                : i + 1}</span>
              <span class="wb-lbl">${esc(st.title || t('work.short.' + st.key))}</span>
            </li>`).join('')}
        </ol>

        <div class="wb-body">
          <h3>${esc(WorkFlow.stepTitle(step))}</h3>
          <p class="wb-desc">${esc(WorkFlow.stepDesc(step))}</p>

          ${step.pick ? WorkFlow.pickHtml() : ''}
          ${step.pick ? WorkFlow.manualsHtml() : ''}

          <div class="wb-checks">
            ${step.checks.map((c) => `
              <label class="wb-check${mine[c.key] ? ' on' : ''}">
                <input type="checkbox" data-wb-check="${c.key}" ${mine[c.key] ? 'checked' : ''}>
                <span class="wb-box" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg></span>
                <span class="wb-ctext">${esc(WorkFlow.checkLabel(c))}</span>
              </label>`).join('')}
          </div>

          ${step.qcGate ? `
            <div class="wb-qcnote ${WorkFlow.qcPassed ? 'ok' : 'wait'}">
              ${WorkFlow.qcPassed ? esc(t('work.qcok')) : esc(t('work.qcwait'))}
              ${WorkFlow.qcPassed ? '' : `<span class="wb-qcacts"><button type="button" class="btn btn-ghost btn-sm" id="wb-qcrefresh">${esc(t('btn.refresh'))}</button><button type="button" class="btn btn-ghost btn-sm" id="wb-exit">${esc(t('work.exitNow'))}</button></span>`}
            </div>` : ''}
        </div>

        <div class="wb-foot">
          <button type="button" class="btn btn-ghost" id="wb-back" ${WorkFlow.step === 0 ? 'disabled' : ''}>← ${esc(t('work.back'))}</button>
          <span class="wb-hint rel">${esc(WorkFlow.gateHint())}</span>
          <button type="button" class="btn btn-work btn-lg" id="wb-next" ${WorkFlow.gateOk() ? '' : 'disabled'}>
            ${esc(last ? t('work.complete') : t('work.next'))} <span aria-hidden="true">→</span>
          </button>
        </div>
      </div>`;

    WorkFlow.bindWizard(root);
  },

  pickHtml() {
    if (!WorkFlow.pick || !WorkFlow.pick.length) return `<p class="wb-pickempty rel">${esc(t('work.pickempty'))}</p>`;
    return `<div class="wb-pick">
      <div class="wb-pick-head">${esc(t('work.picklist'))} <span class="count">${WorkFlow.pick.filter((i) => i.checkedBy).length}/${WorkFlow.pick.length}</span></div>
      ${WorkFlow.pick.map((i) => `
        <label class="wb-check pick${i.checkedBy ? ' on' : ''}">
          <input type="checkbox" data-wb-pick="${i.id}" ${i.checkedBy ? 'checked' : ''}>
          <span class="wb-box" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg></span>
          <span class="wb-ctext"><span class="mono">${esc(i.artikel || '')}</span> ${esc(i.omschrijving || '')} <span class="sub">×${i.aantal}${i.locatie ? ' · ' + esc(i.locatie) : ''}</span>${i.checkedBy ? ` <span class="sub">@${esc(i.checkedBy)}</span>` : ''}</span>
        </label>`).join('')}
    </div>`;
  },

  bindClose(root) {
    root.querySelectorAll('[data-wb-close]').forEach((el) => el.addEventListener('click', () => WorkFlow.close()));
  },

  persist() {
    const o = WorkFlow.order;
    return api('POST', `/api/v1/orders/${o.id}/work/update`, { step: WorkFlow.step, checks: WorkFlow.checks });
  },

  bindWizard(root) {
    WorkFlow.bindClose(root);

    root.querySelectorAll('[data-wb-check]').forEach((inp) => {
      inp.addEventListener('change', () => {
        const key = WorkFlow.steps()[WorkFlow.step].key;
        WorkFlow.checks[key] = WorkFlow.checks[key] || {};
        if (inp.checked) WorkFlow.checks[key][inp.dataset.wbCheck] = true;
        else delete WorkFlow.checks[key][inp.dataset.wbCheck];
        inp.closest('.wb-check').classList.toggle('on', inp.checked);
        WorkFlow.persist();
        WorkFlow.refreshFoot(root);
      });
    });

    root.querySelectorAll('[data-wb-pick]').forEach((inp) => {
      inp.addEventListener('change', async () => {
        inp.disabled = true;
        const d = await api('POST', `/api/v1/checklist/${inp.dataset.wbPick}/${inp.checked ? 'tick' : 'untick'}`);
        inp.disabled = false;
        if (d.error) { toast(d.error.message || d.error.code, true); inp.checked = !inp.checked; return; }
        const item = WorkFlow.pick.find((i) => String(i.id) === inp.dataset.wbPick);
        if (item) item.checkedBy = inp.checked ? state.user.username : '';
        inp.closest('.wb-check').classList.toggle('on', inp.checked);
        const head = root.querySelector('.wb-pick-head .count');
        if (head) head.textContent = `${WorkFlow.pick.filter((i) => i.checkedBy).length}/${WorkFlow.pick.length}`;
        WorkFlow.refreshFoot(root);
      });
    });

    root.querySelectorAll('[data-wb-block]').forEach((btn) => {
      btn.addEventListener('click', async () => {
        btn.disabled = true;
        const d = await api('POST', `/api/v1/manuals/${btn.dataset.wbMblock}/blocks/${btn.dataset.wbBlock}/answer`, { answer: btn.dataset.answer });
        if (d.error) { toast(d.error.message || d.error.code, true); btn.disabled = false; return; }
        try {
          const dm = await api('GET', `/api/v1/orders/${WorkFlow.order.id}/manuals`);
          WorkFlow.manuals = dm.manuals || [];
        } catch { /* keep */ }
        WorkFlow.renderWizard();
      });
    });

    const exitBtn = root.querySelector('#wb-exit');
    if (exitBtn) exitBtn.addEventListener('click', () => {
      WorkFlow.close();
      toast(t('work.exitToast'));
    });

    const qcRefresh = root.querySelector('#wb-qcrefresh');
    if (qcRefresh) qcRefresh.addEventListener('click', async () => {
      qcRefresh.disabled = true;
      await WorkFlow.loadStepData();
      WorkFlow.renderWizard();
    });

    root.querySelector('#wb-back').addEventListener('click', async () => {
      if (WorkFlow.step === 0) return;
      WorkFlow.step -= 1;
      await WorkFlow.persist();
      await WorkFlow.loadStepData();
      WorkFlow.renderWizard();
    });

    root.querySelector('#wb-clockout').addEventListener('click', async () => {
      const d = await api('POST', `/api/v1/orders/${WorkFlow.order.id}/work/end`, { completed: false });
      if (d.error) { toast(d.error.message || d.error.code, true); return; }
      const el = WorkFlow.fmtElapsed(Date.now() - WorkFlow.session.startedAt);
      WorkFlow.close();
      toast(`${t('work.clockedout')} · ${el}`);
    });

    root.querySelector('#wb-next').addEventListener('click', async () => {
      const btn = root.querySelector('#wb-next');
      const step = WorkFlow.steps()[WorkFlow.step];
      const target = step.next(WorkFlow.order);
      if (target) {
        btn.disabled = true;
        const ok = await act('POST', `/api/v1/orders/${WorkFlow.order.id}/transition`, { status: target }, null, { quiet: true });
        if (!ok) { btn.disabled = false; return; }
        WorkFlow.order.status = target;
      }
      if (WorkFlow.step === WorkFlow.steps().length - 1) {
        const d = await api('POST', `/api/v1/orders/${WorkFlow.order.id}/work/end`, { completed: true });
        if (d.error) { toast(d.error.message || d.error.code, true); btn.disabled = false; return; }
        WorkFlow.renderDone();
        return;
      }
      WorkFlow.step += 1;
      await WorkFlow.persist();
      await WorkFlow.loadStepData();
      WorkFlow.renderWizard();
    });
  },

  refreshFoot(root) {
    const next = root.querySelector('#wb-next');
    const hint = root.querySelector('.wb-hint');
    if (next) next.disabled = !WorkFlow.gateOk();
    if (hint) hint.textContent = WorkFlow.gateHint();
  },

  renderDone() {
    const o = WorkFlow.order;
    const s = WorkFlow.session;
    const root = WorkFlow.ensureRoot();
    const elapsed = WorkFlow.fmtElapsed((s.endedAt || Date.now()) - s.startedAt);
    root.innerHTML = `
      <div class="wb-backdrop"></div>
      <div class="wb-panel wb-done" role="dialog" aria-modal="true">
        <div class="wb-done-card">
          <span class="wb-done-ico" aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>
          </span>
          <h2>${esc(t('work.doneTitle'))}</h2>
          <p class="mono strong">${esc(o.orderNumber)}</p>
          <p class="wb-sub">${esc(t('work.doneSub'))}</p>
          <div class="wb-done-time mono">${elapsed}</div>
          <p class="rel">${esc(t('work.elapsed'))}</p>
          <div class="wb-done-actions">
            <button type="button" class="btn btn-ghost" id="wb-done-close">${esc(t('work.backToOrders'))}</button>
            <button type="button" class="btn btn-primary" id="wb-done-view">${esc(t('work.viewOrder'))}</button>
          </div>
        </div>
      </div>`;
    root.querySelector('#wb-done-close').addEventListener('click', () => { WorkFlow.close(); location.hash = '#/orders'; });
    root.querySelector('#wb-done-view').addEventListener('click', () => { const id = o.id; WorkFlow.close(); openDetail(id); });
  },
};

/* Global entry-point routing: any "Start Work" button anywhere opens the bench. */
document.addEventListener('click', (e) => {
  const b = e.target.closest && e.target.closest('[data-work-open]');
  if (!b) return;
  e.stopPropagation();
  WorkFlow.open(Number(b.dataset.workOpen));
});

window.WorkFlow = WorkFlow;
