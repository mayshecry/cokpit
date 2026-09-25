'use strict';

/* QC bench — one continuous flow for working the QC_Review queue:
   queue rail on the left, the checking process in the middle, verdict with
   structured fail reasons, then the next order slides in. Built for volume:
   keyboard-first (P/F verdict, Enter submit, arrows walk the queue). */

const QCBench = {
  queue: [],
  reasons: [],
  idx: 0,
  step: 0,
  checks: {},          // key -> bool for the current order
  verdict: null,       // 'PASS' | 'FAIL'
  reason: '',
  note: '',
  sit: { n: 0, pass: 0, fail: 0, streak: 0, best: 0, startedAt: Date.now() },
  poll: null,

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
    if (!QCBench.reasons.length) {
      try { QCBench.reasons = (await api('GET', '/api/v1/qc/reasons')).reasons || []; } catch (e) { QCBench.reasons = []; }
    }
    QCBench.bindOnce();
    await QCBench.refresh(false);
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
      const prevId = QCBench.queue[QCBench.idx] && QCBench.queue[QCBench.idx].id;
      QCBench.queue = d.queue || [];
      if (prevId != null) {
        const at = QCBench.queue.findIndex((q) => q.id === prevId);
        QCBench.idx = at >= 0 ? at : Math.min(QCBench.idx, Math.max(0, QCBench.queue.length - 1));
      } else {
        QCBench.idx = Math.min(QCBench.idx, Math.max(0, QCBench.queue.length - 1));
      }
      QCBench.render();
    } catch (err) {
      if (!quiet) $('#qcbench-content').innerHTML = `<p class="muted">Queue unavailable: ${esc(err.message)}</p>`;
    }
  },

  resetOrderState() {
    QCBench.step = 0;
    QCBench.checks = {};
    QCBench.verdict = null;
    QCBench.reason = '';
    QCBench.note = '';
  },

  cur() { return QCBench.queue[QCBench.idx] || null; },

  sitLabel() {
    const s = QCBench.sit;
    const rate = s.n ? ((s.pass / s.n) * 100).toFixed(1) + '%' : '—';
    return `${s.n} checked · ${rate} pass · streak ${s.streak}`;
  },

  render() {
    const box = $('#qcbench-content');
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
      $('#qcb-again').addEventListener('click', () => QCBench.refresh(false));
      return;
    }
    const waitH = (Date.now() - q.updatedAt) / 3600000;
    const breach = q.targetAt < Date.now();
    box.innerHTML = `
      <div class="qcb-wrap">
        <aside class="qcb-rail">
          <header class="qcb-rail-head">
            <span>Queue</span><strong class="mono">${QCBench.queue.length} waiting</strong>
          </header>
          <ul class="qcb-list">
            ${QCBench.queue.map((it, i) => `
              <li class="qcb-item ${i === QCBench.idx ? 'active' : ''} ${it.targetAt < Date.now() ? 'breached' : ''}" data-i="${i}">
                <span class="mono qcb-item-num">${esc(it.orderNumber)}</span>
                <span class="qcb-item-meta">${esc(it.customer || '—')} · ${esc(it.device || '—')}</span>
                <span class="qcb-item-foot mono">${it.tech ? '@' + esc(it.tech) : 'unassigned'} · ${waitLabel(it.updatedAt)} in queue</span>
              </li>`).join('')}
          </ul>
        </aside>
        <section class="qcb-bench" data-order="${q.id}">
          <header class="qcb-head">
            <div>
              <div class="qcb-ord mono">${esc(q.orderNumber)}</div>
              <div class="qcb-meta">${esc(q.customer || '—')} · ${esc(q.device || '—')} · tech ${q.tech ? '@' + esc(q.tech) : '—'}</div>
            </div>
            <div class="qcb-head-right">
              <span class="qcb-wait mono ${breach ? 'bad-t' : ''}">${waitLabel(q.updatedAt)} in queue${breach ? ' · past target' : ''}</span>
              <span class="qcb-pos mono">${QCBench.idx + 1} / ${QCBench.queue.length}</span>
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
              <p class="muted qcb-hint">Tick everything before moving on — untick to flag a problem.</p>
              <ul class="qcb-checks">
                ${QCBench.STEPS[QCBench.step].checks.map(([k, label]) => `
                  <li><label class="qcb-check ${QCBench.checks[k] ? 'on' : ''}">
                    <input type="checkbox" data-k="${k}" ${QCBench.checks[k] ? 'checked' : ''}>
                    <span class="qcb-box" aria-hidden="true"></span>
                    <span>${esc(label)}</span>
                  </label></li>`).join('')}
              </ul>` : QCBench.verdictHtml()}
          </div>
          <footer class="qcb-foot">
            <button type="button" class="btn btn-ghost" id="qcb-back" ${QCBench.step === 0 ? 'disabled' : ''}>← Back</button>
            <span class="qcb-keys muted">P pass · F fail · Enter submit · ↑/↓ queue</span>
            ${QCBench.step < 3
              ? `<button type="button" class="btn btn-primary" id="qcb-next" ${QCBench.stepDone() ? '' : 'disabled'}>Next step →</button>`
              : `<button type="button" class="btn btn-primary" id="qcb-submit" ${QCBench.canSubmit() ? '' : 'disabled'}>Submit & next →</button>`}
          </footer>
        </section>
      </div>`;
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
        <button type="button" class="qcb-v pass ${v === 'PASS' ? 'sel' : ''}" data-v="PASS">✓ Pass<span>order completes</span></button>
        <button type="button" class="qcb-v fail ${v === 'FAIL' ? 'sel' : ''}" data-v="FAIL">✕ Fail<span>back to rework</span></button>
      </div>
      ${v === 'FAIL' ? `
        <div class="qcb-reasons" role="radiogroup" aria-label="Fail reason">
          ${QCBench.reasons.map((r, i) => `
            <button type="button" class="chip qcb-reason ${QCBench.reason === r ? 'active' : ''}" data-r="${esc(r)}" title="key ${i + 1}">${esc(r)}</button>`).join('')}
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
        if (k <= QCBench.step) { QCBench.step = k; QCBench.render(); }
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
      if (e.target.closest('#qcb-next')) { QCBench.step = Math.min(3, QCBench.step + 1); QCBench.render(); return; }
      if (e.target.closest('#qcb-submit')) { QCBench.submit(); return; }
      if (e.target.closest('#qcb-again')) { QCBench.refresh(false); return; }
    });
    box.addEventListener('change', (e) => {
      if (!e.target.matches('.qcb-checks input')) return;
      QCBench.checks[e.target.dataset.k] = e.target.checked;
      e.target.closest('.qcb-check').classList.toggle('on', e.target.checked);
      const nxt = $('#qcb-next');
      if (nxt) nxt.disabled = !QCBench.stepDone();
    });
    box.addEventListener('input', (e) => {
      if (e.target.id === 'qcb-note') QCBench.note = e.target.value;
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

  advance() {
    const bench = $('#qcbench-content .qcb-bench');
    if (bench) bench.classList.add('qcb-out');
    setTimeout(async () => {
      QCBench.resetOrderState();
      QCBench.queue.splice(QCBench.idx, 1);
      if (QCBench.idx >= QCBench.queue.length) QCBench.idx = 0;
      QCBench.render();
      const nb = $('#qcbench-content .qcb-bench');
      if (nb) nb.classList.add('qcb-in');
      QCBench.refresh(true);
    }, 240);
  },
};

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
    if (!QCBench.queue.length) return;
    QCBench.idx = (QCBench.idx + (e.key === 'ArrowDown' ? 1 : -1) + QCBench.queue.length) % QCBench.queue.length;
    QCBench.resetOrderState();
    QCBench.render();
    e.preventDefault();
  }
});

$('#qcb-refresh').addEventListener('click', () => QCBench.refresh(false));

window.QCBench = QCBench;
