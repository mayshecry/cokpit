'use strict';

                                                                         

const Dashboard = {
  data: null,

  async load() {
    const el0 = $('#dash');
    if (!Dashboard.data && el0) {
      el0.classList.remove('hidden');
      el0.innerHTML = `
        <div class="dash-kpis">${Array.from({ length: 5 }, () => '<div class="kpi"><span class="sk" style="height:26px;width:64px"></span><span class="sk" style="height:11px;width:80px;margin-top:8px"></span></div>').join('')}</div>
        <div class="dash-grid">${Array.from({ length: 4 }, () => '<div class="card dash-card"><span class="sk" style="height:14px;width:45%"></span><span class="sk" style="height:170px;width:100%;margin-top:12px"></span></div>').join('')}</div>`;
    }
    try {
      const d = await api('GET', '/api/v1/stats/ops?days=14');
      Dashboard.data = d;
      Dashboard.render();
    } catch {
      const el = $('#dash');
      if (el) el.classList.add('hidden');
    }
  },

  render() {
    const el = $('#dash');
    const d = Dashboard.data;
    if (!el || !d) return;
    el.classList.remove('hidden');
    const actives = (d.leaderboard || []).reduce((a, u) => a + (u.active || 0), 0);
    const clockedHtml = Dash.clocked(d);
    const mix = d.slaMix || {};
    const mixTot = (mix.ON_TIME || 0) + (mix.WARNING || 0) + (mix.BREACHED || 0);
    const breachedPct = mixTot ? Math.round(((mix.BREACHED || 0) / mixTot) * 100) : 0;
    const ontimePct = mixTot ? Math.round(((mix.ON_TIME || 0) / mixTot) * 100) : 0;
    const clockVals = (d.clockedPerDay || []).map((x) => x.ms || 0);
    const clockMax = Math.max(3600000, ...clockVals);
    const clockPeak = clockMax === 3600000 && Math.max(...clockVals) === 0 ? 0 : clockMax;

    el.innerHTML = `
      <div class="dash-kpis">
        ${Dash.kpi('Open orders', d.open, d.open ? 'brand' : '', d.openTrend, d.openDelta)}
        ${Dash.kpi('SLA breached', d.slaMix.BREACHED || 0, d.slaMix.BREACHED ? 'bad' : 'good', d.breachTrend, d.breachDelta)}
        ${Dash.kpi('Clocked in', actives, actives ? 'good' : '', d.clockTrend, d.clockDelta)}
        ${Dash.kpi('Completed today', d.completedToday, d.completedToday ? 'good' : '', d.doneTrend, d.doneDelta)}
        ${Dash.kpi('Avg hands-on', Dash.dur(d.handleAvgMs), '', d.avgTrend, d.avgDelta)}
      </div>
      <div class="dash-grid">
        <section class="dash-card dash-card-wide">
          <header class="dash-card-head">
            <div class="dash-card-title"><h3>Volume</h3><p class="dash-card-sub">Completed vs received · last ${d.days} days</p></div>
            <div class="dash-legend"><span><i class="sw a"></i>Completed</span><span><i class="sw b"></i>Received</span></div>
          </header>
          ${Dash.bars(d)}
        </section>
        <section class="dash-card">
          <header class="dash-card-head">
            <div class="dash-card-title"><h3>SLA health</h3><p class="dash-card-sub">Open orders by status · live</p></div>
            <span class="dash-chip ${breachedPct > 30 ? 'chip-bad' : breachedPct > 0 ? 'chip-warn' : 'chip-good'}">${breachedPct}% breached</span>
          </header>
          <div class="dash-two-col">${Dash.donut(d.slaMix)}<div class="dash-sla-summary">
            ${mixTot ? `<div class="sla-row"><span class="sla-label">On time</span><span class="sla-val good">${mix.ON_TIME || 0}</span><span class="sla-pct good">${ontimePct}%</span></div>
            <div class="sla-row"><span class="sla-label">Warning</span><span class="sla-val warn">${mix.WARNING || 0}</span><span class="sla-pct warn">${mixTot ? Math.round(((mix.WARNING || 0) / mixTot) * 100) : 0}%</span></div>
            <div class="sla-row"><span class="sla-label">Breached</span><span class="sla-val bad">${mix.BREACHED || 0}</span><span class="sla-pct bad">${breachedPct}%</span></div>` : '<div class="dash-empty">No open orders</div>'}
          </div></div>
        </section>
        <section class="dash-card dash-card-wide">
          <header class="dash-card-head">
            <div class="dash-card-title"><h3>Team effort</h3><p class="dash-card-sub">Clocked time · last ${d.days} days</p></div>
          </header>
          ${Dash.team(d)}
        </section>
        <section class="dash-card">
          <header class="dash-card-head">
            <div class="dash-card-title"><h3>Clock time</h3><p class="dash-card-sub">Hours per day · last ${d.days} days</p></div>
            <span class="dash-chip chip-info">${Dash.dur(clockPeak)} peak</span>
          </header>
          ${clockedHtml}
        </section>
      </div>`;
    hydrateCounts(el);
  },
};

