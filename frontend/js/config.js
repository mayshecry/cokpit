'use strict';

  

  function initSettingsPop() {
    const pop = $('#settings-pop');
    $('#settings-btn').addEventListener('click', (e) => {
      e.stopPropagation();
      pop.classList.toggle('hidden');
      if (pop.classList.contains('hidden')) return;
      $('#pref-theme').value = localStorage.getItem('cockpit_theme') || 'system';
      $('#pref-density').value = localStorage.getItem('cockpit_density') || 'spacious';
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
      const res = await fetch('/api/v1/admin/backup', {
        credentials: 'same-origin',
        headers: state.token ? { Authorization: 'Bearer ' + state.token } : {},
      });
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

                                                 
let wfDraft = null;
  let wfScope = '';
  let wfProfileID = 0;
  let wfProfileName = '';
  let wfProfiles = [];
  const wfDrafts = { '': null };
  let custList = [];                                                

  async function refreshCustomersCard() {
    if (window.SLA && typeof SLA.ensureDefaultCustomers === 'function') {
      await SLA.ensureDefaultCustomers();
    }
    try {
      const d = await api('GET', '/api/v1/customers');
      const raw = d.customers || [];
      custList = window.SLA ? SLA.mergeCustomersWithDefaults(raw) : raw.sort((a, b) => a.name.localeCompare(b.name));
    } catch {
      custList = window.SLA ? SLA.mergeCustomersWithDefaults([]) : [];
    }
    paintCustomersCard();
    fillScopeOptions();
  }

  function paintCustomersCard() {
    const host = $('#cust-list');
    if (!host) return;
    const cc = $('#cust-count');
    const withFlow = custList.filter((c) => c.hasFlow).length;
    const withSla = custList.filter((c) => window.SLA && SLA.getCustomerDays(c.name)).length;
    if (cc) cc.textContent = custList.length ? `${custList.length} total · ${withSla} SLA rules · ${withFlow} own flow` : '';
    if (!custList.length) { host.innerHTML = '<p class="muted cust-empty">No customers yet — create the first one above.</p>'; return; }
    const q = custFilter.trim().toLowerCase();
    const view = q ? custList.filter((c) => c.name.toLowerCase().includes(q)) : custList;
    if (!view.length) { host.innerHTML = `<p class="muted cust-empty">Nothing matches &ldquo;${esc(custFilter)}&rdquo;.</p>`; return; }
    const defDays = window.SLA ? SLA.getDefaults().defaultWorkdays : 2;
    const skipWk = window.SLA ? SLA.excludeWeekends() : true;
    host.innerHTML = view.map((c) => {
      const slaDays = window.SLA ? SLA.getCustomerDays(c.name) : null;
      const effDays = slaDays || defDays;
      return `
      <div class="cust-row" data-cust="${esc(c.name)}">
        <span class="cust-name">${esc(c.name)}</span>
        ${c.number ? `<span class="chip mono cust-num">${esc(c.number)}</span>` : ''}
        ${c.custom ? '<span class="chip cust-chip">custom</span>' : ''}
        ${c.hasFlow ? '<span class="chip chip-ok cust-chip">own flow</span>' : ''}
        <span class="cust-sla-wrap" title="Customer SLA in workdays (${skipWk ? 'weekends excluded' : 'calendar days'})">
          <span class="cust-sla-lbl">SLA</span>
          <input type="number" min="1" max="60" step="1" class="cust-sla-inp mono" data-cust-sla="${esc(c.name)}" value="${effDays}" aria-label="SLA workdays for ${esc(c.name)}">
          <span class="cust-sla-unit">${effDays === 1 ? 'workday' : 'workdays'}${slaDays ? '' : ' (default)'}</span>
        </span>
        <span class="cust-row-acts">
          ${c.custom && !c.managed ? '<button type="button" class="btn btn-ghost btn-sm cust-adopt" title="Add to the customer directory">Adopt</button>' : ''}
          <button type="button" class="btn btn-ghost btn-sm cust-edit">${c.hasFlow ? 'Edit flow' : 'Own flow'}</button>
        </span>
      </div>`;
    }).join('');
  }

  let custFilter = '';

  function customersCardHtml() {
    const defs = window.SLA ? SLA.getDefaults() : { defaultWorkdays: 2, excludeWeekends: true };
    return `
      <section class="card cust-card" id="cust-card">
        <div class="cfg-sec-head">
          <div class="cfg-sec-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="8" r="3.2"/><path d="M3.4 19.2a5.6 5.6 0 0 1 11.2 0"/><circle cx="17" cy="9.4" r="2.5"/><path d="M15.4 14.2a5 5 0 0 1 5.3 4.6"/></svg></div>
          <div class="cfg-sec-txt"><h3>Customers &amp; SLA Policies</h3><p class="muted">Directory of customers known from orders and SIs. Configure per-customer SLA in workdays (e.g. <strong>Kenter = 2 workdays</strong>, <strong>Dela = 5 workdays</strong>) with weekend exclusion.</p></div>
          <div class="cfg-sec-acts">
            <button type="button" class="btn btn-primary btn-sm" id="cust-new">+ New customer</button>
          </div>
        </div>
        <div class="cust-sla-bar">
          <label class="cust-sla-toggle">
            <input type="checkbox" id="sla-exclude-weekends" ${defs.excludeWeekends !== false ? 'checked' : ''}>
            <span>Exclude weekends (Sat &amp; Sun do not count toward SLA)</span>
          </label>
          <div class="cust-sla-def">
            <label for="sla-default-days">Default SLA:</label>
            <input type="number" id="sla-default-days" class="cust-sla-inp mono" min="1" max="60" value="${defs.defaultWorkdays || 2}">
            <span class="muted">workdays</span>
          </div>
          <span class="cfg-pill ok">Kenter: ${window.SLA ? SLA.effectiveDays('Kenter') : 2}d · Dela: ${window.SLA ? SLA.effectiveDays('Dela') : 5}d</span>
        </div>
        <div class="cust-tools">
          <input id="cust-filter" class="cfg-search" type="search" placeholder="Filter customers&hellip;" autocomplete="off" spellcheck="false">
          <span class="cfg-pill neutral" id="cust-count"></span>
        </div>
        <div id="cust-list"><p class="muted cust-empty">Loading&hellip;</p></div>
      </section>`;
  }

  async function newCustomerModal(prefillName) {
    const defDays = window.SLA ? SLA.getDefaults().defaultWorkdays : 2;
    const vals = await modal({
      title: 'New customer',
      description: 'Adds the customer to the directory with its SLA workday target (weekends excluded).',
      confirmLabel: 'Create customer',
      fields: [
        { name: 'name', label: 'Name', placeholder: 'e.g. Kenter, Dela, Gemeente Sittard', value: prefillName || '', required: true },
        { name: 'number', label: 'Customer number (optional)', placeholder: 'e.g. 1042', hint: 'Leave empty for an automatic number.', half: true },
        { name: 'slaDays', label: 'SLA (workdays, excl. weekends)', type: 'number', value: String(defDays), placeholder: 'e.g. 2', hint: 'Saturdays and Sundays are skipped automatically.', half: true },
      ],
    });
    if (!vals) return;
    const name = String(vals.name || '').trim();
    const slaDays = Number(vals.slaDays);
    if (window.SLA && name && slaDays > 0) {
      SLA.setCustomerDays(name, slaDays, SLA.excludeWeekends());
    }
    try {
      const r = await api('POST', '/api/v1/customers', { name, number: String(vals.number || '').trim() });
      toast(`Customer "${r.customer.name}" created` + (slaDays > 0 ? ` · SLA ${slaDays} workday${slaDays === 1 ? '' : 's'}` : ''));
      await refreshCustomersCard();
      switchScopeTo(r.customer.name);
    } catch (err) {
      await refreshCustomersCard();
      toast(err.message, true);
    }
  }

  async function switchScopeTo(name) {
    wfDrafts[wfScope] = wfDraft;
    wfScope = name;
    wfProfileID = 0;
    wfProfileName = '';
    const sel = $('#wf-scope'); if (sel) sel.value = name;
    const ed = $('#wf-editor'); if (ed) ed.scrollIntoView({ behavior: 'smooth', block: 'start' });
    await loadWorkflowProfiles();
  }

  function fillScopeOptions() {
    const sel = $('#wf-scope');
    if (!sel) return;
    const flowNames = new Set(custList.filter((c) => c.hasFlow).map((c) => c.name));
    sel.innerHTML = '<option value="">Global default</option>' + custList.map((c) =>
      `<option value="${esc(c.name)}"${c.name === wfScope ? ' selected' : ''}>${esc(c.name)}${flowNames.has(c.name) ? ' ●' : ''}</option>`).join('');
    renderWorkflowProfilePicker();
    updateScopeUI();
  }

  function updateScopeUI() {
    const scoped = !!wfScope;
    const box = $('#wf-scopebox'); if (box) box.classList.toggle('scoped', scoped);
    const ed = $('#wf-editor'); if (ed) ed.classList.toggle('scoped', scoped);
    const vc = $('#wf-scope-val'); if (vc) vc.classList.toggle('hidden', !scoped);
    const gc = $('#wf-global-chip'); if (gc) gc.classList.toggle('hidden', scoped);
    const profileTools = $('#wf-profile-tools'); if (profileTools) profileTools.classList.toggle('hidden', !scoped);
    const desc = $('#wf-scope-desc');
    if (desc) desc.innerHTML = scoped
      ? `Choose a saved profile for <b>${esc(wfScope)}</b>, or edit the legacy customer flow. Profiles can be selected when creating orders.`
      : 'Default for every customer';
    const rst = $('#wf-reset');
    if (rst) {
      rst.textContent = scoped ? 'Remove custom flow' : 'Reset to default';
      rst.classList.toggle('hidden', scoped && (!!wfProfileID || !!wfProfileName));
    }
  }

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
      const d = wfProfileID
        ? await api('GET', `/api/v1/workflow-profiles/${wfProfileID}`)
        : await api('GET', '/api/v1/workflow' + (wfScope ? '?customer=' + encodeURIComponent(wfScope) : ''));
      const steps = wfProfileID ? (d.profile && d.profile.steps) : d.steps;
      wfDraft = steps
        ? steps.map((st) => ({ key: st.key, title: st.title || '', desc: st.desc || '', location: st.location || '', checks: (st.checks || []).map((c) => ({ key: c.key, label: c.label })), next: st.next || '', pick: !!st.pick, qcGate: !!st.qcGate }))
        : wfDefaultDraft();
    } catch { wfDraft = wfDefaultDraft(); }
    wfDrafts[wfScope] = wfDraft;
    renderWorkflowEditor();
    updateScopeUI();
  }

  async function loadWorkflowProfiles(preferredID = 0) {
    wfProfiles = [];
    if (!wfScope) { renderWorkflowProfilePicker(); updateScopeUI(); return; }
    try {
      const d = await api('GET', '/api/v1/workflow-profiles?customer=' + encodeURIComponent(wfScope));
      wfProfiles = d.profiles || [];
    } catch { wfProfiles = []; }
    const preferred = wfProfiles.find((p) => Number(p.id) === Number(preferredID));
    const chosen = preferred || wfProfiles[0] || null;
    wfProfileID = chosen ? Number(chosen.id) : 0;
    wfProfileName = chosen ? chosen.name : '';
    renderWorkflowProfilePicker();
    updateScopeUI();
    await loadWorkflowEditor();
  }

  function renderWorkflowProfilePicker() {
    const sel = $('#wf-profile');
    if (!sel) return;
    const opts = ['<option value="">Legacy customer flow</option>'].concat(wfProfiles.map((p) =>
      `<option value="${p.id}"${Number(p.id) === Number(wfProfileID) ? ' selected' : ''}>${esc(p.name)}</option>`));
    if (wfProfileName && !wfProfileID) opts.push(`<option value="new" selected>New profile: ${esc(wfProfileName)} (unsaved)</option>`);
    if (!wfProfileID && !wfProfileName) opts[0] = '<option value="" selected>Legacy customer flow</option>';
    sel.innerHTML = opts.join('');
  }

  async function newWorkflowProfile() {
    if (!wfScope) return;
    const vals = await modal({
      title: 'New workflow profile',
      description: `Create a named workflow for ${wfScope}. You can select it when creating an order for this customer.`,
      confirmLabel: 'Create profile',
      fields: [{ name: 'name', label: 'Profile name', placeholder: 'e.g. Standard setup, Repair, Reimage', required: true }],
    });
    if (!vals) return;
    const name = String(vals.name || '').trim();
    if (!name) return;
    wfProfileID = 0;
    wfProfileName = name;
    wfDraft = wfDefaultDraft();
    renderWorkflowProfilePicker();
    renderWorkflowEditor();
    updateScopeUI();
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
        <input data-f="location" class="wf-location" value="${esc(st.location || '')}" placeholder="Station / table / area (e.g. Bench 3, QC table)" maxlength="80" aria-label="Work location">
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
      title: st.title, desc: st.desc, location: st.location || '',
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
      if (f === 'location') st.location = e.target.value;
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
      wfDraft.push({ key: '', title: '', desc: '', location: '', checks: [{ key: '', label: '' }], next: '', pick: false, qcGate: false });
      renderWorkflowEditor();
    });
    $('#wf-save', root).addEventListener('click', async () => {
      try {
        const body = { name: wfProfileName, steps: wfCollect() };
        if (wfScope && wfProfileName) {
          const result = wfProfileID
            ? await api('PUT', `/api/v1/workflow-profiles/${wfProfileID}`, body)
            : await api('POST', '/api/v1/workflow-profiles?customer=' + encodeURIComponent(wfScope), body);
          wfProfileID = Number(result.profile.id);
          wfProfileName = result.profile.name;
          await loadWorkflowProfiles(wfProfileID);
          toast(`Workflow profile “${wfProfileName}” saved for ${wfScope}`);
        } else {
          const qs = wfScope ? '?customer=' + encodeURIComponent(wfScope) : '';
          await api('PUT', '/api/v1/workflow' + qs, { steps: wfCollect() });
          toast(wfScope ? `Customer flow saved — only "${wfScope}" orders use it` : 'Work flow saved — everyone uses it now');
        }
        if (window.WorkFlow) WorkFlow.loadFlow(true, true);
        refreshCustomersCard();
      } catch (err) { toast(err.message, true); }
    });
    $('#wf-reset', root).addEventListener('click', async () => {
      if (wfScope) {
        const ok = await modal({
          title: 'Remove custom flow?',
          description: `"${wfScope}" will follow the default guided flow again. Open work sessions keep the flow they started with.`,
          confirmLabel: 'Remove override',
        });
        if (!ok) return;
      }
      try {
        const qs = wfScope ? '?customer=' + encodeURIComponent(wfScope) : '';
        await api('PUT', '/api/v1/workflow' + qs, { steps: null });
        wfDraft = wfDefaultDraft();
        wfDrafts[wfScope] = wfDraft;
        renderWorkflowEditor();
        updateScopeUI();
        if (window.WorkFlow) WorkFlow.loadFlow(true, true);
        if (wfScope) { toast(`Custom flow removed — "${wfScope}" uses the default again`); refreshCustomersCard(); }
        else toast('Work flow reset to the built-in default');
      } catch (err) { toast(err.message, true); }
    });

    const scopeSel = $('#wf-scope', root);
    if (scopeSel) scopeSel.addEventListener('change', async () => {
      wfDrafts[wfScope] = wfDraft;
      wfScope = scopeSel.value;
      wfProfileID = 0;
      wfProfileName = '';
      if (wfScope) await loadWorkflowProfiles();
      else { wfProfiles = []; renderWorkflowProfilePicker(); await loadWorkflowEditor(); }
    });
    const profileSel = $('#wf-profile', root);
    if (profileSel) profileSel.addEventListener('change', async () => {
      if (profileSel.value === 'new') return;
      wfProfileID = Number(profileSel.value) || 0;
      const chosen = wfProfiles.find((p) => Number(p.id) === wfProfileID);
      wfProfileName = chosen ? chosen.name : '';
      await loadWorkflowEditor();
      renderWorkflowProfilePicker();
      updateScopeUI();
    });
    const newProfileBtn = $('#wf-profile-new', root);
    if (newProfileBtn) newProfileBtn.addEventListener('click', newWorkflowProfile);
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

    const loaded = sources.filter((s) => !!m[s.key]).length;
    const progress = Math.round((loaded / sources.length) * 100);
    const xlsxIco = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2.5H6.5A1.5 1.5 0 0 0 5 4v16a1.5 1.5 0 0 0 1.5 1.5h11A1.5 1.5 0 0 0 19 20V7.5Z"/><path d="M14 2.5v5h5"/><path d="m9.2 12.2 5.6 5.6m0-5.6-5.6 5.6"/></svg>';
    html += `<section class="card config-card cfg-card">
      <div class="cfg-sec-head">
        <div class="cfg-sec-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19V5a2 2 0 0 1 2-2h8.5L20 8.5V19a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2Z"/><path d="M14 3v6h6"/></svg></div>
        <div class="cfg-sec-txt"><h3>Excel-bronnen</h3><p class="muted">Upload de vier one-time Excel-bronnen. Elke bron wordt omgezet naar de gedeelde orderpick-config.json.</p></div>
        <div class="cfg-sec-acts"><span class="cfg-pill ${loaded === sources.length ? 'ok' : 'neutral'}" id="cfg-progress">${loaded} van ${sources.length} geladen</span></div>
      </div>
      <div class="cfg-src-grid">`;
    for (const s of sources) {
      const meta = m[s.key];
      html += `<button type="button" class="cfg-src ${meta ? 'loaded' : ''}" data-cfg-src="${s.key}">
        <span class="cfg-src-ico">${xlsxIco}</span>
        <span class="cfg-src-main">
          <span class="cfg-src-title">${esc(s.label)}</span>
          <span class="cfg-src-desc muted">${esc(s.desc)}</span>
          <span class="cfg-src-meta muted">${metaTxt(meta)}</span>
        </span>
        <span class="cfg-src-side">
          <span class="cfg-pill ${meta ? 'ok' : 'warn'}">${meta ? 'Geladen' : 'Ontbreekt'}</span>
          <span class="cfg-src-act">${meta ? 'Vervangen' : 'Uploaden'}</span>
        </span>
      </button>`;
    }
    html += `</div>
      <div class="cfg-progress"><div class="cfg-progress-track"><div class="cfg-progress-fill" style="width:${progress}%"></div></div><span class="muted">deelbaar als orderpick-config.json</span></div>
    </section>`;

    const pickMeta = state.picklistMeta || {};
    html += `<section class="card config-card cfg-card">
      <div class="cfg-sec-head">
        <div class="cfg-sec-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12.5 21H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h8.5L20 8.5V12"/><path d="M14 3v6h6"/><path d="M12 17.5h7.5m0 0-2.6-2.6m2.6 2.6-2.6 2.6"/></svg></div>
        <div class="cfg-sec-txt"><h3>Bundel &amp; dagelijkse picklijst</h3><p class="muted">Zet de gebundelde config naast de portal-pagina zodat hij automatisch laadt — daarna hoeft het team elke dag alleen de verkoopregels te importeren.</p></div>
      </div>
      <div class="cfg-duo">
        <div class="cfg-duo-cell">
          <span class="cfg-pill ${alleVier ? 'ok' : 'warn'}">${alleVier ? '4/4 bronnen geladen' : `nog ${sources.length - loaded} bron${sources.length - loaded === 1 ? '' : 'nen'} nodig`}</span>
          <p class="muted cfg-duo-desc">orderpick-config.json bevat alle vier bronnen én staat klaar voor de gedeelde map.</p>
          <div class="cfg-duo-acts">
            <button type="button" id="cfg-download" class="btn btn-primary btn-sm" ${alleVier ? '' : 'disabled'}>&darr; Download orderpick-config.json</button>
            <button type="button" id="cfg-upload" class="btn btn-ghost btn-sm">&uarr; Laden vanuit JSON</button>
          </div>
          ${state.configBron ? `<p class="muted config-meta">Actieve configuratie geladen via: ${esc(state.configBron)}</p>` : ''}
        </div>
        <div class="cfg-duo-cell">
          <span class="cfg-pill ${pickMeta.bestand ? 'ok' : 'neutral'}">${pickMeta.bestand ? 'Picklijst actief' : 'Dagelijks ritueel'}</span>
          <p class="muted cfg-duo-desc">Verkoopregels-Excel &rarr; live pick-checklists (locatie + artikel) die het team realtime afvinkt.</p>
          <div class="cfg-duo-acts">
            <button type="button" id="picklist-upload" class="btn btn-primary btn-sm">&uarr; ${pickMeta.bestand ? 'Nieuwe verkoopregels importeren' : 'Verkoopregels importeren (.xlsx)'}</button>
          </div>
          ${pickMeta.bestand
            ? `<p class="muted config-meta">Laatste import: ${esc(pickMeta.bestand)} · ${pickMeta.orders}/${pickMeta.totaal} orders bijgewerkt${pickMeta.missed ? ` · ${pickMeta.missed} niet gevonden` : ''} · ${fmtTime(pickMeta.tijd)}</p>`
            : '<p class="muted config-meta">Nog geen picklijst ge&iuml;mporteerd. Upload het dagelijkse bestand om checklists te genereren.</p>'}
        </div>
      </div>
    </section>`;

    $('#config-content').innerHTML = html + customersCardHtml() + `
      <section class="card wf-editor" id="wf-editor">
        <div class="cfg-sec-head">
          <div class="cfg-sec-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="5.5" cy="6" r="2.1"/><circle cx="5.5" cy="18" r="2.1"/><circle cx="18.5" cy="12" r="2.6"/><path d="M7.6 6h6.5a2.9 2.9 0 0 1 0 5.8H9.9A2.9 2.9 0 0 0 7 14.7V16"/></svg></div>
          <div class="cfg-sec-txt"><h3>Guided work flow</h3><p class="muted">The steps, manual checks and status moves behind every Start Work session — saved on the server for the whole team. Add a station or table to each step: workers see where to go before starting and while doing that step. Pick a customer below to edit its default flow or create named profiles that can be selected on each new order.</p></div>
          <div class="cfg-sec-acts wf-head-actions">
            <button type="button" class="btn btn-ghost btn-sm" id="wf-reset">Reset to default</button>
            <button type="button" class="btn btn-primary btn-sm" id="wf-save">Save flow</button>
          </div>
        </div>
        <div class="wf-scopebox" id="wf-scopebox">
          <label class="wf-scope-l" for="wf-scope">Editing</label>
          <select id="wf-scope" class="wf-scope-select"></select>
          <span class="chip hidden" id="wf-global-chip">Default</span>
          <span class="chip chip-ok hidden" id="wf-scope-val">Custom</span>
          <span class="wf-scope-desc muted" id="wf-scope-desc">Default for every customer</span>
          <div class="wf-profile-tools hidden" id="wf-profile-tools">
            <label for="wf-profile">Customer workflow</label>
            <select id="wf-profile" class="wf-profile-select"></select>
            <button type="button" class="btn btn-secondary btn-sm" id="wf-profile-new">+ New profile</button>
          </div>
        </div>
        <div id="wf-steps"></div>
        <button type="button" class="btn btn-secondary btn-sm" id="wf-add-step">+ Add step</button>
      </section>`;
    bindWorkflowEditor($('#config-content'));
    loadWorkflowEditor();
    fillScopeOptions();
    const newBtn = $('#cust-new');
    if (newBtn) newBtn.addEventListener('click', () => newCustomerModal());
    const wkChk = $('#sla-exclude-weekends');
    if (wkChk) wkChk.addEventListener('change', () => {
      if (window.SLA) SLA.setDefaults({ excludeWeekends: wkChk.checked });
      paintCustomersCard();
      toast(wkChk.checked ? 'SLA now excludes weekends (Sat & Sun)' : 'SLA now counts calendar days');
    });
    const defDayInp = $('#sla-default-days');
    if (defDayInp) defDayInp.addEventListener('change', () => {
      const v = Math.max(1, Math.min(60, Math.round(Number(defDayInp.value) || 2)));
      defDayInp.value = String(v);
      if (window.SLA) SLA.setDefaults({ defaultWorkdays: v });
      paintCustomersCard();
      toast(`Default SLA set to ${v} workday${v === 1 ? '' : 's'}`);
    });
    const custHost = $('#cust-list');
    if (custHost) {
      custHost.addEventListener('change', (e) => {
        const inp = e.target.closest('[data-cust-sla]');
        if (!inp || !window.SLA) return;
        const name = inp.dataset.custSla;
        const days = Math.max(1, Math.min(60, Math.round(Number(inp.value) || 2)));
        inp.value = String(days);
        SLA.setCustomerDays(name, days, SLA.excludeWeekends());
        paintCustomersCard();
        toast(`${name} SLA set to ${days} workday${days === 1 ? '' : 's'} (weekends excluded)`);
      });
      custHost.addEventListener('click', async (e) => {
        if (e.target.closest('[data-cust-sla]')) return;
        const row = e.target.closest('.cust-row');
        if (!row) return;
        const name = row.dataset.cust;
        if (e.target.closest('.cust-edit')) { switchScopeTo(name); return; }
        if (e.target.closest('.cust-adopt')) {
          try { await api('POST', '/api/v1/customers', { name, number: '' }); toast(`"${name}" added to the directory`); refreshCustomersCard(); }
          catch (err) { toast(err.message, true); }
        }
      });
    }
    refreshCustomersCard();
    const custSearch = $('#cust-filter');
    if (custSearch) custSearch.addEventListener('input', () => { custFilter = custSearch.value; paintCustomersCard(); });

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

