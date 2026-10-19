'use strict';

/* Ran devices — admin-only dashboard for refurb prep logs.
   Ingestion is public (POST /api/v1/prep-logs); viewing is users:manage. */
const Devices = (() => {
  const S = { list: [], selectedId: null, search: '', filter: 'all', busy: false };

  const root = () => $('#view-devices');
  const $rd = (sel) => $(sel, root());

  const relTime = (iso) => {
    const t = new Date(iso).getTime();
    if (!t) return '—';
    const s = Math.max(0, (Date.now() - t) / 1000);
    if (s < 60) return 'just now';
    if (s < 3600) return `${Math.floor(s / 60)}m ago`;
    if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
    return `${Math.floor(s / 86400)}d ago`;
  };
  const fmtDate = (ts) => {
    if (!ts) return '—';
    const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})/.exec(ts);
    if (m) return `${m[3]}-${m[2]}-${m[1]} ${m[4]}:${m[5]}`;
    const d = new Date(ts);
    return isNaN(d) ? ts : d.toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' });
  };

  // ── data ─────────────────────────────────────────────────────────────
  async function load(keepSelection) {
    if (S.busy) return;
    S.busy = true;
    try {
      const d = await api('GET', '/api/v1/admin/prep-devices');
      S.list = d.devices || [];
      renderKpis();
      const visible = filtered();
      if (!keepSelection || !visible.some((x) => x.id === S.selectedId)) {
        S.selectedId = visible.length ? visible[0].id : null;
      }
      renderList();
      if (S.selectedId) await select(S.selectedId, true);
      else renderDetailEmpty();
    } catch (e) {
      toast('Ran devices failed to load: ' + e.message, 'error');
      renderListError(e.message);
    } finally {
      S.busy = false;
    }
  }

  async function select(id, silent) {
    S.selectedId = id;
    $$('.rd-row', $rd('#rd-list')).forEach((el) => el.classList.toggle('active', Number(el.dataset.id) === id));
    $$('.rd-row', $rd('#rd-list')).forEach((el) => el.setAttribute('aria-pressed', String(Number(el.dataset.id) === id)));
    const det = $rd('#rd-detail');
    det.innerHTML = '<div class="rd-empty"><div class="sk sk-line" style="width:60%"></div><div class="sk sk-line" style="width:80%"></div><div class="sk sk-block"></div><div class="sk sk-block"></div></div>';
    try {
      const d = await api('GET', `/api/v1/admin/prep-devices/${id}`);
      if (S.selectedId !== id) return; // user moved on already
      renderDetail(d);
    } catch (e) {
      if (S.selectedId !== id) return;
      det.innerHTML = `<div class="rd-empty"><p class="muted">Could not load device: ${esc(e.message)}</p></div>`;
    }
  }

  // ── list + kpis ──────────────────────────────────────────────────────
  // Each machine contributes exactly once to the fleet chart. A flagged
  // check takes priority over incomplete prep; no checks is not a pass.
  const statuses = [
    { key: 'ready', label: 'Prep complete', color: 'var(--good)' },
    { key: 'issues', label: 'Needs attention', color: 'var(--bad)' },
    { key: 'incomplete', label: 'Incomplete', color: 'var(--warn)' },
    { key: 'unknown', label: 'No check data', color: 'var(--muted)' },
  ];
  function deviceStatus(x) {
    if (x.checksFlag > 0) return 'issues';
    if (!(x.checksOK > 0)) return 'unknown';
    return x.complete ? 'ready' : 'incomplete';
  }
  const counts = () => Object.fromEntries(statuses.map((s) => [s.key, S.list.filter((x) => deviceStatus(x) === s.key).length]));

  function renderKpis() {
    const total = S.list.length, c = counts();
    const runs = S.list.reduce((n, x) => n + (x.runs || 1), 0);
    const ok = S.list.reduce((n, x) => n + (x.checksOK || 0), 0);
    const flag = S.list.reduce((n, x) => n + (x.checksFlag || 0), 0);
    const latest = S.list.reduce((v, x) => x.lastSeenAt > v ? x.lastSeenAt : v, '');
    let offset = 0;
    const segments = statuses.map((s) => {
      const share = total ? c[s.key] / total * 100 : 0;
      const arc = share ? `<circle class="rd-arc" cx="90" cy="90" r="68" pathLength="100" fill="none" stroke="${s.color}" stroke-width="17" stroke-dasharray="${share} ${100 - share}" stroke-dashoffset="${-offset}" transform="rotate(-90 90 90)"><title>${s.label}: ${c[s.key]}</title></circle>` : '';
      offset += share;
      return arc;
    }).join('');
    $rd('#rd-overview').innerHTML = `
      <section class="card rd-fleet-card">
        <div class="rd-card-heading"><div><span class="rd-eyebrow">FLEET OVERVIEW</span><h2>Every device. One view.</h2></div><span class="rd-scope">Latest report per device</span></div>
        <div class="rd-fleet-body">
          <div class="rd-donut" role="img" aria-label="${total} devices: ${statuses.map((s) => `${c[s.key]} ${s.label}`).join(', ')}">
            <svg viewBox="0 0 180 180" aria-hidden="true"><circle cx="90" cy="90" r="68" fill="none" stroke="var(--border)" stroke-width="17"/>${segments}</svg>
            <div class="rd-donut-center"><strong>${total}</strong><span>Total devices</span></div>
          </div>
          <div class="rd-legend">${statuses.map((s) => `<button type="button" data-status="${s.key}" aria-pressed="${S.filter === s.key}" class="rd-legend-row"><span class="rd-dot" style="background:${s.color}"></span><span>${s.label}</span><strong>${c[s.key]}</strong><small>${total ? Math.round(c[s.key] / total * 100) : 0}%</small></button>`).join('')}</div>
        </div>
        <p class="rd-chart-note">${total ? 'Devices with flagged checks appear under Needs attention, even if prep is incomplete.' : 'Upload a prep log to start building your fleet overview.'}</p>
      </section>
      <section class="card rd-health-card">
        <span class="rd-eyebrow">PREP READINESS</span><div class="rd-readiness">${total ? Math.round(c.ready / total * 100) : '—'}<span>${total ? '%' : ''}</span></div>
        <h2>Completed without flags</h2><p class="muted">${c.ready} of ${total} devices have a completed prep report with checks and no flagged issues.</p>
        <div class="rd-health-track"><span style="width:${total ? c.ready / total * 100 : 0}%"></span></div>
        <div class="rd-health-foot"><span>Last log received</span><strong>${esc(latest ? relTime(latest) : 'No logs yet')}</strong></div>
      </section>`;
    $rd('#rd-kpis').innerHTML = `
      <div class="rd-kpi"><span class="rd-kpi-l">Prep runs recorded</span><span class="rd-kpi-n">${runs}</span><span class="muted">Including repeat runs</span></div>
      <div class="rd-kpi good"><span class="rd-kpi-l">Checks passed</span><span class="rd-kpi-n">${ok}</span><span class="muted">Across latest reports</span></div>
      <div class="rd-kpi ${flag ? 'bad' : ''}"><span class="rd-kpi-l">Checks flagged</span><span class="rd-kpi-n">${flag}</span><span class="muted">On ${c.issues} devices</span></div>`;
    $rd('#rd-filters').innerHTML = [{key: 'all', label: 'All devices'}, ...statuses].map((s) => `<button type="button" class="rd-filter" data-status="${s.key}" aria-pressed="${S.filter === s.key}">${s.label}<span>${s.key === 'all' ? total : c[s.key]}</span></button>`).join('');
    $$('[data-status]', root()).forEach((b) => b.addEventListener('click', () => {
      S.filter = S.filter === b.dataset.status ? 'all' : b.dataset.status;
      renderKpis();
      syncSelection();
    }));
  }

  function filtered() {
    const q = S.search.trim().toLowerCase();
    return S.list.filter((x) => (S.filter === 'all' || deviceStatus(x) === S.filter) &&
      (!q || [x.computerName, x.manufacturer, x.model, x.cpuName, x.os, x.fileName]
      .filter(Boolean).some((v) => String(v).toLowerCase().includes(q))));
  }

  function syncSelection() {
    const rows = filtered();
    const keep = rows.some((x) => x.id === S.selectedId);
    if (!keep) S.selectedId = rows.length ? rows[0].id : null;
    renderList();
    if (!keep) {
      if (S.selectedId) select(S.selectedId);
      else renderDetailEmpty();
    }
  }

  function renderList() {
    const el = $rd('#rd-list');
    const rows = filtered();
    $rd('#rd-result-count').textContent = `${rows.length} / ${S.list.length}`;
    if (!rows.length) {
      el.innerHTML = S.list.length
        ? '<div class="rd-empty small"><p class="muted">No devices match these filters. Try another status or search.</p></div>'
        : `<div class="rd-empty">
             <div class="rd-empty-ico">🖥️</div>
             <h3>No devices yet</h3>
             <p class="muted">Devices appear here automatically when the prep script posts its log:<br>
             <code>POST /api/v1/prep-logs</code> (public, no login)</p>
           </div>`;
      return;
    }
    el.innerHTML = rows.map((x) => `
      <button type="button" class="rd-row ${x.id === S.selectedId ? 'active' : ''}" data-id="${x.id}" aria-pressed="${x.id === S.selectedId}">
        <span class="rd-row-top">
          <strong>${esc(x.computerName)}</strong>
          <span class="rd-flag rd-status-${deviceStatus(x)}">${statuses.find((s) => s.key === deviceStatus(x)).label}</span>
        </span>
        <span class="rd-row-sub">${esc([x.manufacturer, x.model].filter(Boolean).join(' · ') || 'Unknown machine')}</span>
        <span class="rd-row-sub">${esc(x.cpuName || '—')}${x.ramText ? ' · ' + esc(x.ramText) : ''}</span>
        <span class="rd-row-foot">
          <span>${relTime(x.lastSeenAt)}</span>
          ${(x.runs || 1) > 1 ? `<span class="rd-runs">ran ${x.runs}×</span>` : ''}
          ${x.complete ? '' : '<span class="rd-runs warn">incomplete</span>'}
        </span>
      </button>`).join('');
    $$('.rd-row', el).forEach((b) => b.addEventListener('click', () => select(Number(b.dataset.id))));
  }

  function renderListError(msg) {
    $rd('#rd-list').innerHTML = `<div class="rd-empty"><p class="muted">Load failed: ${esc(msg)}</p></div>`;
  }

  function renderDetailEmpty() {
    $rd('#rd-detail').innerHTML = `
      <div class="rd-empty tall">
        <div class="rd-empty-ico">🛠️</div>
        <h3>Nothing selected</h3>
        <p class="muted">Pick a device on the left to see its full prep report.</p>
      </div>`;
  }

  // ── detail ───────────────────────────────────────────────────────────
  const PIPE = [
    ['specs', 'System specs', /collecting system specs/i],
    ['ident', 'Identity', /collecting device identification|Device CSV written/i],
    ['board', 'Motherboard', /collecting motherboard details/i],
    ['hw', 'HW checks', /collecting extended hardware checks/i],
    ['batt', 'Battery report', /battery report written|generating battery report/i],
    ['wu', 'Windows Update', /windows update trigger phase done|triggering update scan/i],
    ['done', 'Sign-off', /prep phase complete/i],
  ];

  function pipelineHtml(doc) {
    const evs = doc.events || [];
    let lastDone = -1;
    const steps = PIPE.map(([k, label, re], i) => {
      const ev = evs.find((e) => re.test(e.m));
      const done = !!ev;
      if (done) lastDone = i;
      return { k, label, done, ts: ev ? ev.t : null };
    });
    const activeIdx = doc.complete ? -1 : lastDone + 1;
    return `<div class="rd-pipe">${steps.map((s, i) => `
      <div class="rd-step ${s.done ? 'done' : ''} ${i === activeIdx ? 'live' : ''}">
        <span class="rd-step-dot">${s.done ? '✓' : i === activeIdx ? '…' : ''}</span>
        <span class="rd-step-txt"><b>${s.label}</b><i>${s.ts ? s.ts.slice(11) : ''}</i></span>
      </div>`).join('')}</div>`;
  }

  function updatesHtml(doc) {
    const ups = doc.updates || [];
    if (!ups.length) return '';
    return `<div class="rd-updates">${ups.map((u) => `
      <span class="rd-upd ${u.code === '0' ? 'ok' : 'fail'}" title="Exit code">
        <b>${u.code === '0' ? '✓' : '✗'}</b> ${esc(u.cmd)} <code>${esc(u.code)}</code>
      </span>`).join('')}</div>`;
  }

  function tile(icon, label, main, sub, tone) {
    return `<div class="rd-tile ${tone || ''}">
      <span class="rd-tile-ico">${icon}</span>
      <span class="rd-tile-txt"><span class="rd-tile-l">${label}</span>
        <span class="rd-tile-m">${main}</span>${sub ? `<span class="rd-tile-s">${sub}</span>` : ''}
      </span></div>`;
  }

  const statTone = (st) => (st ? (String(st).toUpperCase() === 'OK' ? 'good' : 'bad') : '');

  function heroHtml(doc) {
    const t = [];
    const cpu = doc.cpu || {};
    t.push(tile('🧠', 'Processor', esc(cpu.name || '—'),
      [cpu.cores && `${cpu.cores}C/${cpu.threads}T`, cpu.maxMHz && `${cpu.maxMHz} MHz`, cpu.socket && `Socket ${cpu.socket}`].filter(Boolean).map(esc).join(' · '),
      statTone(cpu.status)));
    const ram = doc.ram || {};
    const mods = (ram.modules || []);
    t.push(tile('🧩', 'Memory', esc(ram.text || '—'),
      mods.length ? esc(`${mods.length}× ${mods[0].mhz || ''} MT/s — ${mods[0].part || ''}`) : '',
      ''));
    const disks = doc.disks || [];
    const tot = disks.reduce((a, d) => a + (d.sizeGB || 0), 0);
    t.push(tile('💽', 'Storage', tot ? `${Math.round(tot)} GB total` : '—',
      esc(disks.map((d) => `${d.model || '?'} (${d.sizeGB || '?'} GB, ${d.iface || '?'})`).join('\n')),
      disks.length && disks.every((d) => (d.status || '').toUpperCase() === 'OK') ? 'good' : (disks.length ? 'bad' : '')));
    const gpu = doc.gpu || {};
    t.push(tile('🖼️', 'Graphics', esc(gpu.name || '—'),
      gpu.resX ? esc(`${gpu.resX}×${gpu.resY}`) + (gpu.driver ? ' · driver ' + esc(gpu.driver) : '') : '',
      statTone(gpu.status)));
    t.push(tile('🪟', 'Operating system', esc(doc.os || '—'), '', ''));
    const batt = doc.battery || {};
    t.push(tile('🔋', 'Battery',
      batt.name ? `${esc(batt.name)} — ${esc(batt.chargePct ?? '?')}%` : 'No battery (desktop?)',
      batt.designMWh && batt.designMWh !== 'N/A' ? `Design ${batt.designMWh} mWh · Full ${batt.fullMWh} mWh` : (batt.name ? 'Capacity n/a from WMI' : ''),
      statTone(batt.status)));
    const tpm = (doc.health || {}).tpm || {};
    t.push(tile('🛡️', 'Security',
      tpm.spec ? `TPM ${esc(String(tpm.spec).split(',')[0])}` : 'TPM unknown',
      tpm.spec ? (tpm.enabled && tpm.activated ? 'Enabled &amp; activated ✓' : 'Present but not active') : '',
      tpm.spec ? (tpm.enabled && tpm.activated ? 'good' : 'warn') : ''));
    const nets = (doc.network || []).filter((n) => !/virtual|bluetooth device/i.test(n.name || ''));
    t.push(tile('📡', 'Network',
      nets.length ? esc(nets.map((n) => n.name.split(' ').slice(0, 3).join(' ')).join(' · ')) : '—',
      esc((doc.overview?.macs || []).join(', ')), ''));
    return `<div class="rd-hero">${t.join('')}</div>`;
  }

  function sectionsHtml(doc) {
    return (doc.sections || []).map((sec) => `
      <details class="rd-sec">
        <summary>
          <span class="rd-sec-name">${esc(sec.name)}</span>
          <span class="rd-sec-count">${sec.items.length} item${sec.items.length > 1 ? 's' : ''}</span>
        </summary>
        <div class="rd-sec-body">
          ${sec.items.map((item, i) => `
            ${sec.items.length > 1 ? `<div class="rd-item-h">#${i + 1}</div>` : ''}
            <table class="rd-kv">${item.map((e) => `
              <tr><th>${esc(e.k)}</th><td>${valueHtml(e)}</td></tr>`).join('')}
            </table>`).join('')}
        </div>
      </details>`).join('');
  }

  function valueHtml(e) {
    const v = esc(e.v ?? '');
    if (e.k === 'Status') {
      const ok = e.v.toUpperCase() === 'OK';
      return `<span class="rd-status ${ok ? 'ok' : 'bad'}"><i></i>${v}</span>`;
    }
    if (/^(SizeBytes|Bytes|VRAM|FreeBytes)$/.test(e.k)) {
      const n = Number(e.v);
      if (n > 1e6) return `${v} <span class="muted">(${Math.round(n / (1 << 30) * 10) / 10} GB)</span>`;
    }
    if (e.k === 'Speed') {
      const n = Number(e.v);
      if (n >= 9e18) return '<span class="muted">n/a (unset)</span>';
      if (n >= 1e6) return `${Math.round(n / 1e6)} Mb/s`;
    }
    if (e.k === 'Serial' || e.k === 'MAC' || e.k === 'System UUID') return `<code class="rd-mono">${v}</code>`;
    return v;
  }

  function renderDetail(d) {
    const doc = d.doc || {};
    const ov = doc.overview || {};
    const issues = doc.issues || [];
    const flag = (doc.stats || {}).checksFlag || 0;

    $rd('#rd-detail').innerHTML = `
      <header class="rd-head">
        <div class="rd-dev-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="11" rx="1.6"/><path d="M8 19h8M12 15v4"/></svg></div>
        <div class="rd-head-main">
          <h2>${esc(doc.host || ov.computerName || d.computerName || 'Device')}</h2>
          <p class="muted">${esc([ov.manufacturer, ov.model].filter(Boolean).join(' · ') || 'Unknown machine')}
            ${ov.biosSerial ? ` · BIOS <code class="rd-mono">${esc(ov.biosSerial)}</code>` : ''}</p>
          <div class="rd-chips">
            <span class="pill ${doc.complete ? 'goodp' : 'warnp'}">${doc.complete ? 'Prep complete' : 'Prep incomplete'}</span>
            <span class="pill ghostp">started ${fmtDate(doc.startedAt)}</span>
            <span class="pill ghostp">${d.runs} run${d.runs > 1 ? 's' : ''} · last ${relTime(d.lastSeen)}</span>
            ${ov.systemUUID ? `<button type="button" class="pill ghostp rd-copy" data-copy="${esc(ov.systemUUID)}" title="Copy UUID">UUID ${esc(String(ov.systemUUID).slice(0, 8))}… ⧉</button>` : ''}
          </div>
        </div>
        <div class="rd-score ${flag ? 'bad' : 'good'}">
          <b>${(doc.stats || {}).checksOK || 0}</b><i>✓</i>
          ${flag ? `<b class="b2">${flag}</b><i>⚠</i>` : ''}
        </div>
      </header>

      ${issues.length ? `
        <div class="rd-issues">
          <b>${issues.length} issue${issues.length > 1 ? 's' : ''} flagged</b>
          ${issues.map((i) => `<div class="rd-issue"><span class="rd-issue-src">${esc(i.src)}</span>${esc(i.text)}</div>`).join('')}
        </div>` : `
        <div class="rd-allok"><span>✓</span> Every check the script probed came back OK.</div>`}

      ${pipelineHtml(doc)}
      ${updatesHtml(doc)}
      ${heroHtml(doc)}

      <h3 class="rd-sub">Full inventory</h3>
      ${sectionsHtml(doc)}

      <div class="rd-actions">
        <button type="button" class="btn btn-ghost" id="rd-dl-raw">⬇ Raw log</button>
        <button type="button" class="btn btn-ghost" id="rd-copy-json">⧉ Copy JSON</button>
        <button type="button" class="btn btn-ghost btn-danger" id="rd-del">Delete device</button>
      </div>

      <details class="rd-sec rd-raw">
        <summary><span class="rd-sec-name">Raw log (${esc(d.fileName || 'unknown')})</span></summary>
        <pre class="rd-raw-pre">${esc(d.raw || '')}</pre>
      </details>`;

    $('#rd-dl-raw', $rd('#rd-detail')).addEventListener('click', () => {
      const blob = new Blob([d.raw || ''], { type: 'text/plain' });
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = d.fileName || `${doc.host || 'device'}_prep.log`;
      a.click();
      URL.revokeObjectURL(a.href);
    });
    $('#rd-copy-json', $rd('#rd-detail')).addEventListener('click', async () => {
      try { await navigator.clipboard.writeText(JSON.stringify(doc, null, 2)); toast('Device JSON copied'); }
      catch { toast('Clipboard blocked', 'error'); }
    });
    $('#rd-del', $rd('#rd-detail')).addEventListener('click', async () => {
      const okDel = await confirmDialog(`Delete ${doc.host || 'this device'}?`,
        'Removes the stored prep report. The device re-appears if the script posts again.',
        'Delete');
      if (!okDel) return;
      try {
        await api('DELETE', `/api/v1/admin/prep-devices/${d.id}`);
        toast('Device deleted');
        S.selectedId = null;
        await load(false);
      } catch (e) { toast('Delete failed: ' + e.message, 'error'); }
    });
    $$('.rd-copy', $rd('#rd-detail')).forEach((b) => b.addEventListener('click', async () => {
      try { await navigator.clipboard.writeText(b.dataset.copy); toast('UUID copied'); } catch {}
    }));
  }

  // ── wiring ───────────────────────────────────────────────────────────
  function wire() {
    const srch = $rd('#rd-search');
    if (srch && !srch.dataset.wired) {
      srch.dataset.wired = '1';
      srch.addEventListener('input', () => { S.search = srch.value; syncSelection(); });
    }
    const ref = $rd('#rd-refresh');
    if (ref && !ref.dataset.wired) {
      ref.dataset.wired = '1';
      ref.addEventListener('click', () => load(true).then(() => {}));
    }
    const up = $rd('#rd-upload');
    const inp = $rd('#rd-file');
    if (up && !up.dataset.wired) {
      up.dataset.wired = '1';
      up.addEventListener('click', () => inp.click());
      inp.addEventListener('change', async () => {
        const f = inp.files[0];
        inp.value = '';
        if (!f) return;
        try {
          const res = await fetch('/api/v1/prep-logs', {
            method: 'POST',
            headers: { 'Content-Type': 'text/plain', 'X-File-Name': f.name },
            body: f,
          });
          const d = await res.json();
          if (!res.ok) throw new Error((d.error && d.error.message) || 'Rejected');
          toast(`${d.computerName}: log accepted — ${d.checksOK} checks OK${d.checksFlag ? `, ${d.checksFlag} flagged` : ''}`);
          await load(false);
        } catch (e) { toast(e.message, 'error'); }
      });
    }
  }

  async function open() { wire(); await load(false); }
  return { open, load };
})();

window.Devices = Devices;
