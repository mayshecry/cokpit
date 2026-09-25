'use strict';

  

  function initSettingsPop() {
    const pop = $('#settings-pop');
    $('#settings-btn').addEventListener('click', (e) => {
      e.stopPropagation();
      pop.classList.toggle('hidden');
      if (pop.classList.contains('hidden')) return;
      $('#pref-theme').value = localStorage.getItem('cockpit_theme') || 'system';
      $('#pref-density').value = localStorage.getItem('cockpit_density') || 'cozy';
      $('#pref-lang').value = lang();
      $('#pref-alerts').checked = localStorage.getItem('cockpit_alerts') === 'on';
    });
    document.addEventListener('click', (e) => {
      if (!pop.classList.contains('hidden') && !e.target.closest('#settings-pop') && !e.target.closest('#settings-btn')) {
        pop.classList.add('hidden');
      }
    });
    $('#pref-theme').addEventListener('change', (e) => { localStorage.setItem('cockpit_theme', e.target.value); applyPrefs(); });
    $('#pref-density').addEventListener('change', (e) => { localStorage.setItem('cockpit_density', e.target.value); applyPrefs(); });
    $('#pref-lang').addEventListener('change', (e) => { localStorage.setItem('cockpit_lang', e.target.value); location.reload(); });
    $('#pref-alerts').addEventListener('change', async (e) => {
      if (e.target.checked) {
        if (typeof Notification !== 'undefined' && Notification.permission === 'default') {
          try { await Notification.requestPermission(); } catch {  }
        }
        localStorage.setItem('cockpit_alerts', 'on');
      } else {
        localStorage.setItem('cockpit_alerts', 'off');
      }
    });
  }

  

  async function downloadBackup() {
    try {
      const res = await fetch('/api/v1/admin/backup', { headers: { Authorization: 'Bearer ' + state.token } });
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const cd = res.headers.get('Content-Disposition') || '';
      const m = cd.match(/filename="?([^";]+)"?/);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = m ? m[1] : 'cockpit-backup.db';
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      toast(t('users.backupOk', { name: a.download }));
    } catch (err) {
      toast('Backup failed: ' + err.message, true);
    }
  }

  

  async function loadUserBriefs() {
    try {
      const d = await api('GET', '/api/v1/users/brief');
      state.userBriefs = d.users || [];
    } catch {  }
  }

  

  function openShortcuts() {
    const rows = [
      [t('shortcut.palette'), ['Ctrl', 'K']],
      [t('shortcut.myWork'), ['h']],
      [t('shortcut.search'), ['/']],
      [t('shortcut.rowNav'), ['j', 'k']],
      [t('shortcut.rowOpen'), ['Enter']],
      [t('shortcut.rowSelect'), ['x']],
      [t('shortcut.new'), ['n']],
      [t('shortcut.export'), ['e']],
      [t('shortcut.goto'), ['g', 'h']],
      [t('shortcut.goWork'), ['g', 'w']],
      [t('shortcut.goOrders'), ['g', 'o']],
      [t('shortcut.goUsers'), ['g', 'u']],
      [t('shortcut.goConfig'), ['g', 'c']],
      [t('shortcut.refresh'), ['r']],
      [t('shortcut.close'), ['Esc']],
      [t('shortcut.cheats'), ['?']],
    ];
    modal({
      title: 'Keyboard shortcuts',
      hideConfirm: true,
      cancelLabel: 'Close',
      bodyHtml: `<div class="shortcut-table">${rows.map(([label, keys]) =>
        `<div class="shortcut-row"><span>${esc(label)}</span><span class="keys">${keys.map((k) => `<kbd>${esc(k)}</kbd>`).join('')}</span></div>`
      ).join('')}</div>`,
    });
  }

  
  
  
  
  
  
  
  const CONFIG_PERSIST_KEY = 'cockpit_config';

  function hasConfigMeta() {
    const m = state.config.meta || {};
    return ['artikelweergave', 'klantenlijst', 'pqRegels', 'excelRegels'].some((k) => !!m[k]);
  }

  function loadConfig() {
    try {
      const raw = localStorage.getItem(CONFIG_PERSIST_KEY);
      if (raw) {
        state.config = JSON.parse(raw);
        if (!state.configBron && hasConfigMeta()) state.configBron = 'deze browser (opgeslagen)';
      }
    } catch (e) {  }
    try {
      const pm = localStorage.getItem('cockpit_picklist_meta');
      if (pm) state.picklistMeta = JSON.parse(pm);
    } catch (e) {  }
  }

  async function saveConfig() {
    try {
      localStorage.setItem(CONFIG_PERSIST_KEY, JSON.stringify(state.config));
    } catch (e) {
      console.error(e);
    }
  }

  function isXlsxBeschikbaar() { return typeof window.XLSX !== 'undefined'; }

  function readWorkbook(file) {
    if (!isXlsxBeschikbaar()) {
      return Promise.reject(new Error('Excel-library kon niet worden geladen. Open het portaal in een browser met internetverbinding.'));
    }
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        try { resolve(XLSX.read(new Uint8Array(e.target.result), { type: 'array', cellDates: true })); }
        catch (err) { reject(err); }
      };
      reader.onerror = reject;
      reader.readAsArrayBuffer(file);
    });
  }

  function fileInput(accept, onFile) {
    const inp = document.createElement('input');
    inp.type = 'file';
    inp.accept = accept;
    inp.onchange = async (e) => {
      if (!e.target.files[0]) return;
      try {
        await onFile(e.target.files[0]);
      } catch (err) {
        console.error(err);
        toast(err && err.message ? err.message : 'Bestand kon niet worden verwerkt.', true);
      }
    };
    return inp;
  }

  async function handleArtikelweergave(file) {
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rows = XLSX.utils.sheet_to_json(ws, { defval: null });
    const map = {};
    let metLocatie = 0;
    for (const r of rows) {
      const code = String(r['Artikelcode'] ?? '').trim();
      const loc = r['Locatie DBC'];
      if (code && loc) { map[code] = String(loc).trim(); metLocatie++; }
    }
    state.config.artikellocaties = map;
    state.config.meta.artikelweergave = { bestand: file.name, aantal: rows.length, metLocatie, tijd: Date.now() };
    await saveConfig();
    toast(`Artikelweergave verwerkt: ${rows.length} artikelen, ${metLocatie} met locatie`);
    renderConfig();
  }

  async function handleKlantenlijst(file) {
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rows = XLSX.utils.sheet_to_json(ws, { range: 1, defval: null });
    const map = {};
    for (const r of rows) {
      const nr = r['Klantnummer'];
      if (nr == null) continue;
      map[String(nr).trim()] = r['Groep'] || 'Overig';
    }
    state.config.klanten = map;
    state.config.meta.klantenlijst = { bestand: file.name, aantal: Object.keys(map).length, tijd: Date.now() };
    await saveConfig();
    toast(`Klantenlijst verwerkt: ${Object.keys(map).length} klanten`);
    renderConfig();
  }

  async function handlePQRegels(file) {
    const wb = await readWorkbook(file);
    const artikelenSet = new Set();
    if (wb.SheetNames.includes('Filter artikelen')) {
      const ws = wb.Sheets['Filter artikelen'];
      const rows = XLSX.utils.sheet_to_json(ws, { defval: null });
      for (const r of rows) {
        for (const k of Object.keys(r)) {
          if (r[k]) artikelenSet.add(String(r[k]).trim());
        }
      }
    }
    const klantenSet = new Set();
    if (wb.SheetNames.includes('Filter klanten')) {
      const ws = wb.Sheets['Filter klanten'];
      const rows = XLSX.utils.sheet_to_json(ws, { header: 1, defval: null });
      for (const row of rows) {
        for (let i = 1; i < row.length; i++) {
          if (row[i] !== null && row[i] !== '') klantenSet.add(String(row[i]).trim());
        }
      }
    }
    state.config.uitgeslotenArtikelen = [...artikelenSet];
    state.config.uitgeslotenKlanten = [...klantenSet];
    state.config.meta.pqRegels = { bestand: file.name, artikelen: artikelenSet.size, klanten: klantenSet.size, tijd: Date.now() };
    await saveConfig();
    toast(`Uitsluitingsregels verwerkt: ${artikelenSet.size} artikelen, ${klantenSet.size} klanten`);
    renderConfig();
  }

  async function handleExcelRegels(file) {
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rows = XLSX.utils.sheet_to_json(ws, { header: 1, defval: null });
    const regels = [];
    for (const row of rows) {
      const kolomCel = row[1], condCel = row[2], kleurCel = row[3];
      if (typeof kolomCel !== 'string' || !kolomCel.startsWith('Kolom:')) continue; 
      const kolom = kolomCel.replace('Kolom:', '').trim();
      const kleur = String(kleurCel || '').replace('Kleur:', '').trim();
      const condStr = String(condCel || '').replace('Celwaarde', '').trim();
      let operator = null, waarde = null;
      if (condStr.startsWith('bevat')) { operator = 'bevat'; waarde = condStr.replace('bevat', '').trim(); }
      else if (condStr.startsWith('=')) { operator = '='; waarde = condStr.replace('=', '').trim(); }
      if (kolom && operator && waarde) regels.push({ kolom, operator, waarde, kleur });
    }
    state.config.kleurregels = regels;
    state.config.meta.excelRegels = { bestand: file.name, aantal: regels.length, tijd: Date.now() };
    await saveConfig();
    toast(`Kleurregels verwerkt: ${regels.length} regels`);
    renderConfig();
  }

  

  

  function evalRegel(row, regel) {
    const veldwaarde = row[regel.kolom];
    if (regel.operator === 'bevat') {
      return String(veldwaarde ?? '').toLowerCase().includes(regel.waarde.toLowerCase());
    }
    
    const num = Number(regel.waarde);
    if (!Number.isNaN(num) && veldwaarde !== null && veldwaarde !== undefined && veldwaarde !== '') {
      return Number(veldwaarde) === num;
    }
    return String(veldwaarde ?? '' ).trim().toLowerCase() === regel.waarde.trim().toLowerCase();
  }

  function regelStatus(row, kleurregels) {
    const regels = kleurregels || [];
    const aantal = row['Aantal te leveren'] ?? 0;

    
    

    if (aantal < 0) {
      return { ok: true, retour: true, label: `Retour: ${Math.abs(aantal)} stuks terugboeken op voorraad` };
    }

    const groenMatch = regels.find(r => r.kleur.toLowerCase() === 'groen' && evalRegel(row, r));
    if (groenMatch) {
      return { ok: true, retour: false, label: `Uitzondering (${groenMatch.kolom} ${groenMatch.operator === 'bevat' ? 'bevat' : '='} "${groenMatch.waarde}")` };
    }

    const vrdRuw = row['Vrd.'];
    const vrd = (vrdRuw === null || vrdRuw === undefined) ? 0 : vrdRuw;
    return { ok: vrd >= aantal, retour: false, label: '' };
  }

  
  
  
  
  
  async function handleVerkoopregels(file) {
    if (!state.config.meta.artikelweergave || !state.config.meta.klantenlijst) {
      toast('Upload eerst Artikelweergave en Klantenlijst bij Configuratie.', true);
      return;
    }
    const wb = await readWorkbook(file);
    const ws = wb.Sheets[wb.SheetNames[0]];
    const rawRows = XLSX.utils.sheet_to_json(ws, { defval: null });

    const uitArt = new Set(state.config.uitgeslotenArtikelen || []);
    const uitKlant = new Set(state.config.uitgeslotenKlanten || []);
    const voor = rawRows.length;

    const regels = rawRows
      .filter(r => !uitArt.has(String(r['Artikel'] ?? '')))
      .filter(r => !uitKlant.has(String(r['Vrk.rel.'] ?? '')))
      .map(r => {
        const klantnr = String(r['Vrk.rel.'] ?? '');
        const groep = state.config.klanten[klantnr] || 'Overig';
        const locatie = state.config.artikellocaties[String(r['Artikel'] ?? '')] || 'Geen locatie gekoppeld';
        const status = regelStatus(r, state.config.kleurregels);
        return {
          orderNr: r['OrderNr.'], artikel: r['Artikel'], naam: r['Naam'],
          omschrijving: r['Omschrijving'], aantal: r['Aantal te leveren'] ?? 0,
          locatie, ok: status.ok, retour: status.retour,
        };
      });

    
    
    const orderMap = {};
    for (const r of regels) {
      if (!r.ok) continue; 
      if (!orderMap[r.orderNr]) orderMap[r.orderNr] = { orderNr: r.orderNr, regels: [] };
      orderMap[r.orderNr].regels.push(r);
    }
    const orderLines = Object.values(orderMap).map(o => ({
      orderNr: String(o.orderNr).trim(),
      items: o.regels
        .filter(r => (Number(r.aantal) || 0) > 0)
        .sort((a, b) => String(a.locatie).localeCompare(String(b.locatie), 'nl', { numeric: true, sensitivity: 'base' }))
        .map(r => ({
          artikel: String(r.artikel ?? ''), omschrijving: String(r.omschrijving ?? ''),
          locatie: String(r.locatie ?? ''), aantal: Number(r.aantal) || 0,
        })),
    })).filter(o => o.items.length > 0);

    if (!orderLines.length) {
      toast('Geen leverbare pickregels gevonden in het bestand.', true);
      return;
    }

    const byNumber = {};
    for (const ord of state.orders) byNumber[String(ord.orderNumber).trim().toLowerCase()] = ord;

    let matched = 0;
    const missed = [];
    for (const line of orderLines) {
      const ord = byNumber[line.orderNr.toLowerCase()];
      if (!ord) { missed.push(line.orderNr); continue; }
      try {
        await api('POST', `/api/v1/orders/${ord.id}/checklist`, { items: line.items });
        matched++;
      } catch (err) {
        missed.push(line.orderNr + ' ('+ (err && err.message ? err.message : 'fout') + ')');
      }
    }

    state.picklistMeta = { bestand: file.name, orders: matched, totaal: orderLines.length, missed: missed.length, tijd: Date.now() };
    try { localStorage.setItem('cockpit_picklist_meta', JSON.stringify(state.picklistMeta)); } catch (e) {  }

    const missedTxt = missed.length ? `; ${missed.length} niet gevonden (${missed.slice(0, 4).join(', ')}${missed.length > 4 ? ', …' : ''})` : '';
    toast(`Picklijst verwerkt: ${matched}/${orderLines.length} orders bijgewerkt${missedTxt}`);
    renderConfig();
  }

  async function probeerAutomatischeConfigJSON() {
    if (state.configBron) return;
    try {
      const res = await fetch('./orderpick-config.json', { cache: 'no-store' });
      if (!res.ok) return;
      const parsed = JSON.parse(await res.text());
      state.config = parsed;
      state.configBron = 'orderpick-config.json (automatisch geladen)';
      await saveConfig();
    } catch (e) {
      
    }
  }

  function downloadConfigJSON() {
    const blob = new Blob([JSON.stringify(state.config, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'orderpick-config.json';
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    toast('Gedownload als orderpick-config.json - plaats dit bestand in de gedeelde map.');
  }

  async function laadConfigVanuitJSON(file) {
    try {
      const parsed = JSON.parse(await file.text());
      state.config = parsed;
      state.configBron = 'JSON-bestand (handmatig geladen)';
      await saveConfig();
      toast('Configuratie geladen vanuit JSON-bestand');
      renderConfig();
    } catch (e) {
      toast('Kon JSON-bestand niet lezen: ' + e.message, true);
    }
  }

  function configCompatibiliteitsWaarschuwing() {
    if (isXlsxBeschikbaar()) return '';
    return `<div class="card config-card" style="border-color:var(--warn-line);background:var(--warn-soft);">
      <h3 style="margin:0 0 4px;font-size:13px;font-weight:650;color:var(--warn);">Excel-library geblokkeerd</h3>
      <p class="muted" style="margin:0;font-size:12.5px;">
        SheetJS (xlsx) wordt van CDN geladen. In sommige previews of offline-omgevingen is dat geblokkeerd ---
        dan werkt upload niet. Open het portaal in een browser met internetverbinding. De opgeslagen
        configuratie blijft daarna ook zonder netwerk werken.
      </p>
    </div>`;
  }

  /* ---- guided work flow editor (admin) ---- */
  let wfDraft = null;

  function wfDefaultDraft() {
    return DEFAULT_WORK_STEPS.map((st) => ({
      key: st.key,
      title: t('work.step.' + st.key),
      desc: t('work.step.' + st.key + 'd'),
      checks: st.checks.map((c) => ({ key: c, label: t('work.check.' + c) })),
      next: typeof st.next === 'function'
        ? (st.next({ status: 'Received' }) || st.next({ status: 'Processing' }) || st.next({ status: 'QC_Review' }) || '')
        : (st.next || ''),
      pick: !!st.pick,
      qcGate: !!st.qcGate,
    }));
  }

  async function loadWorkflowEditor() {
    try {
      const d = await api('GET', '/api/v1/workflow');
      wfDraft = d.steps
        ? d.steps.map((st) => ({ key: st.key, title: st.title || '', desc: st.desc || '', checks: (st.checks || []).map((c) => ({ key: c.key, label: c.label })), next: st.next || '', pick: !!st.pick, qcGate: !!st.qcGate }))
        : wfDefaultDraft();
    } catch { wfDraft = wfDefaultDraft(); }
    renderWorkflowEditor();
  }

  function renderWorkflowEditor() {
    const host = $('#wf-steps');
    if (!host || !wfDraft) return;
    host.innerHTML = wfDraft.map((st, i) => `
      <div class="wf-step" data-i="${i}">
        <div class="wf-step-head">
          <span class="wf-n">${i + 1}</span>
          <input data-f="title" value="${esc(st.title)}" placeholder="Step title" maxlength="60">
          <select data-f="next" title="Status move on Next">
            <option value="">no status change</option>
            ${['Received', 'Processing', 'QC_Review', 'Completed'].map((v) => `<option value="${v}"${st.next === v ? ' selected' : ''}>→ ${v.replace('_', ' ')}</option>`).join('')}
          </select>
          <label class="wf-flag"><input type="checkbox" data-f="pick" ${st.pick ? 'checked' : ''}> pick list</label>
          <label class="wf-flag"><input type="checkbox" data-f="qcGate" ${st.qcGate ? 'checked' : ''}> QC gate</label>
          <span class="wf-ops">
            <button type="button" class="icon-btn" data-op="up" ${i === 0 ? 'disabled' : ''} title="Move up">↑</button>
            <button type="button" class="icon-btn" data-op="down" ${i === wfDraft.length - 1 ? 'disabled' : ''} title="Move down">↓</button>
            <button type="button" class="icon-btn" data-op="del" title="Delete step">✕</button>
          </span>
        </div>
        <input data-f="desc" class="wf-desc" value="${esc(st.desc)}" placeholder="Short description shown under the title" maxlength="200">
        <div class="wf-checks">
          ${st.checks.map((c, ci) => `
            <span class="wf-checkrow">
              <input data-f="check" data-ci="${ci}" value="${esc(c.label)}" placeholder="Check label" maxlength="120">
              <button type="button" class="icon-btn" data-op="delcheck" data-ci="${ci}" title="Remove check">✕</button>
            </span>`).join('')}
        </div>
        <button type="button" class="btn btn-ghost btn-sm" data-op="addcheck">+ manual check</button>
      </div>`).join('');
  }

  function wfCollect() {
    return wfDraft.map((st, i) => ({
      key: st.key || ('step' + (i + 1)),
      title: st.title, desc: st.desc,
      checks: st.checks.map((c, ci) => ({ key: c.key || ('k' + (ci + 1)), label: c.label })),
      next: st.next || null, pick: st.pick, qcGate: st.qcGate,
    }));
  }

  function bindWorkflowEditor(root) {
    if (!root || root._wfBound) return;
    root._wfBound = true;
    const stepsHost = $('#wf-steps', root);
    stepsHost.addEventListener('input', (e) => {
      const stepEl = e.target.closest('.wf-step');
      if (!stepEl) return;
      const st = wfDraft[Number(stepEl.dataset.i)];
      const f = e.target.dataset.f;
      if (f === 'title') st.title = e.target.value;
      if (f === 'desc') st.desc = e.target.value;
      if (f === 'check') st.checks[Number(e.target.dataset.ci)].label = e.target.value;
    });
    stepsHost.addEventListener('change', (e) => {
      const stepEl = e.target.closest('.wf-step');
      if (!stepEl) return;
      const st = wfDraft[Number(stepEl.dataset.i)];
      if (e.target.dataset.f === 'next') st.next = e.target.value;
      if (e.target.dataset.f === 'pick') st.pick = e.target.checked;
      if (e.target.dataset.f === 'qcGate') st.qcGate = e.target.checked;
    });
    stepsHost.addEventListener('click', (e) => {
      const btn = e.target.closest('[data-op]');
      if (!btn) return;
      const stepEl = btn.closest('.wf-step');
      const i = Number(stepEl.dataset.i);
      const op = btn.dataset.op;
      if (op === 'up' && i > 0) { [wfDraft[i - 1], wfDraft[i]] = [wfDraft[i], wfDraft[i - 1]]; }
      if (op === 'down' && i < wfDraft.length - 1) { [wfDraft[i + 1], wfDraft[i]] = [wfDraft[i], wfDraft[i + 1]]; }
      if (op === 'del') { wfDraft.splice(i, 1); }
      if (op === 'delcheck') { wfDraft[i].checks.splice(Number(btn.dataset.ci), 1); }
      if (op === 'addcheck') { wfDraft[i].checks.push({ key: '', label: '' }); }
      renderWorkflowEditor();
    });
    $('#wf-add-step', root).addEventListener('click', () => {
      wfDraft.push({ key: '', title: '', desc: '', checks: [{ key: '', label: '' }], next: '', pick: false, qcGate: false });
      renderWorkflowEditor();
    });
    $('#wf-save', root).addEventListener('click', async () => {
      try {
        await api('PUT', '/api/v1/workflow', { steps: wfCollect() });
        toast('Work flow saved — everyone uses it now');
        if (window.WorkFlow) WorkFlow.loadFlow(true);
      } catch (err) { toast(err.message, true); }
    });
    $('#wf-reset', root).addEventListener('click', async () => {
      try {
        await api('PUT', '/api/v1/workflow', { steps: null });
        wfDraft = wfDefaultDraft();
        renderWorkflowEditor();
        if (window.WorkFlow) WorkFlow.loadFlow(true);
        toast('Work flow reset to the built-in default');
      } catch (err) { toast(err.message, true); }
    });
  }

  function renderConfig() {
    const m = state.config.meta || {};
    const sources = [
      { key: 'artikelweergave', label: 'Artikelweergave', desc: 'Artikelcode -> magazijnlocatie', handler: handleArtikelweergave },
      { key: 'klantenlijst',    label: 'Klantenlijst',    desc: "Klantnummer -> groep (Prio's / InControl / Overig)", handler: handleKlantenlijst },
      { key: 'pqRegels',        label: 'Uitsluitingsregels', desc: 'Artikelen en klanten die altijd worden uitgesloten (uit PQ_regels)', handler: handlePQRegels },
      { key: 'excelRegels',     label: 'Kleurregels',     desc: 'Uitzonderingen/aandachtspunten per artikel (uit Excel_regels)', handler: handleExcelRegels },
    ];
    const alleVier = sources.every((s) => !!m[s.key]);
    const metaTxt = (meta) => meta
      ? `${esc(meta.bestand)} · bijgewerkt ${fmtTime(meta.tijd)}`
      : 'nog niet geüupload';

    let html = '';
    if (!isXlsxBeschikbaar()) html += configCompatibiliteitsWaarschuwing();

    html += `<div class="card config-card">
      <h3 class="config-card-title">Excel-bronnen</h3>
      <p class="muted config-card-sub">Upload de vier one-time Excel-bronnen. Elke brons wordt omgezet naar de gedeelde orderpick-config.json.</p>
      <div class="config-section">`;
    for (const s of sources) {
      const meta = m[s.key];
      html += `<div class="config-row">
        <div class="config-row-main">
          <div class="config-row-title">${esc(s.label)}</div>
          <div class="config-row-desc muted">${esc(s.desc)}</div>
          <div class="config-meta muted">${metaTxt(meta)}</div>
        </div>
        <button type="button" class="btn ${meta ? 'btn-ghost' : 'btn-primary'}" data-cfg-src="${s.key}">${meta ? 'Vervangen' : 'Uploaden'}</button>
      </div>`;
    }
    html += `</div></div>`;

    html += `<div class="card config-card">
      <h3 class="config-card-title">Configuratie bundelen</h3>
      <p class="muted config-card-sub">
        Download orderpick-config.json en plaats het in de gedeelde map naast deze portal-pagina — dan wordt
        het bij het openen automatisch geladen, zodat medewerkers elke dag alleen nog de verkoopregels hoeven te importeren.
      </p>
      <div class="config-bundle">
        <button type="button" id="cfg-download" class="btn btn-primary" ${alleVier ? '' : 'disabled'}>⬇ Downloaden als orderpick-config.json</button>
        <button type="button" id="cfg-upload" class="btn btn-ghost">⬆ Configuratie laden vanuit JSON</button>
      </div>
      ${alleVier ? '' : '<p class="muted config-card-sub">Upload eerst alle vier bronnen hierboven voordat je kunt bundelen.</p>'}
      ${state.configBron ? `<p class="muted config-meta">Actieve configuratie geladen via: ${esc(state.configBron)}</p>` : ''}
    </div>`;

    const pickMeta = state.picklistMeta || {};
    html += `<div class="card config-card">
      <h3 class="config-card-title">Dagelijkse picklijst (verkoopregels)</h3>
      <p class="muted config-card-sub">
        Importeer het dagelijkse verkoopregels-Excel. Orders die in de API bestaan krijgen per order
        een live pick-checklist (locatie + artikel) die het hele team realtime kan afvinken — met naam en tijdstip.

      <div class="config-bundle">
        <button type="button" id="picklist-upload" class="btn btn-primary">⬆ Verkoopregels importeren (.xlsx)</button>
      </div>
      ${pickMeta.bestand
        ? `<p class="muted config-meta">Laatste import: ${esc(pickMeta.bestand)} · ${pickMeta.orders}/${pickMeta.totaal} orders bijgewerkt${pickMeta.missed ? ` · ${pickMeta.missed} niet gevonden` : ''} · ${fmtTime(pickMeta.tijd)}</p>`
        : '<p class="muted config-meta">Nog geen picklijst geimporteerd. Upload het dagelijkse bestand om checklists te genereren.</p>'}
    </div>`;

    $('#config-content').innerHTML = html + `
      <section class="card wf-editor" id="wf-editor">
        <div class="wf-head">
          <div><h3>Guided work flow</h3><p class="muted">The steps, manual checks and status moves behind every Start Work session — saved on the server for the whole team.</p></div>
          <div class="wf-head-actions">
            <button type="button" class="btn btn-ghost btn-sm" id="wf-reset">Reset to default</button>
            <button type="button" class="btn btn-primary btn-sm" id="wf-save">Save flow</button>
          </div>
        </div>
        <div id="wf-steps"></div>
        <button type="button" class="btn btn-secondary btn-sm" id="wf-add-step">+ Add step</button>
      </section>`;
    bindWorkflowEditor($('#config-content'));
    loadWorkflowEditor();

    const downloadBtn = $('#cfg-download');
    if (downloadBtn) downloadBtn.addEventListener('click', downloadConfigJSON);
    const uploadBtn = $('#cfg-upload');
    if (uploadBtn) uploadBtn.addEventListener('click', () => fileInput('.json', laadConfigVanuitJSON).click());
    $$('[data-cfg-src]').forEach((b) => {
      b.addEventListener('click', () => {
        const s = sources.find((x) => x.key === b.dataset.cfgSrc);
        if (s) fileInput('.xlsx', s.handler).click();
      });
    });
    const pickBtn = $('#picklist-upload');
    if (pickBtn) pickBtn.addEventListener('click', () => fileInput('.xlsx', handleVerkoopregels).click());
  }

