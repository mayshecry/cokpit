'use strict';

/* Quality menu — QC cockpit for one month: precision KPIs with month-over-month
   deltas and six-month sparklines, daily pass/fail trend with rolling rate line
   and hover tooltip, animated donut, rework-per-tech table, fail-reason
   breakdown and a latest-checks feed. Month switches slide; numbers morph. */

const Quality = {
  month: null,
  data: null,

  curMonth() {
    const d = new Date();
    return d.toISOString().slice(0, 7);
  },

  async load(dir) {
    if (!Quality.month) Quality.month = Quality.curMonth();
    $('#qc-month').textContent = Quality.month;
    const box = $('#quality-content');
    if (!Quality.data) Quality.skeleton(box);
    let wrap = box.querySelector('.qc-wrap');
    if (dir && wrap) {
      wrap.classList.add(dir > 0 ? 'leave-next' : 'leave-prev');
      await new Promise((r) => setTimeout(r, 170));
    }
    try {
      const d = await api('GET', '/api/v1/stats/qc?month=' + Quality.month);
      Quality.data = d;
      Quality.render(d, dir || 0);
    } catch (err) {
      box.innerHTML = `<p class="muted">Quality stats unavailable: ${esc(err.message)}</p>`;
    }
  },

  skeleton(box) {
    box.innerHTML = `
      <div class="qc-skel" aria-hidden="true">
        <div class="dash-kpis qc-kpis">${'<div class="kpi"><span class="sk sk-lg"></span><span class="sk sk-sm"></span></div>'.repeat(5)}</div>
        <div class="dash-grid qc-grid">
          <div class="card dash-card qc-span2"><span class="sk sk-chart"></span></div>
          <div class="card dash-card"><span class="sk sk-chart"></span></div>
          <div class="card dash-card qc-span2"><span class="sk sk-chart"></span></div>
          <div class="card dash-card"><span class="sk sk-chart"></span></div>
        </div>
      </div>`;
  },

  shift(delta) {
    const [y, m] = Quality.month.split('-').map(Number);
    const d = new Date(Date.UTC(y, m - 1 + delta, 1));
    const next = d.toISOString().slice(0, 7);
    if (delta > 0 && next > Quality.curMonth()) return;
    if (next === Quality.month) return;
    Quality.month = next;
    Quality.load(delta);
  },

  monthName(shift) {
    const [y, m] = Quality.month.split('-').map(Number);
    const d = new Date(Date.UTC(y, m - 1 + (shift || 0), 1));
    return d.toLocaleDateString('en-GB', { month: 'short', year: 'numeric', timeZone: 'UTC' }).toLowerCase();
  },

  pct(x) {
    return x == null ? '—' : x.toFixed(2) + '%';
  },

  deltaHtml(cur, prev, unit, invert) {
    if (cur == null || prev == null || prev === undefined) return '<span class="qc-delta na">no history</span>';
    const d = cur - prev;
    if (Math.abs(d) < 0.005) return '<span class="qc-delta flat" title="versus previous month">→ 0.00' + (unit || ' pp') + '</span>';
    const good = invert ? d < 0 : d > 0;
    const arrow = d > 0 ? '▲' : '▼';
    return `<span class="qc-delta ${good ? 'good' : 'bad'}" title="versus previous month">${arrow} ${Math.abs(d).toFixed(2)}${unit || ' pp'}</span>`;
  },

  sparkSvg(vals, opts) {
    const o = opts || {};
    const w = o.w || 84, h = o.h || 26, pad = 3;
    const pts = [];
    let min = Infinity, max = -Infinity;
    vals.forEach((v) => { if (v != null) { min = Math.min(min, v); max = Math.max(max, v); } });
    if (min === Infinity) return '';
    if (max - min < 0.001) { max += 1; min -= 1; }
    vals.forEach((v, i) => {
      if (v == null) { pts.push(null); return; }
      const x = pad + (i / (vals.length - 1)) * (w - pad * 2);
      const y = pad + (1 - (v - min) / (max - min)) * (h - pad * 2);
      pts.push([x, y]);
    });
    let d = '', pen = false;
    pts.forEach((p) => {
      if (!p) { pen = false; return; }
      d += (pen ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1) + ' ';
      pen = true;
    });
    const last = pts.filter(Boolean).pop();
    return `<svg viewBox="0 0 ${w} ${h}" class="qc-spark ${o.cls || ''}" width="${w}" height="${h}" aria-hidden="true">
      <path d="${d}" fill="none"></path>
      <circle cx="${last[0].toFixed(1)}" cy="${last[1].toFixed(1)}" r="2.4"></circle>
    </svg>`;
  },

  trendSvg(trend) {
    if (!trend || !trend.length) return '';
    const W = 660, H = 200, PL = 30, PR = 34, PB = 26, PT = 12;
    const iw = W - PL - PR, ih = H - PT - PB;
    const max = Math.max(...trend.map((t) => t.pass + t.fail), 1);
    const bw = iw / trend.length;
    let bars = '';
    const pts = [];
    const rolls = [];
    let run = { p: 0, f: 0, n: 0 };
    trend.forEach((t, i) => {
      run.p += t.pass; run.f += t.fail; run.n++;
      if (run.n > 7) { const o = trend[i - 7]; run.p -= o.pass; run.f -= o.fail; run.n--; }
      const tot = run.p + run.f;
      const roll = tot ? (run.p / tot) * 100 : 100;
      rolls.push(roll);
      const x = PL + i * bw;
      const hp = (t.pass / max) * ih;
      const hf = (t.fail / max) * ih;
      const w = Math.max(2, bw * 0.62);
      const ox = x + (bw - w) / 2;
      const tip = `data-d="${t.day}" data-p="${t.pass}" data-f="${t.fail}" data-r="${roll.toFixed(2)}"`;
      bars += `<rect class="qc-bar-p" ${tip} x="${ox.toFixed(1)}" y="${(PT + ih - hp).toFixed(1)}" width="${w.toFixed(1)}" height="${hp.toFixed(1)}" rx="2.5" style="animation-delay:${i * 16}ms"></rect>`;
      if (hf > 0.5) bars += `<rect class="qc-bar-f" ${tip} x="${ox.toFixed(1)}" y="${(PT + ih - hp - hf).toFixed(1)}" width="${w.toFixed(1)}" height="${hf.toFixed(1)}" rx="2.5" style="animation-delay:${i * 16 + 60}ms"></rect>`;
      pts.push([(x + bw / 2).toFixed(1), (PT + ih - (roll / 100) * ih).toFixed(1)]);
    });
    const path = pts.map((p, i) => (i ? 'L' : 'M') + p[0] + ' ' + p[1]).join(' ');
    const ticks = [0, 0.5, 1].map((f) => {
      const y = PT + ih - f * ih;
      return `<line class="qc-grid-l" x1="${PL}" y1="${y}" x2="${W - PR}" y2="${y}"></line>
        <text class="qc-ax" x="${PL - 7}" y="${y + 3.5}" text-anchor="end">${Math.round(f * max)}</text>
        <text class="qc-ax r" x="${W - PR + 7}" y="${y + 3.5}">${Math.round(f * 100)}%</text>`;
    }).join('');
    const every = Math.ceil(trend.length / 8);
    const labels = trend.map((t, i) => (i % every === 0 ? `<text class="qc-ax" x="${PL + i * bw + bw / 2}" y="${H - 8}" text-anchor="middle">${t.day.slice(8)}</text>` : '')).join('');
    return `<svg viewBox="0 0 ${W} ${H}" class="qc-trend" role="img" aria-label="Daily QC checks with rolling pass rate">
      ${ticks}${bars}
      <path class="qc-line" d="${path}" fill="none"></path>
      ${labels}
    </svg>`;
  },

  donutSvg(pass, fail, rate) {
    const C = 2 * Math.PI * 52;
    const passLen = pass + fail ? (pass / (pass + fail)) * C : 0;
    return `
      <svg viewBox="0 0 140 140" class="donut qc-donut" role="img" aria-label="Pass versus fail share">
        <circle r="52" cx="70" cy="70" fill="none" stroke="var(--bad)" stroke-width="15"></circle>
        <circle r="52" cx="70" cy="70" fill="none" stroke="var(--good)" stroke-width="15"
          stroke-dasharray="${Math.max(0, passLen - 2).toFixed(1)} ${C}" transform="rotate(-90 70 70)" class="qc-donut-arc"></circle>
        <text x="70" y="67" class="donut-n" text-anchor="middle" data-count="${rate.toFixed(2)}" data-dec="2" data-suffix="%">0.00%</text>
        <text x="70" y="86" class="donut-t" text-anchor="middle">first-pass</text>
      </svg>`;
  },

  render(d, dir) {
    const rows = d.perUser || [];
    const pass = d.pass || 0;
    const fail = d.fail || 0;
    const tot = d.checks || 0;
    const rate = d.rate || 0;
    const prevRate = d.prevRate == null ? null : d.prevRate;
    const worst = rows.length && rows[0].fail > 0 ? rows[0].username : null;
    const name = (u) => (d.userNames || {})[u] || u;
    const trend = d.trend || [];
    const reasons = d.reasons || [];
    const recent = d.recent || [];
    const history = d.history || [];
    const maxReason = reasons.length ? reasons[0].count : 1;
    const maxFail = Math.max(...rows.map((r) => r.fail), 1);
    const histRates = history.map((h) => h.rate);

    const kpis = `
      <div class="dash-kpis qc-kpis">
        <div class="kpi"><span class="kpi-v" data-count="${tot}">0</span><span class="kpi-l">QC checks</span></div>
        <div class="kpi ${rate >= 95 ? 'good' : rate >= 90 ? '' : 'bad'}"><span class="kpi-v" data-count="${rate.toFixed(2)}" data-dec="2" data-suffix="%">0%</span><span class="kpi-l">first-pass yield</span>${Quality.sparkSvg(histRates, { cls: 'sp-good' })}</div>
        <div class="kpi ${fail ? 'bad' : 'good'}"><span class="kpi-v" data-count="${fail}">0</span><span class="kpi-l">rework items</span></div>
        <div class="kpi"><span class="kpi-v" data-count="${rows.length}">0</span><span class="kpi-l">techs checked</span></div>
        <div class="kpi qc-delta-tile">${prevRate == null ? '<span class="kpi-v dim">—</span>' : `<span class="kpi-v ${rate - prevRate >= 0 ? 'good-v' : 'bad-v'}">${(rate - prevRate >= 0 ? '+' : '−') + Math.abs(rate - prevRate).toFixed(2)}</span>`}<span class="kpi-l">pp vs ${esc(Quality.monthName(-1))}</span></div>
      </div>`;

    const trendCard = `
      <section class="card dash-card qc-span2 qc-trendcard">
        <header class="dash-card-head"><h3>Check volume &amp; rolling pass rate</h3>
          <span class="muted">per day · ${esc(Quality.month)} · line = 7-day rolling</span></header>
        ${trend.length ? Quality.trendSvg(trend) : '<p class="muted dash-empty">No QC checks recorded this month yet.</p>'}
        <div class="qc-legend"><i class="lg good"></i><span>pass</span><i class="lg bad"></i><span>fail</span><i class="lg line"></i><span>rolling pass rate</span></div>
        <div class="qc-tip" hidden></div>
      </section>`;

    const donutCard = `
      <section class="card dash-card">
        <header class="dash-card-head"><h3>Good vs wrong</h3><span class="muted">${esc(Quality.month)}</span></header>
        ${tot ? `
        <div class="donut-wrap">
          ${Quality.donutSvg(pass, fail, rate)}
          <ul class="donut-legend">
            <li><i class="lg good"></i><span>Passed</span><strong data-count="${pass}">0</strong></li>
            <li><i class="lg bad"></i><span>Failed</span><strong data-count="${fail}">0</strong></li>
            <li><i class="lg mute"></i><span>Prev month</span><strong>${Quality.pct(prevRate)}</strong></li>
          </ul>
        </div>` : '<p class="muted dash-empty">Nothing checked yet this month.</p>'}
      </section>`;

    const tableCard = `
      <section class="card dash-card qc-span2">
        <header class="dash-card-head"><h3>Rework per tech</h3><span class="muted">who the fails landed on, ${esc(Quality.month)}</span></header>
        ${rows.length ? `
        <table class="dash-table qc-table">
          <thead><tr><th>Tech</th><th class="num">Checks</th><th class="num">Pass</th><th class="num">Fail</th><th class="num">Fail %</th><th class="num">vs prev</th><th class="qc-sparkhead">fail % · 6 mo</th><th class="qc-barcol">fail share</th><th></th></tr></thead>
          <tbody>${rows.map((r, i) => {
            const t = r.pass + r.fail;
            const fp = t ? (r.fail / t) * 100 : 0;
            return `<tr style="animation-delay:${i * 45}ms">
              <td class="qc-tech">${esc(name(r.username))}</td>
              <td class="num">${t}</td>
              <td class="num">${r.pass}</td>
              <td class="num ${r.fail ? 'strong bad-num' : ''}">${r.fail}</td>
              <td class="num">${fp.toFixed(2)}%</td>
              <td class="num">${r.prevRate == null ? '<span class="qc-delta na">new</span>' : Quality.deltaHtml(fp, 100 - r.prevRate, ' pp', true)}</td>
              <td class="qc-sparkcell">${Quality.sparkSvg(r.spark || [], { cls: 'sp-bad', w: 76, h: 22 })}</td>
              <td class="qc-barcol"><span class="qc-minibar"><i style="width:${((r.fail / maxFail) * 100).toFixed(0)}%;animation-delay:${i * 45 + 120}ms"></i></span></td>
              <td>${r.username === worst ? '<span class="badge sla-BREACHED"><span class="dot"></span>most rework</span>' : ''}</td>
            </tr>`;
          }).join('')}</tbody>
        </table>` : '<p class="muted dash-empty">No QC checks recorded this month yet.</p>'}
      </section>`;

    const reasonCard = `
      <section class="card dash-card">
        <header class="dash-card-head"><h3>Why it fails</h3><span class="muted">top reasons, ${esc(Quality.month)}</span></header>
        ${reasons.length ? `<ul class="qc-reasons">${reasons.map((r, i) => `
          <li style="animation-delay:${i * 50}ms">
            <div class="qc-r-head"><span>${esc(r.reason)}</span><strong>${r.count}</strong></div>
            <span class="qc-r-bar"><i style="width:${((r.count / maxReason) * 100).toFixed(0)}%;animation-delay:${i * 50 + 80}ms"></i></span>
          </li>`).join('')}</ul>` : '<p class="muted dash-empty">No fails recorded — nothing to explain.</p>'}
      </section>`;

    const feedCard = `
      <section class="card dash-card qc-span3">
        <header class="dash-card-head"><h3>Latest checks</h3><span class="muted">newest first · inspector → tech · ←/→ switches month</span></header>
        ${recent.length ? `<ul class="qc-feed">${recent.map((r, i) => `
          <li style="animation-delay:${i * 40}ms">
            <span class="qc-feed-dot ${r.status === 'PASS' ? 'ok' : 'no'}"></span>
            <span class="mono qc-feed-order">${esc(r.orderNumber)}</span>
            <span class="qc-feed-who">${esc(name(r.tech))}</span>
            <span class="muted qc-feed-insp">by ${esc(name(r.inspector))}</span>
            ${r.reason ? `<span class="qc-feed-reason">${esc(r.reason)}</span>` : '<span class="qc-feed-reason ok-t">clean pass</span>'}
            <span class="muted qc-feed-when">${esc(relTime(new Date(r.at).toISOString()))}</span>
          </li>`).join('')}</ul>` : '<p class="muted dash-empty">No QC checks recorded this month yet.</p>'}
      </section>`;

    const box = $('#quality-content');
    const enterCls = dir > 0 ? 'enter-next' : dir < 0 ? 'enter-prev' : '';
    box.innerHTML = `<div class="qc-wrap ${enterCls}">${kpis}<div class="dash-grid qc-grid">${trendCard}${donutCard}${tableCard}${reasonCard}${feedCard}</div></div>`;
    hydrateCounts(box);
    Quality.bindTip(box);

    // animate donut arc + rolling-rate line + sparklines once mounted
    requestAnimationFrame(() => {
      const arc = box.querySelector('.qc-donut-arc');
      if (arc && arc.getTotalLength) {
        const len = arc.getTotalLength();
        if (len) {
          const target = arc.getAttribute('stroke-dasharray');
          arc.style.strokeDasharray = `0 ${len}`;
          arc.getBoundingClientRect();
          arc.style.transition = 'stroke-dasharray 900ms cubic-bezier(.22,.9,.28,1)';
          requestAnimationFrame(() => { arc.style.strokeDasharray = target; });
        }
      }
      box.querySelectorAll('.qc-line, .qc-spark path').forEach((line) => {
        if (!line.getTotalLength) return;
        const L = line.getTotalLength();
        line.style.strokeDasharray = L;
        line.style.strokeDashoffset = L;
        line.getBoundingClientRect();
        line.style.transition = 'stroke-dashoffset 1000ms cubic-bezier(.35,.8,.3,1) 200ms';
        requestAnimationFrame(() => { line.style.strokeDashoffset = '0'; });
      });
    });
  },

  bindTip(box) {
    const card = box.querySelector('.qc-trendcard');
    const tip = box.querySelector('.qc-tip');
    const svg = box.querySelector('.qc-trend');
    if (!card || !tip || !svg) return;
    svg.addEventListener('pointermove', (e) => {
      const r = e.target.closest('rect[data-d]');
      if (!r) { tip.hidden = true; return; }
      const cb = card.getBoundingClientRect();
      tip.innerHTML = `<strong>${r.dataset.d}</strong><span>${r.dataset.p} pass · ${r.dataset.f} fail</span><em>rolling ${r.dataset.r}%</em>`;
      tip.hidden = false;
      const x = Math.min(Math.max(e.clientX - cb.left + 14, 8), cb.width - 150);
      const y = Math.min(Math.max(e.clientY - cb.top - 14, 8), cb.height - 70);
      tip.style.transform = `translate(${x}px, ${y}px)`;
    });
    svg.addEventListener('pointerleave', () => { tip.hidden = true; });
  },
};

window.Quality = Quality;

$('#qc-prev').addEventListener('click', () => Quality.shift(-1));
$('#qc-next').addEventListener('click', () => Quality.shift(1));

document.addEventListener('keydown', (e) => {
  if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
  const pane = $('#view-quality');
  if (!pane || pane.classList.contains('hidden')) return;
  const t = document.activeElement;
  if (t && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)) return;
  if (!$('#modal-root').classList.contains('hidden') || !$('#palette-root').classList.contains('hidden')) return;
  if (e.key === 'ArrowLeft') Quality.shift(-1); else Quality.shift(1);
});
