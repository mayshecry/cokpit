'use strict';

  

  let clViewTimer = null;

  function startChecklistsPolling() {
    stopChecklistsPolling();
    clViewTimer = setInterval(async () => {
      if (!state.token || !location.hash.startsWith('#/checklists')) return stopChecklistsPolling();
      await loadChecklistsView(true);
    }, 5000);
  }
  function stopChecklistsPolling() {
    if (clViewTimer) { clearInterval(clViewTimer); clViewTimer = null; }
  }

  async function loadChecklistsView(silent) {
    try {
      const d = await api('GET', '/api/v1/checklists');
      state.checklists = d.checklists || [];
      if (state.checklistSelId && !state.checklists.some((c) => c.orderId === state.checklistSelId)) {
        state.checklistSelId = null;
      }
      if (!state.checklistSelId && state.checklists.length) {
        state.checklistSelId = state.checklists[0].orderId;
      }
      renderChecklistsList();
      renderChecklistPanel();
      
      
      if (!silent) focusPickScan();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  function renderChecklistsList() {
    const tbody = $('#checklists-table tbody');
    if (!tbody) return;
    
    const empty = !state.checklists.length;
    const split = $('#checklists-split');
    if (split) split.classList.toggle('hidden', empty);
    const hero = $('#checklists-empty');
    if (hero) hero.classList.toggle('hidden', !empty);
    const heroBtn = $('#cl-hero-demo');
    if (heroBtn) heroBtn.classList.toggle('hidden', !can('orders:create'));
    const heroNote = $('#cl-hero-note');
    if (heroNote) heroNote.classList.toggle('hidden', can('orders:create'));
    tbody.innerHTML = state.checklists.map((cl) => {
          const pct = cl.total ? Math.round((cl.done / cl.total) * 100) : 0;
          return `
            <tr class="row${state.checklistSelId === cl.orderId ? ' selected' : ''}" data-cl="${cl.orderId}" tabindex="0">
              <td class="num">${cl.orderId}</td>
              <td>${esc(cl.orderNumber)}</td>
              <td>${badge('status-' + esc(statusClass(cl.status)), statusLabel(cl.status))}</td>
              <td><div class="cl-bar${pct === 100 ? ' is-done' : ''}"><span style="width:${pct}%"></span></div></td>
              <td class="num">${cl.done}/${cl.total}</td>
            </tr>`;
      }).join('');
    const count = $('#checklist-row-count');
    if (count) {
      count.textContent = state.checklists.length
        ? `${state.checklists.length} pick list${state.checklists.length === 1 ? '' : 's'}`
        : '';
    }
  }

  function renderChecklistPanel() {
    const panel = $('#checklist-panel');
    const body = $('#checklist-panel-body');
    if (!panel || !body) return;
    const sel = state.checklists.find((c) => c.orderId === state.checklistSelId);
    if (!sel) {
      body.innerHTML = '';
      panel.classList.add('hidden');
      const split = $('#checklists-split');
      if (split) split.classList.add('no-detail');
      return;
    }
    
    const active = document.activeElement;
    const focusBtn = body.contains(active) && active.dataset && active.dataset.act
      ? `button[data-act="${active.dataset.act}"][data-id="${active.dataset.id}"]`
      : null;
    const pct = sel.total ? Math.round((sel.done / sel.total) * 100) : 0;
    body.innerHTML = `
      <div class="detail-head">
        <div class="detail-head-text">
          <div class="eyebrow">Order #${sel.orderId}</div>
          <h2>${esc(sel.orderNumber)}</h2>
          <div class="detail-badges">
            ${badge('status-' + esc(statusClass(sel.status)), statusLabel(sel.status))}
          </div>
        </div>
      </div>
      <div class="cl-panel-progress">
        <div class="cl-bar${pct === 100 ? ' is-done' : ''}"><span style="width:${pct}%"></span></div>
        <span class="muted">${sel.done}/${sel.total} · ${pct}%</span>
        <button type="button" class="btn btn-secondary btn-sm" data-act="cl-open-order">Open order</button>
      </div>
      <div class="detail-scroll">
        <section class="detail-section">
          <h3>Pick lines ${sel.total ? `<span class="count">${sel.done}/${sel.total}</span>` : ''}</h3>
          ${checklistItemsHtml(sel.items || [], 'No pick lines.')}
        </section>
      </div>`;
    if (focusBtn) {
      const el = body.querySelector(focusBtn);
      if (el) el.focus();
    }
    panel.classList.remove('hidden');
    const split = $('#checklists-split');
    if (split) split.classList.remove('no-detail');
  }

  

  
  
  
  const normCode = (s) => String(s || '').trim().replace(/\s+/g, '').toUpperCase();

  function focusPickScan() {
    const input = $('#pick-scan-input');
    const panel = $('#checklist-panel');
    if (input && panel && !panel.classList.contains('hidden')
        && location.hash.startsWith('#/checklists')) {
      input.focus({ preventScroll: true });
    }
  }

  function setPickScanFeedback(msg, cls) {
    const el = $('#pick-scan-feedback');
    if (!el) return;
    el.textContent = msg;
    el.className = 'scan-feedback' + (cls ? ' ' + cls : '');
  }

  function flashChecklistItem(id, cls) {
    const el = document.querySelector(`#checklist-panel-body .checklist-item[data-item="${id}"]`);
    if (!el) return;
    el.classList.remove('scan-flash', 'loc-flash');
    void el.offsetWidth; 
    el.classList.add(cls);
    el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    setTimeout(() => el.classList.remove(cls), 1700);
  }

  
  async function scanTickItem(it, label) {
    if (!can('pick:use')) {
      setPickScanFeedback('Your role cannot tick off lines (needs pick:use).', 'bad');
      return false;
    }
    const ok = await doChecklistToggle({ dataset: { id: String(it.id), action: 'tick' } });
    if (!ok) return false;
    await loadChecklistsView(true);
    flashChecklistItem(it.id, 'scan-flash');
    setPickScanFeedback(`✓ ${label}`, 'ok');
    return true;
  }

  function switchPickList(orderId) {
    state.checklistSelId = orderId;
    renderChecklistsList();
    renderChecklistPanel();
  }

  async function doPickScan(raw) {
    const code = normCode(raw);
    if (!code) return;
    const lists = state.checklists || [];
    if (!lists.length) {
      setPickScanFeedback('No pick lists yet — click “Load test data” or upload under Configuration first.', 'bad');
      return;
    }
    setPickScanFeedback('Looking up…');
    const sel = lists.find((c) => c.orderId === state.checklistSelId);

    
    
    if (sel) {
      const artHits = (sel.items || []).filter((it) => normCode(it.artikel) === code);
      const openHit = artHits.find((it) => !it.checkedBy);
      if (openHit) {
        await scanTickItem(openHit, `Ticked ${openHit.artikel}${openHit.aantal > 0 ? ' ×' + openHit.aantal : ''} (${sel.orderNumber})`);
        return;
      }
      if (artHits.length) {
        setPickScanFeedback(`Already ticked by ${artHits[0].checkedBy}${artHits[0].checkedAt ? ' · ' + fmtTime(artHits[0].checkedAt) : ''}`, 'bad');
        return;
      }
      
      const locHits = (sel.items || []).filter((it) => normCode(it.locatie) === code);
      if (locHits.length) {
        locHits.forEach((it) => flashChecklistItem(it.id, 'loc-flash'));
        setPickScanFeedback(`Location ${locHits[0].locatie} — ${locHits.length} line${locHits.length === 1 ? '' : 's'}`, 'ok');
        return;
      }
    }

    
    const orderHit = lists.find((c) => normCode(c.orderNumber) === code);
    if (orderHit) {
      switchPickList(orderHit.orderId);
      setPickScanFeedback(`Switched to ${orderHit.orderNumber} — ${orderHit.done}/${orderHit.total} picked`, 'ok');
      return;
    }

    
    for (const cl of lists) {
      if (sel && cl.orderId === sel.orderId) continue;
      const openHit = (cl.items || []).find((it) => !it.checkedBy && normCode(it.artikel) === code);
      if (openHit) {
        switchPickList(cl.orderId);
        await scanTickItem(openHit, `Ticked ${openHit.artikel} on ${cl.orderNumber}`);
        return;
      }
    }

    
    for (const cl of lists) {
      if (sel && cl.orderId === sel.orderId) continue;
      const locItem = (cl.items || []).find((it) => normCode(it.locatie) === code);
      if (locItem) {
        switchPickList(cl.orderId);
        (cl.items || []).filter((it) => normCode(it.locatie) === code)
          .forEach((it) => flashChecklistItem(it.id, 'loc-flash'));
        setPickScanFeedback(`Location ${locItem.locatie} on ${cl.orderNumber}`, 'ok');
        return;
      }
    }

    
    for (const cl of lists) {
      const doneHit = (cl.items || []).find((it) => it.checkedBy && normCode(it.artikel) === code);
      if (doneHit) {
        setPickScanFeedback(`All ${doneHit.artikel} lines already ticked on ${cl.orderNumber}`, 'bad');
        return;
      }
    }

    setPickScanFeedback(`“${String(raw).trim()}” not found in any pick list`, 'bad');
  }