const Dash = {
  kpi(label, value, tone, trend, delta) {
    const trendHtml = trend ? `<span class="kpi-trend ${delta > 0 ? 'up' : delta < 0 ? 'down' : 'flat'}">${delta > 0 ? '▲' : delta < 0 ? '▼' : '–'}${Math.abs(delta || 0)}</span>` : '';
    return `<div class="kpi${tone ? ' ' + tone : ''}"><div class="kpi-inner"><span class="kpi-v">${esc(value)}</span>${trendHtml}</div><span class="kpi-l">${esc(label)}</span></div>`;
  },

  dur(ms) {
    if (!ms || ms < 0 || !isFinite(ms)) return '—';
    const m = Math.round(ms / 60000);
    const h = Math.floor(m / 60);
    return h ? `${h}h ${m % 60}m` : `${m}m`;
  },

  days(d) {
    const out = [];
    for (let i = d.days - 1; i >= 0; i--) out.push(new Date(d.now - i * 86400000).toISOString().slice(0, 10));
    return out;
  },

  bars(d) {
    const days = Dash.days(d);
    const comp = Object.fromEntries((d.throughput || []).map((x) => [x.day, x.count]));
    const cre = Object.fromEntries((d.created || []).map((x) => [x.day, x.count]));
    const max = Math.max(1, ...days.map((k) => Math.max(comp[k] || 0, cre[k] || 0)));
    const W = 620, H = 190, P = 18, ih = H - 46;
    const bw = (W - P * 2) / days.length;
    const cols = days.map((k, i) => {
      const c = comp[k] || 0, r = cre[k] || 0;
      const hc = (c / max) * ih, hr = (r / max) * ih;
      const x = P + i * bw;
      return `<g><title>${k} — ${c} completed · ${r} received</title>
        <rect x="${(x + bw * 0.16).toFixed(1)}" y="${(H - 26 - hr).toFixed(1)}" width="${(bw * 0.3).toFixed(1)}" height="${hr.toFixed(1)}" rx="2" class="bar-b"></rect>
        <rect x="${(x + bw * 0.52).toFixed(1)}" y="${(H - 26 - hc).toFixed(1)}" width="${(bw * 0.3).toFixed(1)}" height="${hc.toFixed(1)}" rx="2" class="bar-a"></rect></g>
        ${i % 2 === 0 ? `<text x="${(x + bw / 2).toFixed(1)}" y="${H - 10}" class="dash-lbl" text-anchor="middle">${k.slice(5)}</text>` : ''}`;
    }).join('');
    const grid = [0.25, 0.5, 0.75, 1].map((f) => `<line x1="${P}" x2="${W - P}" y1="${(H - 26 - f * ih).toFixed(1)}" y2="${(H - 26 - f * ih).toFixed(1)}" class="dash-grid"></line>`).join('');
    return `<svg viewBox="0 0 ${W} ${H}" class="dash-svg" role="img" aria-label="Completed and received orders per day">${grid}${cols}</svg>`;
  },

  donut(mix) {
    const tot = (mix.ON_TIME || 0) + (mix.WARNING || 0) + (mix.BREACHED || 0);
    if (!tot) return '<p class="muted">Nothing open — the floor is clear.</p>';
    const r = 52, C = 2 * Math.PI * r;
    let off = 0;
    const parts = [['ON_TIME', 'var(--good)', 'On time'], ['WARNING', 'var(--warn)', 'Warning'], ['BREACHED', 'var(--bad)', 'Breached']]
      .map(([k, col, lbl]) => {
        const v = mix[k] || 0;
        const seg = v ? `<circle r="${r}" cx="70" cy="70" fill="none" stroke="${col}" stroke-width="17"
          stroke-dasharray="${((v / tot) * C - 2).toFixed(1)} ${C}" stroke-dashoffset="${(-off).toFixed(1)}
          transform="rotate(-90 70 70)"><title>${lbl}: ${v}</title></circle>` : '';
        off += (v / tot) * C;
        return seg;
      }).join('');
    const legend = [['On time', mix.ON_TIME, 'good'], ['Warning', mix.WARNING, 'warn'], ['Breached', mix.BREACHED, 'bad']]
      .map(([lbl, v, tone]) => `<li><i class="lg ${tone}"></i><span>${lbl}</span><strong>${v || 0}</strong></li>`).join('');
    return `<div class="donut-wrap">
      <svg viewBox="0 0 140 140" class="donut" role="img" aria-label="Open orders by SLA status">
        <circle r="${r}" cx="70" cy="70" fill="none" stroke="var(--neutral-line)" stroke-width="17"></circle>
        ${parts}
        <text x="70" y="68" class="donut-n" text-anchor="middle">${tot}</text>
        <text x="70" y="86" class="donut-t" text-anchor="middle">open</text>
      </svg>
      <ul class="donut-legend">${legend}</ul>
    </div>`;
  },

  clocked(d) {
    const days = Dash.days(d);
    const per = Object.fromEntries((d.clockedPerDay || []).map((x) => [x.day, x.ms]));
    const vals = days.map((k) => per[k] || 0);
    const max = Math.max(3600000, ...vals);
    const W = 300, H = 150, P = 12, ih = H - 40;
    const bw = (W - P * 2) / days.length;
    const cols = days.map((k, i) => {
      const h = (vals[i] / max) * ih;
      const x = P + i * bw;
      return `<g><title>${k} — ${Dash.dur(vals[i])} clocked</title>
        <rect x="${(x + bw * 0.18).toFixed(1)}" y="${(H - 22 - h).toFixed(1)}" width="${(bw * 0.64).toFixed(1)}" height="${h.toFixed(1)}" rx="3" class="bar-c"></rect></g>
        ${i % 3 === 0 ? `<text x="${(x + bw / 2).toFixed(1)}" y="${H - 8}" class="dash-lbl" text-anchor="middle">${k.slice(8)}</text>` : ''}`;
    }).join('');
    Dash._peak = `<span class="muted">peak ${Dash.dur(max === 3600000 && Math.max(...vals) === 0 ? 0 : max)}</span>`;
    return `<svg viewBox="0 0 ${W} ${H}" class="dash-svg" role="img" aria-label="Clocked hours per day">${cols}</svg>`;
  },

  team(d) {
    const rows = [...(d.leaderboard || [])].sort((a, b) => b.clockedMs - a.clockedMs);
    if (!rows.length) return '<p class="muted dash-empty">No work sessions in this window yet.</p>';
    const name = (u) => (d.userNames || {})[u] || u;
    return `<table class="dash-table">
      <thead><tr><th>Tech</th><th class="num">Clocked</th><th class="num">Sessions</th><th class="num">Done</th><th class="num">Now</th></tr></thead>
      <tbody>${rows.map((u) => `
        <tr>
          <td>${esc(name(u.username))}</td>
          <td class="num strong">${Dash.dur(u.clockedMs)}</td>
          <td class="num">${u.sessions}</td>
          <td class="num">${u.completed}</td>
          <td class="num">${u.active ? `<span class="live-dot" title="${u.active} active session(s)">●</span>` : '<span class="muted">—</span>'}</td>
        </tr>`).join('')}
      </tbody>
    </table>`;
  },
};

window.Dashboard = Dashboard;
