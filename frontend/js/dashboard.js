'use strict';

/* Ops dashboard — server-aggregated KPIs and charts on the home pane. */

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
    el.innerHTML = `
      <div class="dash-kpis">
        ${Dash.kpi('Open orders', d.open, '')}
        ${Dash.kpi('Breached now', d.slaMix.BREACHED, d.slaMix.BREACHED ? 'bad' : '')}
        ${Dash.kpi('Clocked in', actives, actives ? 'good' : '')}
        ${Dash.kpi('Completed today', d.completedToday, '')}
        ${Dash.kpi('Avg hands-on', Dash.dur(d.handleAvgMs), '')}
      </div>
      <div class="dash-grid">
        <section class="card dash-card">
          <header class="dash-card-head"><h3>Completed vs received</h3><span class="muted">last ${d.days} days</span></header>
          ${Dash.bars(d)}
        </section>
        <section class="card dash-card">
          <header class="dash-card-head"><h3>Open orders by SLA</h3><span class="muted">live</span></header>
          ${Dash.donut(d.slaMix)}
        </section>
        <section class="card dash-card">
          <header class="dash-card-head"><h3>Team effort</h3><span class="muted">clocked time, last ${d.days} days</span></header>
          ${Dash.team(d)}
        </section>
        <section class="card dash-card">
          <header class="dash-card-head"><h3>Clock time per day</h3><span class="muted">hours on the floor</span></header>
          ${Dash.clocked(d)}
        </section>
      </div>`;
    hydrateCounts(el);
  },
};

const Dash = {
  kpi(label, value, tone) {
    return `<div class="kpi${tone ? ' ' + tone : ''}"><span class="kpi-v">${esc(value)}</span><span class="kpi-l">${esc(label)}</span></div>`;
  },

  dur(ms) {
    if (!ms) return '—';
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
    return `<svg viewBox="0 0 ${W} ${H}" class="dash-svg" role="img" aria-label="Completed and received orders per day">${grid}${cols}</svg>
      <div class="dash-legend"><span><i class="sw a"></i>completed</span><span><i class="sw b"></i>received</span></div>`;
  },

  donut(mix) {
    const tot = (mix.ON_TIME || 0) + (mix.WARNING || 0) + (mix.BREACHED || 0);
    if (!tot) return '<p class="muted dash-empty">Nothing open — the floor is clear.</p>';
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
    return `<svg viewBox="0 0 ${W} ${H}" class="dash-svg" role="img" aria-label="Clocked hours per day">${cols}</svg>
      <div class="dash-legend"><span><i class="sw c"></i>hours clocked</span><span class="muted">peak ${Dash.dur(max === 3600000 && Math.max(...vals) === 0 ? 0 : max)}</span></div>`;
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
