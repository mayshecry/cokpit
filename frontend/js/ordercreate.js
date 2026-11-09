'use strict';

                                             
                                                                                                              
                                                                                                                              
                                                    

(function () {
  let customers = [];
  let selected = null;                                                      
  let ranDevices = [];
  let selectedRanDevice = null;
  let deviceMode = 'ran';                    
  let activeWorkdays = null;
  let excludeWeekends = true;
  let cleanup = null;
  let workflowProfiles = [];
  let workflowProfilesRequest = Promise.resolve();

  const toLocalInput = (d) => {
    const dt = d instanceof Date ? d : new Date(d);
    const t = new Date(dt.getTime() - dt.getTimezoneOffset() * 60000);
    return t.toISOString().slice(0, 16);
  };

  function readTarget() {
    const inp = $('#oc-target');
    if (window.SLA) return SLA.parseTargetInput(inp && inp.value);
    const raw = (inp && inp.value) || '';
    if (!raw) return { iso: null, past: false, problem: '' };
    const dt = new Date(raw);
    if (Number.isNaN(dt.getTime())) return { iso: null, past: false, problem: 'Enter a valid target date and time.' };
    return { iso: dt.toISOString(), past: dt.getTime() < Date.now(), problem: '' };
  }


  function avatarHue(name) {
    let h = 0;
    const s = String(name || '');
    for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360;
    return h;
  }

  function avatarInitials(name) {
    const parts = String(name || '?').trim().split(/[\s\-_/]+/).filter(Boolean);
    if (!parts.length) return '?';
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
  }

  function formatDeviceTitle(d) {
    if (!d) return '';
    const mfr = String(d.manufacturer || '').trim();
    const mdl = String(d.model || '').trim();
    const full = [mfr, mdl].filter(Boolean).join(' ').trim();
    return full || d.computerName || ('Device #' + d.id);
  }

  function formatDeviceConfig(d) {
    if (!d) return '';
    const parts = [];
    if (d.cpuName) {
      const shortCpu = String(d.cpuName)
        .replace(/\(R\)|\(TM\)|CPU|Processor|11th Gen |12th Gen |13th Gen /gi, '')
        .replace(/\s+/g, ' ')
        .trim();
      parts.push(shortCpu);
    }
    if (d.ramText) parts.push(String(d.ramText).replace(/\s*\(.*?\)/g, '').trim() + ' RAM');
    if (d.storageGB) parts.push(d.storageGB + ' GB SSD');
    if (d.os) parts.push(String(d.os).replace(/^Microsoft\s+/i, '').trim());
    return parts.join(' · ');
  }

  function formatDeviceAsset(d, detailDoc) {
    if (!d) return '';
    const ov = (detailDoc && detailDoc.overview) || {};
    const sn = ov.biosSerial || ov.assetTag || ov.enclosureSerial || '';
    const host = ov.computerName || d.computerName || '';
    if (sn && host && sn !== host) return `${sn} (${host})`;
    if (sn) return sn;
    if (host) return host;
    return String(d.deviceKey || '').replace(/^(UUID|NAME):/i, '');
  }

  function synthesizePrepLog({ host, device, config, asset, os }) {
    const cleanHost = String(host || asset || 'ESP-MANUAL-' + Math.floor(100 + Math.random() * 899))
      .trim()
      .replace(/[^A-Za-z0-9._-]/g, '-')
      .slice(0, 48) || 'ESP-MANUAL-01';
    const devStr = String(device || 'Custom Workstation').trim();
    const devParts = devStr.split(/\s+/);
    const mfr = devParts[0] || 'Custom';
    const model = devParts.slice(1).join(' ') || devStr;
    const cfgStr = String(config || 'Intel Core i7 · 16 GB RAM · 512 GB SSD').trim();
    const cpuOnly = cfgStr.split('·')[0].trim() || cfgStr;
    const ramMatch = /(\d+)\s*GB\s*(?:RAM)?/i.exec(cfgStr);
    const ramGB = ramMatch ? Number(ramMatch[1]) : 16;
    const diskMatch = /(\d+)\s*(TB|GB)\s*(?:SSD|NVMe|HDD)/i.exec(cfgStr);
    const diskGB = diskMatch ? (diskMatch[2].toUpperCase() === 'TB' ? Number(diskMatch[1]) * 1024 : Number(diskMatch[1])) : 512;
    const diskBytes = String(Math.round(diskGB * 1073741824));
    const serial = String(asset || cleanHost).trim();
    const osName = String(os || 'Microsoft Windows 11 Pro').trim();
    const now = new Date();
    const ts = (offsetSec) => {
      const t = new Date(now.getTime() + offsetSec * 1000);
      const pad = (n) => String(n).padStart(2, '0');
      return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`;
    };
    const lines = [
      `${ts(0)} - === Starting laptop prep on ${cleanHost} ===`,
      `${ts(1)} - CPU: ${cpuOnly}`,
      `${ts(2)} - RAM: ${ramGB} GB`,
      `${ts(3)} - OS: ${osName}`,
      `${ts(4)} - ----- DEVICE ID -----`,
      `${ts(5)} - ComputerName: ${cleanHost}`,
      `${ts(6)} - Manufacturer: ${mfr}`,
      `${ts(7)} - Model: ${model}`,
      `${ts(8)} - BIOS Serial: ${serial}`,
      `${ts(9)} - Asset Tag: ${serial}`,
      `${ts(10)} - System UUID: MANUAL-${cleanHost.toUpperCase()}-${Date.now().toString(36).toUpperCase()}`,
      `${ts(11)} - ----- CPU DETAIL -----`,
      `${ts(12)} - Name: ${cpuOnly}`,
      `${ts(13)} - Mfr: ${mfr}`,
      `${ts(14)} - Cores: 8`,
      `${ts(15)} - Threads: 16`,
      `${ts(16)} - Status: OK`,
      `${ts(17)} - ----- DISK DRIVES -----`,
      `${ts(18)} - Model: NVMe SSD ${diskGB}GB`,
      `${ts(19)} - SizeBytes: ${diskBytes}`,
      `${ts(20)} - Interface: NVMe`,
      `${ts(21)} - Serial: ${serial}-DISK0`,
      `${ts(22)} - Status: OK`,
      `${ts(23)} - ----- BIOS -----`,
      `${ts(24)} - Mfr: ${mfr}`,
      `${ts(25)} - Version: 1.14.0`,
      `${ts(26)} - Status: OK`,
      `${ts(27)} - TPM Activated=True Enabled=True Spec=2.0`,
      `${ts(28)} - === Prep phase complete ===`,
    ];
    return lines.join('\n') + '\n';
  }

  let seededDemoDevices = false;
  async function ensureSeededRanDevices(list) {
    if (seededDemoDevices || (list && list.length >= 3)) return list;
    seededDemoDevices = true;
    const samples = [
      {
        host: 'KENTER-LP-204',
        device: 'Lenovo ThinkPad T14 Gen 3',
        config: 'Intel Core i7-1260P · 16 GB RAM · 512 GB SSD',
        asset: 'PF3KNT204',
        os: 'Microsoft Windows 11 Pro',
      },
      {
        host: 'DELA-WS-118',
        device: 'HP EliteBook 840 G9',
        config: 'Intel Core i5-1245U · 16 GB RAM · 512 GB SSD',
        asset: '5CG238DELA',
        os: 'Microsoft Windows 11 Pro',
      },
    ];
    for (const s of samples) {
      try {
        await fetch('/api/v1/prep-logs', {
          method: 'POST',
          headers: { 'Content-Type': 'text/plain; charset=utf-8', 'X-File-Name': s.host + '_prep.log' },
          body: synthesizePrepLog(s),
        });
      } catch {}
    }
    try {
      const d = await api('GET', '/api/v1/admin/prep-devices');
      return d.devices || list;
    } catch {
      return list;
    }
  }

  function open(opts = {}) {
    customers = window.SLA ? SLA.mergeCustomersWithDefaults(opts.customers || []) : (opts.customers || []).slice();
    selected = null;
    workflowProfiles = [];
    workflowProfilesRequest = Promise.resolve();
    selectedRanDevice = null;
    deviceMode = 'ran';
    excludeWeekends = window.SLA ? SLA.excludeWeekends() : true;
    const defDays = window.SLA ? SLA.getDefaults().defaultWorkdays : 2;
    activeWorkdays = defDays;

    const root = $('#modal-root');
    const nextNum = opts.nextNum || '';
    const initTarget = window.SLA
      ? SLA.addWorkdays(new Date(), defDays, { excludeWeekends }).date
      : (opts.defaultTarget || new Date(Date.now() + 24 * 3600 * 1000));

    root.innerHTML = `
      <div class="modal oc-modal" role="dialog" aria-modal="true" aria-labelledby="oc-title">
        <form id="oc-form" novalidate>
          <header class="oc-head">
            <div class="oc-head-main">
              <span class="oc-eyebrow">Orders · Intake &amp; SLA</span>
              <h2 id="oc-title">New order</h2>
              <p class="oc-sub">Pick a customer to auto-apply their workday SLA (weekends excluded), and link a Ran device or enter hardware manually.</p>
            </div>
            <button type="button" class="icon-btn oc-x" data-oc-cancel aria-label="Close">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
            </button>
          </header>

          <div class="oc-body">
            <section class="oc-sec">
              <div class="oc-sec-head">
                <span class="oc-sec-title">Order identification</span>
                <span class="oc-sec-meta">Required fields marked <em>*</em></span>
              </div>
              <div class="oc-grid2">
                <div class="field">
                  <label for="oc-num">Order number <em>*</em></label>
                  <input id="oc-num" name="orderNumber" required autocomplete="off" spellcheck="false"
                         class="mono" placeholder="e.g. ORD-10432" value="${esc(nextNum)}">
                  <span class="field-hint">${nextNum ? `Suggested next number (${esc(nextNum)}) — editable.` : 'Unique order reference.'}</span>
                </div>
                <div class="field">
                  <label for="oc-debit">${esc(t('info.debit'))}</label>
                  <input id="oc-debit" name="debitNumber" autocomplete="off" spellcheck="false"
                         class="mono" placeholder="DEB-0000">
                  <span class="field-hint">Auto-filled when the customer has a directory number.</span>
                </div>
              </div>
            </section>

            <section class="oc-sec">
              <div class="oc-sec-head">
                <span class="oc-sec-title">${esc(t('orders.customer'))} <em>*</em></span>
                <span class="oc-sec-meta" id="oc-cust-meta">${customers.length ? `${customers.length} known · Kenter (2d) &amp; Dela (5d) pre-configured` : 'Type a name to create your first customer'}</span>
              </div>

              <div class="oc-cust" id="oc-cust-picker">
                <div class="oc-sel hidden" id="oc-cust-sel">
                  <span class="oc-av" id="oc-cust-av"></span>
                  <div class="oc-sel-txt">
                    <div class="oc-sel-name" id="oc-cust-name"></div>
                    <div class="oc-sel-tags" id="oc-cust-tags"></div>
                  </div>
                  <button type="button" class="btn btn-ghost btn-sm" id="oc-cust-change">Change</button>
                </div>

                <div class="oc-search-wrap" id="oc-cust-search">
                  <div class="oc-search">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>
                    <input id="oc-cust-q" type="text" autocomplete="off" spellcheck="false"
                           placeholder="Search customer (e.g. Kenter, Dela) or type a new name…">
                    <button type="button" class="oc-clear hidden" id="oc-cust-clear" aria-label="Clear">&times;</button>
                  </div>
                  <div class="oc-menu" id="oc-cust-menu" role="listbox"></div>
                </div>
              </div>
              <div class="oc-workflow hidden" id="oc-workflow-box">
                <label for="oc-workflow-profile">Workflow for this order</label>
                <select id="oc-workflow-profile" name="workflowProfileId"></select>
                <small id="oc-workflow-hint">Choose which guided steps this order will follow.</small>
              </div>
            </section>

            <section class="oc-sec">
              <div class="oc-sec-head">
                <span class="oc-sec-title">Target completion &amp; Customer SLA</span>
                <span class="oc-sla-pill" id="oc-sla-pill"><span class="dot"></span><span id="oc-sla-rel"></span></span>
              </div>

              <div class="oc-sla-policy" id="oc-sla-policy">
                <div class="oc-sla-policy-main">
                  <span class="oc-sla-badge" id="oc-sla-cust-badge">Default SLA: ${defDays} workdays</span>
                  <span class="oc-sla-skip-note" id="oc-sla-skip-note">Weekends (Sat &amp; Sun) are not counted</span>
                </div>
                <div class="oc-sla-policy-controls">
                  <label class="oc-sla-days-lbl" for="oc-cust-sla-days">
                    <span id="oc-cust-sla-for">Workdays:</span>
                    <input type="number" id="oc-cust-sla-days" class="oc-sla-days-inp mono" min="1" max="60" step="1" value="${defDays}">
                    <span>d</span>
                  </label>
                  <button type="button" class="btn btn-ghost btn-sm hidden" id="oc-save-cust-sla" title="Save this workday SLA for the selected customer">Save for customer</button>
                  <label class="oc-wk-toggle" title="Saturday and Sunday are skipped when calculating SLA target and countdown">
                    <input type="checkbox" id="oc-skip-weekends" ${excludeWeekends ? 'checked' : ''}>
                    <span>Skip weekends</span>
                  </label>
                </div>
              </div>

              <div class="oc-target">
                <div class="oc-chips" id="oc-chips">
                  <button type="button" data-preset="today">Today 17:00</button>
                  <button type="button" data-workdays="1">1 workday</button>
                  <button type="button" data-workdays="2" class="${defDays === 2 ? 'on' : ''}">2 workdays <small>Kenter</small></button>
                  <button type="button" data-workdays="3">3 workdays</button>
                  <button type="button" data-workdays="5" class="${defDays === 5 ? 'on' : ''}">5 workdays <small>Dela</small></button>
                  <button type="button" data-workdays="10">10 workdays</button>
                </div>
                <input id="oc-target" name="target" type="datetime-local" value="${toLocalInput(initTarget)}">
              </div>
            </section>

            <section class="oc-sec">
              <div class="oc-sec-head">
                <span class="oc-sec-title">Device &amp; hardware</span>
                <div class="oc-mode-tabs" role="tablist" aria-label="Device entry mode">
                  <button type="button" role="tab" class="oc-mode-tab active" data-dev-mode="ran" id="oc-tab-ran">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M2 20h20"/></svg>
                    <span>Select Ran device</span>
                    <span class="oc-mode-cnt" id="oc-ran-cnt">…</span>
                  </button>
                  <button type="button" role="tab" class="oc-mode-tab" data-dev-mode="manual" id="oc-tab-manual">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14"/></svg>
                    <span>Manual / Add new</span>
                  </button>
                </div>
              </div>

              <div class="oc-ran-pane" id="oc-ran-pane">
                <div class="oc-sel hidden" id="oc-ran-sel">
                  <span class="oc-av oc-ran-av" id="oc-ran-av">PC</span>
                  <div class="oc-sel-txt">
                    <div class="oc-sel-name" id="oc-ran-name"></div>
                    <div class="oc-sel-tags" id="oc-ran-tags"></div>
                  </div>
                  <button type="button" class="btn btn-ghost btn-sm" id="oc-ran-change">Change device</button>
                </div>

                <div class="oc-ran-picker" id="oc-ran-picker">
                  <div class="oc-search">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>
                    <input id="oc-ran-q" type="text" autocomplete="off" spellcheck="false"
                           placeholder="Search Ran devices by hostname, model, CPU, RAM or serial…">
                    <button type="button" class="oc-clear hidden" id="oc-ran-clear" aria-label="Clear">&times;</button>
                  </div>
                  <div class="oc-menu oc-ran-menu" id="oc-ran-menu" role="listbox">
                    <div class="oc-empty">Loading Ran devices…</div>
                  </div>
                </div>
              </div>

              <div class="oc-manual-pane hidden" id="oc-manual-pane">
                <div class="oc-manual-presets">
                  <span class="oc-preset-lbl">Quick models:</span>
                  <button type="button" class="oc-preset-chip" data-fill-dev="Lenovo ThinkPad T14 Gen 3" data-fill-cfg="Intel Core i7 · 16 GB RAM · 512 GB SSD · Windows 11 Pro">ThinkPad T14 G3</button>
                  <button type="button" class="oc-preset-chip" data-fill-dev="HP EliteBook 840 G9" data-fill-cfg="Intel Core i5 · 16 GB RAM · 512 GB SSD · Windows 11 Pro">EliteBook 840 G9</button>
                  <button type="button" class="oc-preset-chip" data-fill-dev="Dell Latitude 5540" data-fill-cfg="Intel Core i7 · 32 GB RAM · 512 GB SSD · Windows 11 Pro">Latitude 5540</button>
                  <button type="button" class="oc-preset-chip" data-fill-dev="Lenovo ThinkCentre M90q" data-fill-cfg="Intel Core i5 · 16 GB RAM · 256 GB SSD · Windows 11 Pro">ThinkCentre M90q</button>
                </div>
                <div class="oc-manual-reg">
                  <label class="oc-wk-toggle">
                    <input type="checkbox" id="oc-reg-ran" checked>
                    <span>Also register this manual device in <strong>Ran Devices</strong> on create</span>
                  </label>
                  <input id="oc-ran-host" class="oc-host-inp mono" placeholder="Hostname (e.g. ESP-LP-305, optional)" autocomplete="off" spellcheck="false">
                  <button type="button" class="btn btn-secondary btn-sm" id="oc-reg-ran-now">+ Add to Ran Devices now</button>
                </div>
              </div>

              <div class="oc-grid2 oc-hw-fields">
                <div class="field">
                  <label for="oc-device">${esc(t('info.device'))}</label>
                  <input id="oc-device" name="device" autocomplete="off" placeholder="e.g. Lenovo ThinkPad T14 Gen 3">
                </div>
                <div class="field">
                  <label for="oc-config">${esc(t('info.config'))}</label>
                  <input id="oc-config" name="configuration" autocomplete="off" placeholder="e.g. i7 · 16 GB RAM · 512 GB SSD">
                </div>
                <div class="field">
                  <label for="oc-asset">${esc(t('info.asset'))}</label>
                  <input id="oc-asset" name="assetNumber" autocomplete="off" spellcheck="false" class="mono" placeholder="ASSET-00000 / Serial">
                </div>
                <div class="field">
                  <label for="oc-omni">${esc(t('info.omni'))}</label>
                  <input id="oc-omni" name="omnitrackerTicket" autocomplete="off" spellcheck="false" class="mono" placeholder="OT-00000">
                </div>
              </div>
            </section>

            <p class="form-error hidden" id="oc-error"></p>
          </div>

          <footer class="oc-foot">
            <div class="oc-sum" id="oc-sum"></div>
            <div class="oc-foot-btns">
              <button type="button" class="btn btn-secondary" data-oc-cancel>Cancel</button>
              <button type="submit" class="btn btn-primary" id="oc-submit">Create order</button>
            </div>
          </footer>
        </form>
      </div>`;

    root.classList.remove('hidden');
    bind(root);
    paintMenu('');
    updateSlaPreview();
    updateSummary();
    loadRanDevices(root);

    const numInput = $('#oc-num', root);
    if (numInput && !numInput.value) numInput.focus();
    else $('#oc-cust-q', root).focus();
  }

  async function loadRanDevices(root) {
    try {
      const d = await api('GET', '/api/v1/admin/prep-devices');
      ranDevices = await ensureSeededRanDevices(d.devices || []);
    } catch {
      ranDevices = [];
    }
    const cnt = $('#oc-ran-cnt', root);
    if (cnt) cnt.textContent = String(ranDevices.length);
    paintRanMenu($('#oc-ran-q', root) ? $('#oc-ran-q', root).value : '');
  }

  function setDeviceMode(mode) {
    deviceMode = mode === 'manual' ? 'manual' : 'ran';
    const root = $('#modal-root');
    if (!root) return;
    $$('[data-dev-mode]', root).forEach((b) => {
      b.classList.toggle('active', b.dataset.devMode === deviceMode);
    });
    const ranPane = $('#oc-ran-pane', root);
    const manPane = $('#oc-manual-pane', root);
    if (ranPane) ranPane.classList.toggle('hidden', deviceMode !== 'ran');
    if (manPane) manPane.classList.toggle('hidden', deviceMode !== 'manual');
    updateSummary();
  }

  async function chooseRanDevice(id) {
    const d = ranDevices.find((x) => Number(x.id) === Number(id));
    if (!d) return;
    selectedRanDevice = d;
    const root = $('#modal-root');
    if (!root) return;

    const title = formatDeviceTitle(d);
    const cfg = formatDeviceConfig(d);
    let asset = formatDeviceAsset(d, null);

    const devInp = $('#oc-device', root);
    const cfgInp = $('#oc-config', root);
    const astInp = $('#oc-asset', root);
    if (devInp) devInp.value = title;
    if (cfgInp && cfg) cfgInp.value = cfg;
    if (astInp && asset) astInp.value = asset;

    const selBox = $('#oc-ran-sel', root);
    const picker = $('#oc-ran-picker', root);
    const av = $('#oc-ran-av', root);
    if (av) {
      av.style.setProperty('--oc-h', avatarHue(d.computerName || title));
      av.textContent = avatarInitials(d.manufacturer || d.computerName || 'PC');
    }
    const nm = $('#oc-ran-name', root);
    if (nm) nm.textContent = `${title} · ${d.computerName || '#' + d.id}`;
    const tags = $('#oc-ran-tags', root);
    if (tags) {
      const tList = [];
      if (d.computerName) tList.push(`<span class="chip mono">${esc(d.computerName)}</span>`);
      if (cfg) tList.push(`<span class="chip">${esc(cfg)}</span>`);
      if (d.complete) tList.push('<span class="chip chip-ok">Prep complete</span>');
      tags.innerHTML = tList.join('');
    }
    if (selBox) selBox.classList.remove('hidden');
    if (picker) picker.classList.add('hidden');
    updateSummary();

                                                                               
    try {
      const full = await api('GET', '/api/v1/admin/prep-devices/' + encodeURIComponent(d.id));
      if (selectedRanDevice && Number(selectedRanDevice.id) === Number(d.id) && full && full.doc) {
        const richAsset = formatDeviceAsset(d, full.doc);
        if (richAsset && astInp) astInp.value = richAsset;
        updateSummary();
      }
    } catch {}
  }

  function clearRanDevice() {
    selectedRanDevice = null;
    const root = $('#modal-root');
    if (!root) return;
    const selBox = $('#oc-ran-sel', root);
    const picker = $('#oc-ran-picker', root);
    if (selBox) selBox.classList.add('hidden');
    if (picker) picker.classList.remove('hidden');
    paintRanMenu($('#oc-ran-q', root) ? $('#oc-ran-q', root).value : '');
    updateSummary();
  }

  function paintRanMenu(raw) {
    const menu = $('#oc-ran-menu');
    if (!menu) return;
    const q = String(raw || '').trim().toLowerCase();
    const clearBtn = $('#oc-ran-clear');
    if (clearBtn) clearBtn.classList.toggle('hidden', !q);

    if (!ranDevices.length) {
      menu.innerHTML = `<div class="oc-empty">No Ran devices found yet — switch to <strong>Manual / Add new</strong> to enter &amp; register one.</div>`;
      return;
    }

    const matches = q
      ? ranDevices.filter((d) => {
          const blob = [d.computerName, d.manufacturer, d.model, d.cpuName, d.ramText, d.os, d.deviceKey, d.fileName]
            .filter(Boolean).join(' ').toLowerCase();
          return blob.includes(q);
        })
      : ranDevices;

    if (!matches.length) {
      menu.innerHTML = `<div class="oc-empty">No Ran device matches &ldquo;${esc(raw)}&rdquo; — use <strong>Manual / Add new</strong> to add it.</div>`;
      return;
    }

    menu.innerHTML = matches.slice(0, 30).map((d) => {
      const title = formatDeviceTitle(d);
      const cfg = formatDeviceConfig(d);
      const host = d.computerName || ('#' + d.id);
      return `
        <button type="button" class="oc-row oc-ran-row" role="option" data-pick-ran="${d.id}">
          <span class="oc-av" style="--oc-h:${avatarHue(host)}">${esc(avatarInitials(d.manufacturer || host))}</span>
          <span class="oc-row-main">
            <span class="oc-row-name">${esc(title)} <span class="mono muted">· ${esc(host)}</span></span>
            <span class="oc-row-sub">${esc(cfg || d.fileName || 'Hardware prep log')}</span>
          </span>
          <span class="oc-row-tags">
            ${d.complete ? '<span class="chip chip-ok">Prep OK</span>' : '<span class="chip">Partial</span>'}
            ${d.checksOK ? `<span class="chip mono">${d.checksOK}✓</span>` : ''}
          </span>
        </button>`;
    }).join('');
  }

  function paintMenu(raw) {
    const menu = $('#oc-cust-menu');
    if (!menu) return;
    const q = String(raw || '').trim();
    const ql = q.toLowerCase();
    const clearBtn = $('#oc-cust-clear');
    if (clearBtn) clearBtn.classList.toggle('hidden', !q);

    const matches = ql
      ? customers.filter((c) => c.name.toLowerCase().includes(ql) || String(c.number || '').toLowerCase().includes(ql))
      : customers;
    const exact = customers.some((c) => c.name.toLowerCase() === ql);

    let html = '';
    if (q && !exact) {
      const defDays = window.SLA ? SLA.effectiveDays(q) : 2;
      html += `
        <button type="button" class="oc-row oc-row-new" data-create="${esc(q)}">
          <span class="oc-av oc-av-new">+</span>
          <span class="oc-row-main">
            <span class="oc-row-name">Create new customer &ldquo;<strong>${esc(q)}</strong>&rdquo;</span>
            <span class="oc-row-sub">Added to the customer directory · default ${defDays} workday SLA (configurable below)</span>
          </span>
          <span class="chip chip-ok">new</span>
        </button>`;
    }

    if (matches.length) {
      html += matches.slice(0, 40).map((c) => {
        const slaDays = window.SLA ? SLA.getCustomerDays(c.name) : null;
        return `
        <button type="button" class="oc-row" role="option" data-pick="${esc(c.name)}">
          <span class="oc-av" style="--oc-h:${avatarHue(c.name)}">${esc(avatarInitials(c.name))}</span>
          <span class="oc-row-main">
            <span class="oc-row-name">${ highlight(c.name, q) }</span>
            <span class="oc-row-sub">${c.managed ? 'In directory' : 'From orders'}${c.hasFlow ? ' · custom work flow' : ''}${slaDays ? ` · SLA: ${slaDays} workday${slaDays === 1 ? '' : 's'} (no weekends)` : ''}</span>
          </span>
          <span class="oc-row-tags">
            ${slaDays ? `<span class="chip chip-ok">${slaDays}d SLA</span>` : ''}
            ${c.number ? `<span class="chip mono">#${esc(c.number)}</span>` : ''}
            ${c.hasFlow ? '<span class="chip">flow</span>' : ''}
          </span>
        </button>`;
      }).join('');
    } else if (!q) {
      html += `<div class="oc-empty">No customers yet — type a name above to create the first one.</div>`;
    }

    menu.innerHTML = html;
  }

  function highlight(text, q) {
    if (!q) return esc(text);
    const i = text.toLowerCase().indexOf(q.toLowerCase());
    if (i < 0) return esc(text);
    return esc(text.slice(0, i)) + '<mark>' + esc(text.slice(i, i + q.length)) + '</mark>' + esc(text.slice(i + q.length));
  }

  function applyWorkdaysTarget(days) {
    const wd = Math.max(1, Math.min(60, Math.round(Number(days) || 2)));
    activeWorkdays = wd;
    const root = $('#modal-root');
    const inp = $('#oc-target', root);
    const daysInp = $('#oc-cust-sla-days', root);
    if (daysInp) daysInp.value = String(wd);

    const res = window.SLA
      ? SLA.addWorkdays(new Date(), wd, { excludeWeekends })
      : { date: new Date(Date.now() + wd * 86400 * 1000), skippedWeekendDays: 0 };

    if (inp) inp.value = toLocalInput(res.date);

    $$('#oc-chips button', root).forEach((b) => {
      b.classList.toggle('on', Number(b.dataset.workdays) === wd);
    });
    updateSlaPreview();
    updateSummary();
  }

  function chooseCustomer(name, isNew) {
    const known = customers.find((c) => c.name.toLowerCase() === name.toLowerCase());
    const custName = known ? known.name : name.trim();
    const slaRule = window.SLA ? SLA.getCustomerRule(custName) : null;
    const custDays = slaRule ? slaRule.workdays : (window.SLA ? SLA.effectiveDays(custName) : 2);

    selected = known
      ? { name: known.name, number: known.number || '', isNew: false, managed: !!known.managed, hasFlow: !!known.hasFlow, slaDays: slaRule ? slaRule.workdays : null }
      : { name: custName, number: '', isNew: !!isNew, managed: false, hasFlow: false, slaDays: slaRule ? slaRule.workdays : null };

    const sel = $('#oc-cust-sel');
    const search = $('#oc-cust-search');
    const av = $('#oc-cust-av');
    av.style.setProperty('--oc-h', avatarHue(selected.name));
    av.textContent = avatarInitials(selected.name);
    $('#oc-cust-name').textContent = selected.name;

    const tags = [];
    if (selected.isNew) tags.push('<span class="chip chip-ok">new customer · will be added</span>');
    else if (selected.managed) tags.push('<span class="chip">directory</span>');
    if (selected.number) tags.push(`<span class="chip mono">#${esc(selected.number)}</span>`);
    if (selected.hasFlow) tags.push('<span class="chip">custom work flow</span>');
    tags.push(`<span class="chip chip-ok" id="oc-cust-sla-tag">SLA: ${custDays} workday${custDays === 1 ? '' : 's'} (no weekends)</span>`);
    $('#oc-cust-tags').innerHTML = tags.join('');

    sel.classList.remove('hidden');
    search.classList.add('hidden');

    const debit = $('#oc-debit');
    if (debit && selected.number && (!debit.value.trim() || debit.dataset.autoDebit === '1')) {
      debit.value = selected.number;
      debit.dataset.autoDebit = '1';
    }

    const saveBtn = $('#oc-save-cust-sla');
    if (saveBtn) saveBtn.classList.remove('hidden');
    const lblFor = $('#oc-cust-sla-for');
    if (lblFor) lblFor.textContent = `${selected.name} SLA:`;

                                                                          
    applyWorkdaysTarget(custDays);
    workflowProfilesRequest = loadOrderWorkflowProfiles(selected.name);
  }

  async function loadOrderWorkflowProfiles(customerName) {
    const box = $('#oc-workflow-box');
    const select = $('#oc-workflow-profile');
    if (!box || !select) return;
    box.classList.add('hidden');
    select.innerHTML = '';
    workflowProfiles = [];
    try {
      const d = await api('GET', '/api/v1/workflow-profiles?customer=' + encodeURIComponent(customerName));
      if (!selected || selected.name !== customerName) return;
      workflowProfiles = d.profiles || [];
      if (!workflowProfiles.length) return;
      const chooseOption = workflowProfiles.length > 1 ? '<option value="">Select a workflow profile…</option>' : '';
      select.innerHTML = chooseOption + workflowProfiles.map((p) => `<option value="${p.id}">${esc(p.name)}</option>`).join('');
      if (workflowProfiles.length > 1) select.value = '';
      box.classList.remove('hidden');
    } catch {
      workflowProfiles = [];
    }
  }

  function clearCustomer() {
    selected = null;
    workflowProfiles = [];
    workflowProfilesRequest = Promise.resolve();
    const workflowBox = $('#oc-workflow-box');
    if (workflowBox) workflowBox.classList.add('hidden');
    $('#oc-cust-sel').classList.add('hidden');
    $('#oc-cust-search').classList.remove('hidden');
    const saveBtn = $('#oc-save-cust-sla');
    if (saveBtn) saveBtn.classList.add('hidden');
    const lblFor = $('#oc-cust-sla-for');
    if (lblFor) lblFor.textContent = 'Workdays:';
    const q = $('#oc-cust-q');
    q.value = '';
    paintMenu('');
    q.focus();
    updateSlaPreview();
    updateSummary();
  }

  function updateSlaPreview() {
    const val = $('#oc-target') && $('#oc-target').value;
    const pill = $('#oc-sla-pill');
    const rel = $('#oc-sla-rel');
    const badgeEl = $('#oc-sla-cust-badge');
    const skipEl = $('#oc-sla-skip-note');
    if (!pill || !rel) return;
    if (!val) {
      pill.className = 'oc-sla-pill';
      rel.textContent = 'No target';
      return;
    }
    const dtTarget = new Date(val);
    if (Number.isNaN(dtTarget.getTime())) {
      pill.className = 'oc-sla-pill';
      rel.textContent = 'Invalid target';
      return;
    }
    const iso = dtTarget.toISOString();
    const st = slaOf({ targetCompletionAt: iso, customerName: selected ? selected.name : '' });
    pill.className = 'oc-sla-pill sla-' + st;

    const between = window.SLA ? SLA.countBetween(new Date(), new Date(val)) : { workdays: activeWorkdays || 1, weekendDays: 0 };
    const wkTxt = between.workdays >= 0 ? `${between.workdays} workday${between.workdays === 1 ? '' : 's'}` : 'past due';
    rel.textContent = `${st.replace('_', ' ')} · ${wkTxt} (${relTime(iso)})`;

    if (badgeEl) {
      if (selected) {
        const rule = window.SLA ? SLA.getCustomerRule(selected.name) : null;
        const d = activeWorkdays || (rule ? rule.workdays : 2);
        badgeEl.textContent = `${selected.name} SLA: ${d} workday${d === 1 ? '' : 's'}${rule ? '' : ' (default)'}`;
      } else {
        const d = activeWorkdays || (window.SLA ? SLA.getDefaults().defaultWorkdays : 2);
        badgeEl.textContent = `Target SLA: ${d} workday${d === 1 ? '' : 's'}`;
      }
    }
    if (skipEl) {
      if (!excludeWeekends) {
        skipEl.textContent = 'Counting all calendar days (weekends included)';
      } else if (between.weekendDays > 0) {
        skipEl.textContent = `Skips ${between.weekendDays} weekend day${between.weekendDays === 1 ? '' : 's'} (Sat/Sun excluded)`;
      } else {
        skipEl.textContent = 'Weekends (Sat & Sun) do not count toward SLA';
      }
    }
  }

  function updateSummary() {
    const sum = $('#oc-sum');
    const btn = $('#oc-submit');
    if (!sum || !btn) return;
    const num = ($('#oc-num') && $('#oc-num').value.trim()) || '';
    const ok = !!(num && selected && selected.name);
    btn.disabled = !ok;
    if (!ok) {
      const missing = [];
      if (!num) missing.push('order number');
      if (!selected) missing.push('customer');
      sum.innerHTML = `<span class="muted">Enter ${missing.join(' &amp; ')} to continue.</span>`;
      return;
    }
    const devVal = ($('#oc-device') && $('#oc-device').value.trim()) || '';
    const devSummary = selectedRanDevice
      ? ` · Ran device <strong>${esc(selectedRanDevice.computerName || devVal)}</strong>`
      : (devVal ? ` · <strong>${esc(devVal)}</strong>` : '');
    const wd = activeWorkdays || (window.SLA ? SLA.effectiveDays(selected.name) : 2);
    sum.innerHTML = `Ready: <strong class="mono">${esc(num)}</strong> for <strong>${esc(selected.name)}</strong> (${wd}d SLA, no weekends)${devSummary}`;
  }

  async function registerManualDeviceNow(silent) {
    const root = $('#modal-root');
    if (!root) return null;
    const device = ($('#oc-device', root) && $('#oc-device', root).value.trim()) || '';
    const config = ($('#oc-config', root) && $('#oc-config', root).value.trim()) || '';
    const asset = ($('#oc-asset', root) && $('#oc-asset', root).value.trim()) || '';
    const hostInp = ($('#oc-ran-host', root) && $('#oc-ran-host', root).value.trim()) || '';
    if (!device && !hostInp && !asset) {
      if (!silent) toast('Enter at least a device model, hostname, or serial first.', true);
      return null;
    }
    const host = hostInp || asset || ('ESP-' + device.replace(/[^A-Za-z0-9]+/g, '-').toUpperCase().slice(0, 12) + '-' + Math.floor(100 + Math.random() * 899));
    const logText = synthesizePrepLog({ host, device: device || host, config, asset: asset || host });
    try {
      const res = await fetch('/api/v1/prep-logs', {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain; charset=utf-8', 'X-File-Name': host + '_prep.log' },
        body: logText,
      });
      const data = await res.json();
      if (!res.ok) throw new Error((data && data.error && data.error.message) || ('HTTP ' + res.status));
      if (!silent) toast(`Device "${data.computerName || host}" added to Ran Devices`);
      await loadRanDevices(root);
      return data;
    } catch (err) {
      if (!silent) toast('Could not register device: ' + err.message, true);
      return null;
    }
  }

  function bind(root) {
    const onKey = (e) => {
      if (e.key === 'Escape') { e.stopPropagation(); close(); }
    };
    document.addEventListener('keydown', onKey, true);
    cleanup = () => document.removeEventListener('keydown', onKey, true);

    root.addEventListener('mousedown', (e) => { if (e.target === root) close(); });
    $$('[data-oc-cancel]', root).forEach((b) => b.addEventListener('click', close));

    const q = $('#oc-cust-q', root);
    q.addEventListener('input', () => paintMenu(q.value));
    q.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        const first = $('#oc-cust-menu .oc-row', root);
        if (first) { e.preventDefault(); first.click(); }
      }
    });
    $('#oc-cust-clear', root).addEventListener('click', () => { q.value = ''; paintMenu(''); q.focus(); });
    $('#oc-cust-change', root).addEventListener('click', clearCustomer);

    $('#oc-cust-menu', root).addEventListener('click', (e) => {
      const pick = e.target.closest('[data-pick]');
      if (pick) { chooseCustomer(pick.dataset.pick, false); return; }
      const cr = e.target.closest('[data-create]');
      if (cr) { chooseCustomer(cr.dataset.create, true); }
    });

    const workflowSelect = $('#oc-workflow-profile', root);
    if (workflowSelect) workflowSelect.addEventListener('change', updateSummary);

                                            
    const daysInp = $('#oc-cust-sla-days', root);
    if (daysInp) {
      daysInp.addEventListener('change', () => {
        const wd = Math.max(1, Math.min(60, Math.round(Number(daysInp.value) || 2)));
        applyWorkdaysTarget(wd);
        if (selected && window.SLA) {
          SLA.setCustomerDays(selected.name, wd, excludeWeekends);
          const tag = $('#oc-cust-sla-tag', root);
          if (tag) tag.textContent = `SLA: ${wd} workday${wd === 1 ? '' : 's'} (no weekends)`;
        }
      });
    }

    const saveCustSlaBtn = $('#oc-save-cust-sla', root);
    if (saveCustSlaBtn) {
      saveCustSlaBtn.addEventListener('click', () => {
        if (!selected || !window.SLA) return;
        const wd = Math.max(1, Math.min(60, Math.round(Number(daysInp.value) || 2)));
        SLA.setCustomerDays(selected.name, wd, excludeWeekends);
        customers = SLA.mergeCustomersWithDefaults(customers);
        const tag = $('#oc-cust-sla-tag', root);
        if (tag) tag.textContent = `SLA: ${wd} workday${wd === 1 ? '' : 's'} (no weekends)`;
        applyWorkdaysTarget(wd);
        toast(`Saved SLA for ${selected.name}: ${wd} workday${wd === 1 ? '' : 's'} (weekends excluded)`);
      });
    }

    const skipWkChk = $('#oc-skip-weekends', root);
    if (skipWkChk) {
      skipWkChk.addEventListener('change', () => {
        excludeWeekends = skipWkChk.checked;
        if (window.SLA) SLA.setDefaults({ excludeWeekends });
        applyWorkdaysTarget(activeWorkdays || 2);
      });
    }

    $('#oc-chips', root).addEventListener('click', (e) => {
      const b = e.target.closest('button');
      if (!b) return;
      $$('#oc-chips button', root).forEach((x) => x.classList.toggle('on', x === b));
      if (b.dataset.preset === 'today') {
        const d = new Date();
        d.setHours(17, 0, 0, 0);
        if (d.getTime() < Date.now()) d.setTime(Date.now() + 2 * 3600 * 1000);
        if (excludeWeekends && window.SLA && SLA.isWeekend(d)) {
          const moved = SLA.addWorkdays(d, 0, { excludeWeekends: true });
          $('#oc-target', root).value = toLocalInput(moved.date);
        } else {
          $('#oc-target', root).value = toLocalInput(d);
        }
        activeWorkdays = 0;
        updateSlaPreview();
        updateSummary();
      } else if (b.dataset.workdays) {
        applyWorkdaysTarget(Number(b.dataset.workdays));
      }
    });

    $('#oc-target', root).addEventListener('input', () => {
      $$('#oc-chips button', root).forEach((x) => x.classList.remove('on'));
      const problem = readTarget().problem;
      if (problem) showErr(problem);
      else {
        const errEl = $('#oc-error', root);
        if (errEl) { errEl.textContent = ''; errEl.classList.add('hidden'); }
      }
      updateSlaPreview();
    });

                                           
    $$('[data-dev-mode]', root).forEach((b) => {
      b.addEventListener('click', () => setDeviceMode(b.dataset.devMode));
    });

    const ranQ = $('#oc-ran-q', root);
    if (ranQ) {
      ranQ.addEventListener('input', () => paintRanMenu(ranQ.value));
    }
    const ranClear = $('#oc-ran-clear', root);
    if (ranClear) {
      ranClear.addEventListener('click', () => { ranQ.value = ''; paintRanMenu(''); ranQ.focus(); });
    }
    const ranChange = $('#oc-ran-change', root);
    if (ranChange) {
      ranChange.addEventListener('click', clearRanDevice);
    }
    const ranMenu = $('#oc-ran-menu', root);
    if (ranMenu) {
      ranMenu.addEventListener('click', (e) => {
        const row = e.target.closest('[data-pick-ran]');
        if (row) chooseRanDevice(Number(row.dataset.pickRan));
      });
    }

                                               
    $$('[data-fill-dev]', root).forEach((chip) => {
      chip.addEventListener('click', () => {
        const devInp = $('#oc-device', root);
        const cfgInp = $('#oc-config', root);
        if (devInp) devInp.value = chip.dataset.fillDev || '';
        if (cfgInp && chip.dataset.fillCfg) cfgInp.value = chip.dataset.fillCfg;
        updateSummary();
      });
    });

    const regNowBtn = $('#oc-reg-ran-now', root);
    if (regNowBtn) {
      regNowBtn.addEventListener('click', async () => {
        regNowBtn.disabled = true;
        const created = await registerManualDeviceNow(false);
        regNowBtn.disabled = false;
        if (created && created.id) {
          setDeviceMode('ran');
          chooseRanDevice(created.id);
        }
      });
    }

    ['#oc-num', '#oc-device', '#oc-config', '#oc-asset'].forEach((sel) => {
      const el = $(sel, root);
      if (el) el.addEventListener('input', updateSummary);
    });

    $('#oc-form', root).addEventListener('submit', submit);
  }

  function showErr(msg) {
    const el = $('#oc-error');
    if (!el) return;
    el.textContent = msg;
    el.classList.remove('hidden');
  }

  async function submit(e) {
    e.preventDefault();
    const root = $('#modal-root');
    const num = $('#oc-num', root).value.trim();
    if (!num) { showErr('Order number is required.'); $('#oc-num', root).focus(); return; }
    if (!selected || !selected.name) {
      const typed = ($('#oc-cust-q', root) && $('#oc-cust-q', root).value.trim()) || '';
      if (typed) chooseCustomer(typed, true);
      else { showErr('Please pick a customer or type a new name.'); $('#oc-cust-q', root).focus(); return; }
    }

    await workflowProfilesRequest;
    if (workflowProfiles.length > 1 && !$('#oc-workflow-profile', root).value) {
      showErr('Select a workflow profile for this order.');
      $('#oc-workflow-profile', root).focus();
      return;
    }

    const target = readTarget();
    if (target.problem) {
      showErr(target.problem);
      const inp = $('#oc-target', root);
      if (inp) inp.focus();
      return;
    }

    const btn = $('#oc-submit', root);
    btn.disabled = true;

    const debit = $('#oc-debit', root).value.trim();
    const daysInp = $('#oc-cust-sla-days', root);
    if (selected && window.SLA && daysInp && Number(daysInp.value) > 0) {
      SLA.setCustomerDays(selected.name, Number(daysInp.value), excludeWeekends);
    }

    if (selected.isNew) {
      try {
        await api('POST', '/api/v1/customers', { name: selected.name, number: debit });
      } catch (err) {
        if (!err || err.status !== 409) {                                                 }
      }
    }

                                                                                              
    const regChk = $('#oc-reg-ran', root);
    const devVal = $('#oc-device', root).value.trim();
    if (deviceMode === 'manual' && regChk && regChk.checked && devVal && !selectedRanDevice) {
      await registerManualDeviceNow(true);
    }

    const body = { orderNumber: num, customerName: selected.name };
    const workflowProfileId = Number($('#oc-workflow-profile', root)?.value || 0);
    if (workflowProfileId > 0) body.workflowProfileId = workflowProfileId;
    const fields = {
      debitNumber: debit,
      device: devVal,
      configuration: $('#oc-config', root).value.trim(),
      assetNumber: $('#oc-asset', root).value.trim(),
      omnitrackerTicket: $('#oc-omni', root).value.trim(),
    };
    Object.entries(fields).forEach(([k, v]) => { if (v) body[k] = v; });

    if (target.iso) body.targetCompletionAt = target.iso;

    try {
      const d = await api('POST', '/api/v1/orders', body);
      const created = d && d.order;
      const newNote = selected.isNew ? ` · customer “${selected.name}” added` : '';
      const slaNote = activeWorkdays ? ` · ${activeWorkdays}d SLA (no weekends)` : '';
      close();
      const pastNote = target.past ? ' · ⚠ registered past its target' : '';
      toast(t('toast.created') + newNote + slaNote + pastNote);
      if (typeof OrderCreate.onDone === 'function') OrderCreate.onDone(created);
      if (typeof loadOrders === 'function') await loadOrders(true);
    } catch (err) {
      btn.disabled = false;
      const existing = err && err.data && err.data.error && err.data.error.extra && err.data.error.extra.existingOrder;
      if (err && err.status === 409 && existing) {
        close();
        const openIt = await modal({
          title: 'Order number already in use',
          description: `${existing.orderNumber} already exists (status: ${statusLabel(existing.status)}, #${existing.id}).`,
          confirmLabel: 'Open existing order',
          cancelLabel: 'Close',
        });
        if (openIt && typeof goToOrder === 'function') goToOrder(existing.id);
        return;
      }
      showErr((err && err.message) || 'Could not create order');
    }
  }

  function close() {
    if (cleanup) { cleanup(); cleanup = null; }
    const root = $('#modal-root');
    if (root) {
      root.classList.add('hidden');
      root.innerHTML = '';
    }
  }

  window.OrderCreate = { open, close, submit, onDone: null };
})();
