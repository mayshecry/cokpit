'use strict';

  

  async function loadOrderManuals(id, silent) {
    if (!id) return;
    try {
      const d = await api('GET', `/api/v1/orders/${id}/manuals`);
      if (state.selectedId !== id) return;
      state.orderManuals = d.manuals || [];
      
      const sec = $('#manuals-section');
      if (sec) sec.outerHTML = manualSectionHtml();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  function manualSectionHtml() {
    const manuals = state.orderManuals || [];
    const canTick = can('pick:use');
    const addBtn = canTick
      ? `<button type="button" class="btn btn-secondary btn-sm" data-act="manual-add">+ Add manual</button>`
      : '';
    const head = `<h3>${esc(t('nav.manuals'))} ${manuals.length ? `<span class="count">${manuals.length}</span>` : ''}${addBtn ? ` <span class="section-actions">${addBtn}</span>` : ''}</h3>`;
    if (!manuals.length) {
      return `<section class="detail-section" id="manuals-section">${head}<p class="empty-inline">${
        canTick ? 'No manuals on this order yet — attach a product’s manual to work through it block by block.' : 'No manuals on this order.'}</p></section>`;
    }
    return `<section class="detail-section" id="manuals-section">${head}${manuals.map(orderManualHtml).join('')}</section>`;
  }

  function orderManualHtml(m) {
    const pct = m.total ? Math.round((m.answered / m.total) * 100) : 0;
    const done = m.total > 0 && m.answered === m.total && !m.failed && !m.flagged;
    const state = done ? ' done' : (m.flagged ? ' flagged' : (m.failed ? ' failed' : ''));
    const fail = m.failed ? `<span class="pill pill-bad" title="Answered NO">${m.failed} ✗</span>` : '';
    const flags = m.flagged ? `<span class="pill pill-flag" title="Flagged as not done correctly">⚑ ${m.flagged}</span>` : '';
    const log = (m.log || []);
    const logHtml = log.length
      ? `<div class="manual-log hidden" data-log="${m.id}">${log.slice().reverse().map(manualLogLineHtml).join('')}</div>`
      : '';
    return `
      <div class="manual-card${state}" data-manual="${m.id}">
        <div class="manual-head">
          <div class="manual-title">
            <strong>${esc(m.productCode)}</strong> <span class="muted">${esc(m.productName || '')}</span>
          </div>
          <div class="manual-side">
            <span class="pill${done ? ' pill-good' : ''}">${m.answered}/${m.total}</span>
            ${fail}${flags}
            ${log.length ? `<button type="button" class="btn btn-ghost btn-sm" data-act="manual-log" data-id="${m.id}">Log (${log.length})</button>` : ''}
            ${can('manuals:manage') ? `<button type="button" class="btn btn-ghost btn-sm" data-act="manual-remove" data-id="${m.id}" title="Remove manual from order">✕</button>` : ''}
          </div>
        </div>
        <div class="manual-progress">
          <div class="cl-bar${done ? ' is-done' : ''}"><span style="width:${pct}%"></span></div>
          <span class="manual-pct muted">${pct}%</span>
        </div>
        <div class="manual-blocks">${(m.blocks || []).map((b) => orderManualBlockHtml(m.id, b)).join('')}</div>
        ${logHtml}
      </div>`;
  }

  
  
  function manualLogLineHtml(e) {
    const act = esc(e.action.toLowerCase());
    const note = e.note ? `<span class="manual-log-note">“${esc(e.note)}”</span>` : '';
    return `<div class="manual-log-line"><span class="manual-log-act act-${act}">${esc(e.action)}</span>
      <span class="by">${esc(e.username)}</span>${note} <span class="muted">· ${fmtTime(e.createdAt)}</span></div>`;
  }

  function orderManualBlockHtml(manualId, b) {
    const stateCls = b.answer === 'YES' ? ' yes' : (b.answer === 'NO' ? ' no' : '');
    const answered = b.answer
      ? `<span class="checklist-by">${b.answer === 'YES' ? '✓' : '✗'} ${esc(b.answer)} · ${esc(b.answeredBy)} <span class="muted">· ${fmtTime(b.answeredAt)}</span></span>`
      : '<span class="checklist-by muted">open</span>';
    const assignee = b.assignee ? `<span class="manual-assignee" title="Assigned to">${esc(b.assignee)}</span>` : '';
    const btns = can('pick:use') ? `
        <div class="manual-answer-row" role="group" aria-label="Did this?">
          <span class="manual-q muted">Did this?</span>
          <button type="button" class="answer-btn yes${b.answer === 'YES' ? ' on' : ''}" data-act="manual-answer" data-manual="${manualId}" data-block="${b.id}" data-answer="YES">✓ Yes</button>
          <button type="button" class="answer-btn no${b.answer === 'NO' ? ' on' : ''}" data-act="manual-answer" data-manual="${manualId}" data-block="${b.id}" data-answer="NO">✗ No</button>
          ${b.answer ? `<button type="button" class="answer-btn clear" data-act="manual-answer" data-manual="${manualId}" data-block="${b.id}" data-answer="CLEAR" title="Reopen">Clear</button>` : ''}
        </div>` : '';
    const admin = can('manuals:manage');
    const flagBtn = admin && b.answer
      ? (b.flagged
        ? `<button type="button" class="icon-btn flag-btn on" data-act="manual-unflag" data-manual="${manualId}" data-block="${b.id}" title="Clear flag" aria-label="Clear flag">🚩</button>`
        : `<button type="button" class="icon-btn flag-btn" data-act="manual-flag" data-manual="${manualId}" data-block="${b.id}" title="Not done correctly — flag for the user" aria-label="Flag block">⚑</button>`)
      : '';
    const flagBanner = b.flagged ? `
        <div class="manual-flag-note" role="status">
          <span class="flag-ico" aria-hidden="true">⚑</span>
          <span><strong>Flagged by ${esc(b.flaggedBy)}</strong>${b.flagReason ? ` — ${esc(b.flagReason)}` : ''}<span class="muted"> · ${fmtTime(b.flaggedAt)}</span></span>
          ${admin ? `<button type="button" class="btn btn-ghost btn-sm" data-act="manual-unflag" data-manual="${manualId}" data-block="${b.id}">Clear flag</button>` : ''}
        </div>` : '';
    return `
      <div class="manual-block${stateCls}${b.flagged ? ' flagged' : ''}" data-block="${b.id}">
        <div class="manual-block-main">
          <span class="manual-seq">${b.seq + 1}</span>
          <div class="manual-block-text">
            <span class="manual-block-title">${esc(b.title)}</span>
            ${b.body ? `<span class="manual-block-body muted">${esc(b.body)}</span>` : ''}
            ${flagBanner}
          </div>
          ${assignee}
        </div>
        <div class="manual-block-side">
          ${answered}
          <div class="manual-block-actions">${btns}${flagBtn}</div>
        </div>
      </div>`;
  }

  async function doManualAnswer(btn) {
    const manualId = Number(btn.dataset.manual);
    const blockId = Number(btn.dataset.block);
    const answer = btn.dataset.answer;
    if (!manualId || !blockId || !answer) return false;
    btn.disabled = true;
    try {
      await api('POST', `/api/v1/manuals/${manualId}/blocks/${blockId}/answer`, { answer });
      toast(answer === 'CLEAR' ? 'Block reopened' : `Block answered ${answer}`);
      await loadOrderManuals(state.selectedId, true);
      if (location.hash.startsWith('#/manuals')) await loadManualsView(true);
      return true;
    } catch (err) {
      toast(err.message, true);
      btn.disabled = false;
      return false;
    }
  }

  
  
  async function doFlagManualBlock(btn) {
    const manualId = Number(btn.dataset.manual);
    const blockId = Number(btn.dataset.block);
    if (!manualId || !blockId) return;
    const title = btn.closest('.manual-block')?.querySelector('.manual-block-title')?.textContent || 'this block';
    const res = await modal({
      title: 'Flag step as not done correctly',
      description: `“${title}” — the user who ticked this off gets a notification with your reason.`,
      confirmLabel: 'Flag step',
      danger: true,
      fields: [{ name: 'reason', label: 'What is wrong?', type: 'textarea', required: true, autofocus: true, placeholder: 'e.g. seal was not checked, paper tray still empty…' }],
    });
    if (!res) return;
    try {
      await api('POST', `/api/v1/manuals/${manualId}/blocks/${blockId}/flag`, { reason: res.reason.trim() });
      toast('Step flagged — user notified');
    } catch (err) {
      toast(err.message, true);
    }
    await loadOrderManuals(state.selectedId, true);
    if (location.hash.startsWith('#/manuals')) await loadManualsView(true);
  }

  
  async function doUnflagManualBlock(btn) {
    const manualId = Number(btn.dataset.manual);
    const blockId = Number(btn.dataset.block);
    if (!manualId || !blockId) return;
    const ok = await confirmDialog('Clear this flag?',
      'The flag disappears from the block. The tick log keeps the full history.', 'Clear flag');
    if (!ok) return;
    try {
      await api('POST', `/api/v1/manuals/${manualId}/blocks/${blockId}/flag?clear=true`, {});
      toast('Flag cleared');
    } catch (err) {
      toast(err.message, true);
    }
    await loadOrderManuals(state.selectedId, true);
    if (location.hash.startsWith('#/manuals')) await loadManualsView(true);
  }

  
  
  async function openAddManual() {
    if (!state.detail) return;
    let products = state.products;
    if (!products.length) {
      try { products = (await api('GET', '/api/v1/products')).products || []; state.products = products; }
      catch (err) { toast(err.message, true); return; }
    }
    if (!products.length) {
      toast('No products exist yet — create one under Products first.', true);
      return;
    }
    const artikels = new Set((state.checklist || []).map((it) => normCode(it.artikel)));
    const score = (p) => (artikels.has(normCode(p.code)) ? 0 : 1);
    const sorted = products.slice().sort((a, b) => score(a) - score(b) || a.code.localeCompare(b.code));
    const existing = new Set((state.orderManuals || []).map((m) => m.productCode));
    const res = await modal({
      title: 'Attach manual',
      description: `Attach a product's manual to order ${state.detail.orderNumber}. The manual is copied now — later template edits don't change it.`,
      confirmLabel: 'Attach',
      fields: [{
        name: 'productId', label: 'Product', type: 'select', required: true,
        options: sorted.map((p) => ({
          value: String(p.id),
          label: `${p.code} — ${p.name}${existing.has(p.code) ? ' (already attached)' : ''}${artikels.has(normCode(p.code)) ? ' ★' : ''}${!p.approvedBy ? ' — pending approval' : ''}${p.department ? ` [${p.department}]` : ''}`,
        })),
      }],
    });
    if (!res) return;
    try {
      await api('POST', `/api/v1/orders/${state.detail.id}/manuals`, { productId: Number(res.productId) });
      toast('Manual attached');
      await loadOrderManuals(state.detail.id, true);
    } catch (err) {
      toast(err.message, true);
    }
  }

  

  async function loadProducts(silent) {
    try {
      if (!state.departments && can('users:list')) {
        try { const dd = await api('GET', '/api/v1/departments'); state.departments = dd.departments || []; } catch { state.departments = []; }
      }
      const d = await api('GET', '/api/v1/products');
      state.products = d.products || [];
      if (state.productSelId && !state.products.some((p) => p.id === state.productSelId)) {
        state.productSelId = null;
        state.productBlocks = [];
      }
      renderProductsTable();
      if (state.productSelId) await loadProductBlocks(state.productSelId, true);
      else renderProductPanel();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  function renderProductsTable() {
    const tbody = $('#products-table tbody');
    if (!tbody) return;
    const empty = !state.products.length;
    const split = $('#products-split');
    if (split) split.classList.toggle('hidden', empty);
    const hero = $('#products-empty');
    if (hero) hero.classList.toggle('hidden', !empty);
    tbody.innerHTML = state.products.map((p) => `
      <tr class="row${state.productSelId === p.id ? ' selected' : ''}" data-product="${p.id}" tabindex="0">
        <td class="mono">${esc(p.code)}</td>
        <td>${esc(p.name)}</td>
        <td class="muted">${esc(p.description || '')}</td>
        <td>${p.department ? `<span class="badge dept">${esc(p.department)}</span>` : '<span class="muted">—</span>'}</td>
        <td>${p.approvedBy ? `<span class="badge appr-ok" title="Approved by ${esc(p.approvedBy)}">✓ approved</span>` : '<span class="badge appr-pend">pending</span>'}</td>
        <td class="num">${p.blockCount}</td>
      </tr>`).join('');
    const count = $('#product-row-count');
    if (count) {
      count.textContent = state.products.length
        ? `${state.products.length} product${state.products.length === 1 ? '' : 's'}`
        : '';
    }
  }

  async function selectProduct(id) {
    state.productSelId = id;
    state.productBlocks = [];
    renderProductsTable();
    renderProductPanel();
    await loadProductBlocks(id, true);
  }

  async function loadProductBlocks(id, silent) {
    try {
      const d = await api('GET', '/api/v1/products/' + id);
      if (state.productSelId !== id) return;
      state.productBlocks = d.blocks || [];
      renderProductPanel();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  function renderProductPanel() {
    const panel = $('#product-panel');
    const body = $('#product-panel-body');
    if (!panel || !body) return;
    const p = state.products.find((x) => x.id === state.productSelId);
    if (!p) {
      body.innerHTML = '';
      panel.classList.add('hidden');
      const split = $('#products-split');
      if (split) split.classList.add('no-detail');
      return;
    }
    const admin = can('manuals:manage');
    const blocksHtml = state.productBlocks.length
      ? state.productBlocks.map((b) => `
          <div class="sb-block${admin ? ' sb-can-drag' : ''}" data-block="${b.id}" style="--sb-h:${sbHue(b.assignee || '')}">
            <span class="sb-tab" aria-hidden="true"></span>
            ${admin ? '<span class="sb-grip" aria-hidden="true"></span>' : ''}
            <span class="sb-seq mono">${b.seq + 1}</span>
            <div class="sb-text">
              <span class="sb-title">${esc(b.title)}</span>
              ${b.body ? `<span class="sb-body">${esc(b.body)}</span>` : ''}
            </div>
            <span class="sb-who${b.assignee ? '' : ' none'}">${b.assignee ? '@' + esc(b.assignee) : 'anyone'}</span>
            ${admin ? `
            <span class="sb-tools">
              <button type="button" class="sb-tool" data-act="block-up" data-id="${b.id}" title="Move up" aria-label="Move up">↑</button>
              <button type="button" class="sb-tool" data-act="block-down" data-id="${b.id}" title="Move down" aria-label="Move down">↓</button>
              <button type="button" class="sb-tool" data-act="block-edit" data-id="${b.id}" title="Edit block" aria-label="Edit block"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.8 2.8 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/></svg></button>
              <button type="button" class="sb-tool" data-act="block-del" data-id="${b.id}" title="Delete block" aria-label="Delete block"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg></button>
            </span>` : ''}
          </div>`).join('')
      : `<div class="sb-empty">${admin ? 'No steps yet — build the first one in the block shop, then drag things into place.' : 'No blocks.'}</div>`;

    const addForm = admin ? `
        <form class="sb-form" id="block-form">
          <input id="block-title" placeholder="Step title, e.g. “Check the seal”" required autocomplete="off">
          <input id="block-body" placeholder="Description (optional)" autocomplete="off">
          <select id="block-assignee" aria-label="Assign to">
            <option value="">Unassigned — anyone can do it</option>
            ${(state.userBriefs || []).map((u) => `<option value="${esc(u.username)}">${esc(u.displayName || u.username)}</option>`).join('')}
          </select>
          <button type="submit" class="btn btn-primary btn-sm">+ Add step</button>
        </form>` : '';
    body.innerHTML = `
      <div class="detail-head">
        <div class="detail-head-text">
          <div class="eyebrow">Product manual</div>
          <h2 class="mono">${esc(p.code)}</h2>
          <div class="detail-badges">
            ${badge('status-Completed', p.name)}
            ${p.department ? `<span class="badge dept">${esc(p.department)}</span>` : ''}
            ${p.approvedBy ? `<span class="badge appr-ok" title="Approved by ${esc(p.approvedBy)}">✓ approved</span>` : '<span class="badge appr-pend">pending SC approval</span>'}
          </div>
        </div>
        ${admin || can('manuals:approve') ? `
        <div class="detail-head-actions">
          ${!p.approvedBy && can('manuals:approve') ? `<button type="button" class="btn btn-primary btn-sm" data-act="product-approve" data-id="${p.id}" title="Approve this manual for use">✓ Approve</button>` : ''}
          ${admin ? `<button type="button" class="btn btn-secondary btn-sm" data-act="product-dept" data-id="${p.id}" title="Move to another department">⇄ Department</button>` : ''}
          ${admin ? `
          <button type="button" class="icon-btn" data-act="product-edit" title="Edit product" aria-label="Edit product">✎</button>
          <button type="button" class="icon-btn" data-act="product-del" title="Delete product" aria-label="Delete product"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg></button>` : ''}
        </div>` : ''}
        <div class="detail-head-actions"><button type="button" class="icon-btn" data-act="panel-close" aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg></button></div>
      </div>
      <div class="sb-wrap">
        ${admin ? `
        <aside class="sb-palette">
          <header>Block shop</header>
          <div class="sb-pal-sec">New step</div>
          ${addForm}
          <div class="sb-pal-sec">People — drop on a step</div>
          <div class="sb-chips">
            <span class="sb-chip" data-sb-user="" style="--sb-h:24" title="Unassign">anyone</span>
            ${(state.userBriefs || []).map((u) => `<span class="sb-chip" data-sb-user="${esc(u.username)}" style="--sb-h:${sbHue(u.username)}">@${esc(u.username)}</span>`).join('')}
          </div>
        </aside>` : ''}
        <div class="sb-stage">
          <header class="sb-stage-head">
            <span>Steps</span><strong class="mono">${state.productBlocks.length}</strong>
            ${admin ? '<span class="muted sb-hint">drag blocks to reorder · click ✎ to edit</span>' : ''}
          </header>
          <div class="sb-stack" id="sb-stack">${blocksHtml}</div>
        </div>
      </div>`;
    sbBindOnce();
    panel.classList.remove('hidden');
    const split = $('#products-split');
    if (split) split.classList.remove('no-detail');
  }

  function deptOptions(current) {
    return [{ value: '', label: 'No department — everyone' },
      ...(state.departments || []).map((d) => ({ value: d.name, label: d.name }))];
  }

  async function openQuickManual() {
    const res = await modal({
      title: 'Quick manual',
      description: 'One step per line — the manual and its blocks are created in one go.',
      confirmLabel: 'Create manual',
      wide: true,
      fields: [
        { name: 'code', label: 'Code', placeholder: 'e.g. PRD-LAPTOP', required: true, half: true },
        { name: 'department', label: 'Department', type: 'select', options: deptOptions(), half: true },
        { name: 'name', label: 'Name', placeholder: 'e.g. Laptop refurbish flow', required: true },
        { name: 'steps', label: 'Steps (one per line)', type: 'textarea', placeholder: 'Unbox and inspect\nApply configuration\nRun diagnostics\nPack and label', required: true },
      ],
    });
    if (!res) return;
    const steps = String(res.steps || '').split('\n').map((x) => x.trim()).filter(Boolean);
    try {
      await api('POST', '/api/v1/products/quick', {
        code: res.code.trim(), name: res.name.trim(), department: res.department || '', steps,
      });
      toast('Manual created');
      await loadProducts(true);
    } catch (err) { toast(err.message, true); }
  }

  async function openCreateProduct() {
    const res = await modal({
      title: 'New product',
      description: 'Products carry a manual. Use the article code from the pick lines so the manual can be suggested on orders.',
      confirmLabel: 'Create product',
      fields: [
        { name: 'code', label: 'Code (artikel)', required: true, autofocus: true, placeholder: 'e.g. PC-DEL-3020' },
        { name: 'name', label: 'Name', required: true, placeholder: 'e.g. Dell Latitude 3020' },
        { name: 'description', label: 'Description', type: 'textarea' },
        { name: 'department', label: 'Department', type: 'select', options: deptOptions() },
      ],
    });
    if (!res) return;
    try {
      const d = await api('POST', '/api/v1/products', {
        code: res.code.trim(), name: res.name.trim(), description: res.description || '', department: res.department || '',
      });
      toast('Product created');
      await loadProducts(true);
      await selectProduct(d.product.id);
    } catch (err) {
      toast(err.message, true, { label: t('toast.retry'), fn: openCreateProduct });
    }
  }

  

  let manualsViewTimer = null;

  function startManualsPolling() {
    stopManualsPolling();
    manualsViewTimer = setInterval(async () => {
      if (!state.user || !location.hash.startsWith('#/manuals')) return stopManualsPolling();
      await loadManualsView(true);
    }, 5000);
  }
  function stopManualsPolling() {
    if (manualsViewTimer) { clearInterval(manualsViewTimer); manualsViewTimer = null; }
  }

  async function loadManualsView(silent) {
    try {
      const d = await api('GET', '/api/v1/manuals');
      state.manuals = d.manuals || [];
      if (state.manualsSelId && !state.manuals.some((m) => m.manualId === state.manualsSelId)) {
        state.manualsSelId = null;
      }
                                                                           
      renderManualsList();
      renderManualPanel();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  function renderManualsList() {
    const tbody = $('#manuals-table tbody');
    if (!tbody) return;
    const mineOnly = state.manualsMine;
    const visible = mineOnly
      ? state.manuals.filter((m) => (m.blocks || []).some((b) => b.assignee === state.user.username))
      : state.manuals;
    const empty = !visible.length;
    const split = $('#manuals-split');
    if (split) split.classList.toggle('hidden', empty);
    const hero = $('#manuals-empty');
    if (hero) hero.classList.toggle('hidden', !empty);
    tbody.innerHTML = visible.map((m) => {
      const pct = m.total ? Math.round((m.answered / m.total) * 100) : 0;
      const done = m.total > 0 && m.answered === m.total && !m.failed && !m.flagged;
      return `
        <tr class="row${state.manualsSelId === m.manualId ? ' selected' : ''}" data-manual-row="${m.manualId}" tabindex="0">
          <td>${esc(m.orderNumber)}</td>
          <td><span class="mono">${esc(m.productCode)}</span> <span class="muted">${esc(m.productName || '')}</span></td>
          <td>${badge('status-' + esc(statusClass(m.status)), statusLabel(m.status))}</td>
          <td><div class="manual-progress"><div class="cl-bar${done ? ' is-done' : ''}"><span style="width:${pct}%"></span></div><span class="manual-pct muted">${pct}%</span></div></td>
          <td class="num"><span class="pill${done ? ' pill-good' : ''}">${m.answered}/${m.total}</span>${m.failed ? ` <span class="pill pill-bad" title="Answered NO">${m.failed}✗</span>` : ''}${m.flagged ? ` <span class="pill pill-flag" title="Flagged as not done correctly">⚑ ${m.flagged}</span>` : ''}</td>
        </tr>`;
    }).join('');
  }

  function renderManualPanel() {
    const panel = $('#manual-panel');
    const body = $('#manual-panel-body');
    if (!panel || !body) return;
    const m = state.manuals.find((x) => x.manualId === state.manualsSelId);
    if (!m) {
      body.innerHTML = '';
      panel.classList.add('hidden');
      const split = $('#manuals-split');
      if (split) split.classList.add('no-detail');
      return;
    }
    const mineOnly = state.manualsMine;
    let blocks = m.blocks || [];
    let mineNote = '';
    if (mineOnly) {
      blocks = blocks.filter((b) => b.assignee === state.user.username);
      mineNote = '<p class="empty-inline muted">Showing only blocks assigned to you.</p>';
    }
    const log = (m.log || []);
    body.innerHTML = `
      <div class="detail-head">
        <div class="detail-head-text">
          <div class="eyebrow">Order #${m.orderId}</div>
          <h2>${esc(m.orderNumber)}</h2>
          <div class="detail-badges">
            ${badge('status-' + esc(statusClass(m.status)), statusLabel(m.status))}
            <span class="mono muted">${esc(m.productCode)}</span>
          </div>
        </div>
        <div class="detail-head-actions">
          <button type="button" class="btn btn-secondary btn-sm" data-act="manual-open-order">Open order</button>
          <button type="button" class="icon-btn" data-act="panel-close" aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg></button>
        </div>
      </div>
      <div class="detail-scroll">
        <section class="detail-section">
          <h3>Blocks ${m.total ? `<span class="count">${m.answered}/${m.total}</span>` : ''}${m.flagged ? ` <span class="pill pill-flag">⚑ ${m.flagged} flagged</span>` : ''}</h3>
          ${blocks.length ? blocks.map((b) => orderManualBlockHtml(m.manualId, b)).join('') : mineNote || '<p class="empty-inline">No blocks.</p>'}
        </section>
        ${log.length ? `
        <section class="detail-section">
          <h3>Tick log <span class="count">${log.length}</span></h3>
          <div class="manual-log">${log.slice().reverse().map(manualLogLineHtml).join('')}</div>
        </section>` : ''}
      </div>`;
    panel.classList.remove('hidden');
    const split = $('#manuals-split');
    if (split) split.classList.remove('no-detail');
  }

                                   
  function sbHue(name) {
    if (!name) return 24;
    let h = 7;
    for (const c of String(name)) h = (h * 31 + c.charCodeAt(0)) % 360;
    return h;
  }

  const SB = { drag: null };

  function sbBindOnce() {
    if (SB.bound) return;
    SB.bound = true;
    const host = $('#product-panel-body');
    if (!host) return;

    SB.move = (e) => {
      const d = SB.drag;
      if (!d) return;
      d.ghost.style.left = (e.clientX + 14) + 'px';
      d.ghost.style.top = (e.clientY - 18) + 'px';
      const stack = $('#sb-stack');
      if (!stack) return;
      const blocks = [...stack.querySelectorAll('.sb-block')].filter((b) => !b.classList.contains('sb-dragging'));
      if (d.type === 'block') {
        let t = blocks.length;
        for (let i = 0; i < blocks.length; i++) {
          const r = blocks[i].getBoundingClientRect();
          if (e.clientY < r.top + r.height / 2) { t = i; break; }
        }
        d.target = t;
        const ref = blocks[t] || null;
        if (ref) stack.insertBefore(d.gap, ref); else stack.appendChild(d.gap);
      } else {
        blocks.forEach((b) => {
          const r = b.getBoundingClientRect();
          b.classList.toggle('sb-drop', e.clientY > r.top && e.clientY < r.bottom && e.clientX > r.left && e.clientX < r.right);
        });
      }
    };

    SB.up = async (e) => {
      window.removeEventListener('pointermove', SB.move);
      const d = SB.drag;
      SB.drag = null;
      if (!d) return;
      d.ghost.remove();
      document.querySelectorAll('.sb-drop').forEach((x) => x.classList.remove('sb-drop'));
      const pid = state.productSelId;
      if (d.type === 'block') {
        d.gap.remove();
        d.el.classList.remove('sb-dragging');
        const from = d.from;
        let target = d.target;
        if (target > from) target -= 1;
        if (target !== from && pid) {
          const dir = target < from ? 'up' : 'down';
          const n = Math.abs(target - from);
          try {
            for (let i = 0; i < n; i++) {
              await api('POST', `/api/v1/products/${pid}/blocks/${d.id}/move`, { direction: dir });
            }
            await loadProductBlocks(pid, true);
          } catch (err) { toast(err.message, true); await loadProductBlocks(pid, true); }
        }
        return;
      }
      const over = document.elementFromPoint(e.clientX, e.clientY);
      const blk = over && over.closest ? over.closest('.sb-block') : null;
      if (blk && pid) {
        const bid = Number(blk.dataset.block);
        try {
          await api('POST', `/api/v1/products/${pid}/blocks/${bid}`, { assignee: d.user });
          await loadProductBlocks(pid, true);
        } catch (err) { toast(err.message, true); }
      }
    };

    host.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      if (e.target.closest('button, input, select, textarea, a')) return;
      const chip = e.target.closest('.sb-chip');
      const block = e.target.closest('.sb-block.sb-can-drag');
      if (!chip && !block) return;
      e.preventDefault();
      const stack = host.querySelector('#sb-stack');
      if (!stack) return;
      if (chip) {
        SB.drag = { type: 'chip', user: chip.dataset.sbUser || '' };
        const ghost = chip.cloneNode(true);
        ghost.classList.add('sb-ghost');
        document.body.appendChild(ghost);
        SB.drag.ghost = ghost;
      } else {
        const blocks = [...stack.querySelectorAll('.sb-block')];
        const from = blocks.indexOf(block);
        block.classList.add('sb-dragging');
        const ghost = block.cloneNode(true);
        ghost.classList.add('sb-ghost');
        ghost.style.width = block.offsetWidth + 'px';
        document.body.appendChild(ghost);
        const gap = document.createElement('div');
        gap.className = 'sb-gap';
        SB.drag = { type: 'block', id: Number(block.dataset.block), from, target: from, ghost, gap, el: block };
        stack.appendChild(gap);
      }
      SB.move(e);
      window.addEventListener('pointermove', SB.move);
      window.addEventListener('pointerup', SB.up, { once: true });
    });
  }

  function renderDetail() {
    const panel = $('#detail-panel');
    const o = state.detail;
    if (!o) return;

    const onHold = o.onHold;
    const held = String(o.status).startsWith('Held_');
    const completed = o.status === 'Completed';
    const targets = nextStates(o.status);
    const sla = o.sla || { status: slaOf(o) };
    const holds = o.activeHolds || [];
    const qcs = o.qcChecks || [];

    const holdsHtml = holds.length
      ? holds.map((h) => `
          <div class="list-item hold-item">
            <div>
              <div class="reason">${esc(h.reason)}</div>
              <div class="meta-line">${esc(h.createdBy)} · ${fmtTime(h.createdAt)}</div>
            </div>
            ${can('holds:resolve') ? `<button class="btn btn-secondary btn-sm" data-act="resolve" data-id="${esc(h.id)}">Resolve</button>` : ''}
          </div>`).join('')
      : '<p class="empty-inline">No active holds.</p>';

    const qcHtml = qcs.length
      ? qcs.map((c) => `
          <div class="list-item qc-item">
            <div class="qc-top">
              ${badge('status-' + esc(c.status), c.status)}
              <span class="meta-line" style="margin:0">${esc(c.inspectorId)} · ${fmtTime(c.createdAt)}</span>
            </div>
            ${c.notes ? `<div class="notes">${esc(c.notes)}</div>` : ''}
          </div>`).join('')
      : '<p class="empty-inline">No QC checks yet.</p>';

    const auditHtml = state.audit.length
      ? `<div class="timeline">${state.audit.map((l) => `
          <div class="audit-item">
            <div class="action">${esc(l.action)}</div>
            <div class="when"><span class="by">${esc(l.performedBy)}</span> · ${fmtTime(l.timestamp)}</div>
          </div>`).join('')}</div>`
      : '<p class="empty-inline">No audit entries.</p>';

    const commentsHtml = `
      <div id="comments-container">${commentsListHtml(state.comments || [])}</div>
      ${can('comments:post') ? `
        <form class="comment-composer" id="comment-form">
          <textarea id="comment-input" rows="2" maxlength="2000" placeholder="Add a comment. Use @username to mention someone…" aria-label="Comment"></textarea>
          <div class="composer-foot">
            <span class="muted">@username to mention</span>
            <button type="submit" class="btn btn-secondary btn-sm">Comment</button>
          </div>
        </form>` : ''}`;

    const scanHtml = can('scan:use') ? `
      <div class="scan-inline">
        <input id="drawer-scan-input" class="mono" placeholder="Scan barcode → order number" autocomplete="off">
        <div id="drawer-scan-feedback" class="scan-feedback" aria-live="polite"></div>
      </div>` : '';

    const actionsHtml = [];
    if (can('orders:transition') && !held && !completed && targets.length) {
      actionsHtml.push(`
        <form class="od-action-block" id="transition-form">
          <span class="label">${esc(t('detail.next'))}</span>
          <div class="od-action-row">
            <select id="transition-target" aria-label="Target status">${targets.map((s) => `<option value="${s}">${statusLabel(s)}</option>`).join('')}</select>
            <button type="submit" class="btn btn-primary btn-sm">${esc(t('btn.transition'))}</button>
          </div>
        </form>`);
    }
    if (can('holds:create') && !completed && !onHold) {
      actionsHtml.push(`
        <form class="od-action-block" id="hold-form">
          <span class="label">${esc(t('detail.placeHold'))}</span>
          <div class="od-action-row">
            <input id="hold-reason" placeholder="${esc(t('detail.holdReason'))}" required autocomplete="off">
            <button type="submit" class="btn btn-secondary btn-sm">${esc(t('btn.hold'))}</button>
          </div>
        </form>`);
    }
    if (can('qc:submit') && !completed) {
      actionsHtml.push(`
        <form class="od-action-block" id="qc-form">
          <span class="label">${esc(t('detail.qc'))}</span>
          <div class="od-action-row">
            <select id="qc-status" aria-label="QC result" style="flex:0 0 96px"><option value="PASS">PASS</option><option value="FAIL">FAIL</option></select>
            <select id="qc-reason" class="hidden" aria-label="Fail reason" style="flex:1 1 150px"></select>
            <input id="qc-notes" placeholder="Notes (optional)" autocomplete="off">
            <button type="submit" class="btn btn-secondary btn-sm">${esc(t('btn.submit'))}</button>
          </div>
        </form>`);
    }

    const briefs = state.userBriefs || [];
    const sharedWith = (o.sharedWith || '').split(',').map((s) => s.trim()).filter(Boolean);
    const allAssigned = [o.assignee].filter(Boolean).concat(sharedWith);
    const assigneeDisplay = allAssigned.map((a) => '@' + esc(a)).join(', ') || esc(t('detail.unassigned'));
    const assigneeHtml = can('orders:transition') && briefs.length
      ? `<select id="assignee-select" aria-label="${esc(t('detail.assignee'))}">
           <option value="">${esc(t('detail.unassigned'))}</option>
           ${briefs.map((u) => `<option value="${esc(u.username)}"${o.assignee === u.username ? ' selected' : ''}>${esc(u.displayName || u.username)}</option>`).join('')}
         </select>`
      : `<span class="assignee-list">${assigneeDisplay}</span>`;

    const hasWorkflow = window.WorkFlow && WorkFlow.buttonFor(o);
    const isShared = !!(o.sharedWith || '').trim();
    const custAvatar = (o.customerName || '?').charAt(0).toUpperCase();
    const custHue = (o.customerName || '?').charCodeAt(0) % 360;

    panel.innerHTML = `
      <div class="od-wrap">
        <div class="od-head">
          <div class="od-head-left">
            <div class="od-cust-card">
              <div class="od-cust-ava" style="--h:${custHue}deg">${custAvatar}</div>
              <div class="od-cust-info">
                <div class="od-cust-name">${esc(o.customerName || '—')}</div>
                <div class="od-cust-sub">${esc(t('orders.customer'))}</div>
              </div>
            </div>
            <div class="od-head-main">
              <div class="od-eyebrow">Order #${o.id}</div>
              <h2 class="copyable" data-copy="${esc(o.orderNumber)}" title="Click to copy">${esc(o.orderNumber)}</h2>
              <div class="od-badges">
                ${badge('status-' + esc(statusClass(o.status)), statusLabel(o.status))}
                ${badge('sla-' + esc(sla.status), String(sla.status).replace('_', ' '))}
                ${isShared ? '<span class="od-chip status-share">Shared</span>' : ''}
              </div>
            </div>
          </div>
          <div class="od-head-actions">
            ${hasWorkflow ? `<span data-work-slot="${o.id}">${WorkFlow.buttonFor(o)}</span>` : ''}
            <button type="button" class="icon-btn od-act-btn" id="detail-share-btn" aria-label="Share order" title="Mark order as shared" ${o.status === 'Completed' ? 'disabled' : ''}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M5 12h14M12 5v14"/></svg>
            </button>
            <button type="button" class="icon-btn od-act-btn" id="detail-share-with-btn" aria-label="Share with colleague" title="Share with colleague" ${o.status === 'Completed' ? 'disabled' : ''}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M16 8a6 6 0 0 1 6 6v7h-4v-7a2 2 0 0 0-2-2 2 2 0 0 0-2 2v7h-4v-7a6 6 0 0 1 6-6z"/><circle cx="9" cy="9" r="1"/></svg>
            </button>
            <button type="button" class="icon-btn od-act-btn" id="copy-link-btn" aria-label="Copy link" title="Copy link to this order">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54 .54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
            </button>
            <button type="button" class="icon-btn od-x" id="detail-close" aria-label="Close detail" title="Close (Esc)">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
            </button>
          </div>
        </div>
        <div class="od-body">
          <section class="od-sec" id="order-meta-sec">
            <div class="od-sec-head">
              <span class="od-sec-title">${esc(t('detail.meta'))}</span>
            </div>
            <div class="od-grid2">
              <div class="od-field">
                <label class="f-label">${esc(t('detail.assignee'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"></path><circle cx="12" cy="7" r="4"></circle></svg></span><span>${assigneeHtml}</span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('meta.target'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><circle cx="12" cy="8" r="7"></circle><polyline points="12 8 12 12 15 15"></polyline></svg></span><span>${fmtTime(o.targetCompletionAt)}<span class="f-sub">${esc(relTime(o.targetCompletionAt))}</span></span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('meta.created'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect><line x1="16" y1="2" x2="16" y2="6"></line><line x1="8" y1="2" x2="8" y2="6"></line><line x1="3" y1="10" x2="21" y2="10"></line></svg></span><span>${fmtTime(o.createdAt)}<span class="f-sub">${esc(relTime(o.createdAt))}</span></span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('meta.updated'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><polyline points="1 4 1 10 7 10"></polyline><polyline points="23 20 23 14 17 14"></polyline><polyline points="1 10 7 10 7 16"></polyline><polyline points="23 14 17 14 17 20"></polyline><path d="M5 18V8M19 8V18M8 5H17M6 14H18"></path></svg></span><span>${fmtTime(o.updatedAt)}<span class="f-sub">${esc(relTime(o.updatedAt))}</span></span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('meta.holds'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg></span><span>${holds.length ? holds.length + esc(t('meta.active')) : esc(t('meta.none'))}</span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('orders.customer'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path><circle cx="9" cy="7" r="4"></circle><path d="M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"></path></svg></span><span>${esc(o.customerName || '—')}</span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('info.debit'))}</label>
                <div class="f-value mono copyable" data-copy="${esc(o.debitNumber)}" title="Click to copy"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><line x1="12" y1="1" x2="12" y2="23"></path><path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"></path></svg></span><span>${esc(o.debitNumber || '—')}</span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('info.device'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect><line x1="8" y1="21" x2="16" y2="21"></line><line x1="12" y1="17" x2="12" y2="21"></line></svg></span><span>${esc(o.device || '—')}</span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('info.asset'))}</label>
                <div class="f-value mono copyable" data-copy="${esc(o.assetNumber)}" title="Click to copy"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="M14.5 2h3a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-3a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1zM15 7h2v4h-2z"></path></svg></span><span>${esc(o.assetNumber || '—')}</span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('info.config'))}</label>
                <div class="f-value"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1M3 3l18 18"></path></svg></span><span>${esc(o.configuration || '—')}</span></div>
              </div>
              <div class="od-field">
                <label class="f-label">${esc(t('info.omni'))}</label>
                <div class="f-value mono copyable" data-copy="${esc(o.omnitrackerTicket)}" title="Click to copy"><span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line><polyline points="10 9 9 9 8 9"></polyline></svg></span><span>${esc(o.omnitrackerTicket || '—')}</span></div>
              </div>
            </div>
          </section>

          ${actionsHtml.length ? `<section class="od-sec" id="order-actions-sec">
            <div class="od-sec-head"><span class="od-sec-title">${esc(t('detail.actions'))}</span></div>
            <div class="od-actions">${actionsHtml.join('')}</div>
          </section>` : ''}

          <section class="od-sec" id="order-holds-sec">
            <div class="od-sec-head"><span class="od-sec-title">${esc(t('detail.holds'))}</span>${holds.length ? `<span class="count">${holds.length}</span>` : ''}</div>
            ${holdsHtml}
          </section>

          <section class="od-sec" id="order-qc-sec">
            <div class="od-sec-head"><span class="od-sec-title">${esc(t('detail.qcs'))}</span>${qcs.length ? `<span class="count">${qcs.length}</span>` : ''}</div>
            ${qcHtml}
          </section>

          ${checklistSectionHtml().replace('detail-section', 'od-sec')}
          ${manualSectionHtml().replace('detail-section', 'od-sec')}

          <section class="od-sec" id="order-comments-sec">
            <div class="od-sec-head"><span class="od-sec-title">${esc(t('detail.comments'))}</span>${state.comments && state.comments.length ? `<span class="count">${state.comments.length}</span>` : ''}</div>
            ${commentsHtml}
          </section>

          ${can('scan:use') ? `<section class="od-sec" id="order-scan-sec">
            <div class="od-sec-head"><span class="od-sec-title">${esc(t('detail.scan'))}</span></div>
            ${scanHtml}
          </section>` : ''}

          <section class="od-sec" id="order-audit-sec">
            <div class="od-sec-head"><span class="od-sec-title">${esc(t('detail.audit'))}</span>${state.audit.length ? `<span class="count">${state.audit.length}</span>` : ''}</div>
            ${auditHtml}
          </section>
        </div>
        <div class="od-foot">
          <div class="od-sum"></div>
          <div class="od-foot-btns"></div>
        </div>
      </div>`;
    panel.classList.remove('hidden');
    $('#orders-split').classList.remove('no-detail');

    const copyBtn = $('#copy-link-btn');
    if (copyBtn) {
      copyBtn.addEventListener('click', async () => {
        try {
          await navigator.clipboard.writeText(location.href);
          toast(t('toast.copied'));
        } catch {  }
      });
    }
    const assigneeSel = $('#assignee-select');
    if (assigneeSel) {
      assigneeSel.addEventListener('change', async () => {
        const v = assigneeSel.value;
        const ok = await act('POST', `/api/v1/orders/${o.id}/assign`, { assignee: v }, null, { quiet: true });
        if (ok) toast(v ? `Assigned to ${v}` : 'Unassigned');
      });
    }

  }
  const shareBtn = $('#detail-share-btn');
  if (shareBtn) {
    shareBtn.addEventListener('click', async () => {
      const parentId = prompt('Enter parent order ID (leave blank for none):');
      if (parentId === null) return;
      const parentIdNum = parentId.trim() ? parseInt(parentId.trim(), 10) : 0;
      if (parentId.trim() && isNaN(parentIdNum)) { toast('Invalid order ID', true); return; }
      shareBtn.disabled = true;
      shareBtn.style.opacity = '0.6';
      try {
        await api('POST', `/api/v1/orders/${o.id}/share`, { parentOrderId: parentIdNum || undefined });
        shareBtn.title = 'Shared';
        toast('Order marked as shared');
      } catch (err) {
        toast(err.message || 'Share failed', true);
        shareBtn.disabled = false;
        shareBtn.style.opacity = '1';
      }
    });
  }

  const shareWithBtn = $('#detail-share-with-btn');
  if (shareWithBtn) {
    let detailShareSelect = null;
    shareWithBtn.addEventListener('click', async () => {
      if (detailShareSelect) {
        detailShareSelect.remove();
        detailShareSelect = null;
        return;
      }
      try {
        const briefs = state.userBriefs || [];
        const already = (o.sharedWith || '').split(',').map((s) => s.trim()).filter(Boolean);
        const others = briefs.filter((u) => u.username !== (state.user && state.user.username) && !already.includes(u.username));
        if (!others.length) { toast('No colleagues available', true); return; }
        const wrap = document.createElement('div');
        wrap.className = 'detail-share-with-wrap';
        wrap.style.cssText = 'display:inline-flex; gap:6px; align-items:center; margin-left:8px;';
        const sel = document.createElement('select');
        sel.className = 'detail-share-with-select';
        sel.innerHTML = '<option value="">— select colleague —</option>' + others.map((u) => `<option value="${esc(u.username)}">@${esc(u.username)} ${u.displayName ? '(' + esc(u.displayName) + ')' : ''}</option>`).join('');
        const goBtn = document.createElement('button');
        goBtn.type = 'button';
        goBtn.className = 'icon-btn';
        goBtn.title = 'Add';
        goBtn.disabled = true;
        goBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>';
        sel.addEventListener('change', () => { goBtn.disabled = !sel.value; });
        goBtn.addEventListener('click', async () => {
          const username = sel.value;
          if (!username) return;
          goBtn.disabled = true;
          try {
            await api('POST', `/api/v1/orders/${o.id}/share-with`, { sharedWith: username });
            detailShareSelect.remove();
            detailShareSelect = null;
            shareWithBtn.title = 'Shared with ' + username;
            toast('Shared with ' + username);
            try { await api('GET', '/api/v1/orders/' + o.id); } catch { }
          } catch (err) {
            toast(err.message || 'Share failed', true);
            goBtn.disabled = false;
          }
        });
        wrap.appendChild(sel);
        wrap.appendChild(goBtn);
        shareWithBtn.parentNode.insertBefore(wrap, shareWithBtn.nextSibling);
        detailShareSelect = wrap;
        sel.focus();
      } catch (err) {
        toast('Could not load users: ' + (err.message || ''), true);
      }
    });
  }



  async function populateQCReasons() {
  const sel = $('#qc-reason');
  if (!sel || sel.options.length) return;
  try {
    const d = await api('GET', '/api/v1/qc/reasons');
    sel.innerHTML = '<option value="">reason…</option>' + (d.reasons || []).map((r) => `<option>${esc(r)}</option>`).join('');
  } catch (err) {                                 }
}
document.addEventListener('change', (e) => {
  if (e.target.id === 'qc-status') {
    const r = $('#qc-reason');
    if (r) r.classList.toggle('hidden', e.target.value !== 'FAIL');
  }
});
