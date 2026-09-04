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
          label: `${p.code} — ${p.name}${existing.has(p.code) ? ' (already attached)' : ''}${artikels.has(normCode(p.code)) ? ' ★' : ''}`,
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
          <div class="manual-block" data-block="${b.id}">
            <div class="manual-block-main">
              <span class="manual-seq">${b.seq + 1}</span>
              <div class="manual-block-text">
                <span class="manual-block-title">${esc(b.title)}</span>
                ${b.body ? `<span class="manual-block-body muted">${esc(b.body)}</span>` : ''}
              </div>
              ${b.assignee ? `<span class="manual-assignee" title="Assigned to">${esc(b.assignee)}</span>` : ''}
            </div>
            ${admin ? `
            <div class="manual-block-side">
              <div class="manual-edit-row">
                <button type="button" class="icon-btn" data-act="block-up" data-id="${b.id}" title="Move up" aria-label="Move up">↑</button>
                <button type="button" class="icon-btn" data-act="block-down" data-id="${b.id}" title="Move down" aria-label="Move down">↓</button>
                <button type="button" class="icon-btn" data-act="block-edit" data-id="${b.id}" title="Edit block" aria-label="Edit block">✎</button>
                <button type="button" class="icon-btn" data-act="block-del" data-id="${b.id}" title="Delete block" aria-label="Delete block">✕</button>
              </div>
            </div>` : ''}
          </div>`).join('')
      : '<p class="empty-inline">No blocks yet — add the first step below.</p>';
    const addForm = admin ? `
        <form class="manual-add-form" id="block-form">
          <span class="label">Add block</span>
          <input id="block-title" placeholder="Block title, e.g. “Check the seal”" required autocomplete="off">
          <input id="block-body" placeholder="Description (optional)" autocomplete="off">
          <select id="block-assignee" aria-label="Assign to">
            <option value="">Unassigned — anyone can do it</option>
            ${(state.userBriefs || []).map((u) => `<option value="${esc(u.username)}">${esc(u.displayName || u.username)}</option>`).join('')}
          </select>
          <button type="submit" class="btn btn-primary btn-sm">Add block</button>
        </form>` : '';
    body.innerHTML = `
      <div class="detail-head">
        <div class="detail-head-text">
          <div class="eyebrow">Product manual</div>
          <h2 class="mono">${esc(p.code)}</h2>
          <div class="detail-badges">${badge('status-Completed', p.name)}</div>
        </div>
        ${admin ? `
        <div class="detail-head-actions">
          <button type="button" class="icon-btn" data-act="product-edit" title="Edit product" aria-label="Edit product">✎</button>
          <button type="button" class="icon-btn" data-act="product-del" title="Delete product" aria-label="Delete product">✕</button>
        </div>` : ''}
      </div>
      <div class="detail-scroll">
        ${p.description ? `<p class="muted" style="margin:0 0 10px">${esc(p.description)}</p>` : ''}
        <section class="detail-section">
          <h3>Blocks ${state.productBlocks.length ? `<span class="count">${state.productBlocks.length}</span>` : ''}</h3>
          ${blocksHtml}
        </section>
        ${addForm}
      </div>`;
    panel.classList.remove('hidden');
    const split = $('#products-split');
    if (split) split.classList.remove('no-detail');
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
      ],
    });
    if (!res) return;
    try {
      const d = await api('POST', '/api/v1/products', {
        code: res.code.trim(), name: res.name.trim(), description: res.description || '',
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
      if (!state.token || !location.hash.startsWith('#/manuals')) return stopManualsPolling();
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
      if (!state.manualsSelId && state.manuals.length) {
        state.manualsSelId = state.manuals[0].manualId;
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

    const actions = [];
    if (can('orders:transition') && !held && !completed && targets.length) {
      actions.push(`
        <form class="action-block" id="transition-form">
          <span class="label">${esc(t('detail.next'))}</span>
          <div class="action-row">
            <select id="transition-target" aria-label="Target status">${targets.map((s) => `<option value="${s}">${statusLabel(s)}</option>`).join('')}</select>
            <button type="submit" class="btn btn-primary btn-sm">${esc(t('btn.transition'))}</button>
          </div>
        </form>`);
    }
    if (can('holds:create') && !completed && !onHold) {
      actions.push(`
        <form class="action-block" id="hold-form">
          <span class="label">${esc(t('detail.placeHold'))}</span>
          <div class="action-row">
            <input id="hold-reason" placeholder="${esc(t('detail.holdReason'))}" required autocomplete="off">
            <button type="submit" class="btn btn-secondary btn-sm">${esc(t('btn.hold'))}</button>
          </div>
        </form>`);
    }
    if (can('qc:submit') && !completed) {
      actions.push(`
        <form class="action-block" id="qc-form">
          <span class="label">${esc(t('detail.qc'))}</span>
          <div class="action-row">
            <select id="qc-status" aria-label="QC result" style="flex:0 0 96px"><option value="PASS">PASS</option><option value="FAIL">FAIL</option></select>
            <input id="qc-notes" placeholder="Notes (optional)" autocomplete="off">
            <button type="submit" class="btn btn-secondary btn-sm">${esc(t('btn.submit'))}</button>
          </div>
        </form>`);
    }

    const briefs = state.userBriefs || [];
    const assigneeHtml = can('orders:transition') && briefs.length
      ? `<select id="assignee-select" aria-label="${esc(t('detail.assignee'))}">
           <option value="">${esc(t('detail.unassigned'))}</option>
           ${briefs.map((u) => `<option value="${esc(u.username)}"${o.assignee === u.username ? ' selected' : ''}>${esc(u.displayName || u.username)}</option>`).join('')}
         </select>`
      : `<span>${o.assignee ? esc(o.assignee) : esc(t('detail.unassigned'))}</span>`;

    panel.innerHTML = `
      <div class="detail-head">
        <div class="detail-head-text">
          <div class="eyebrow">Order #${o.id}</div>
          <h2>${esc(o.orderNumber)}</h2>
          <div class="detail-badges">
            ${badge('status-' + esc(statusClass(o.status)), statusLabel(o.status))}
            ${badge('sla-' + esc(sla.status), String(sla.status).replace('_', ' '))}
          </div>
        </div>
        <div class="detail-head-actions">
          <button type="button" class="icon-btn" id="copy-link-btn" aria-label="Copy link" title="Copy link to this order">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
          </button>
          <button type="button" class="icon-btn" id="detail-close" aria-label="Close detail" title="Close (Esc)">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
          </button>
        </div>
      </div>
      <div class="detail-scroll">
        <div class="meta">
          <div class="item"><span class="k">${esc(t('detail.assignee'))}</span><span class="v" id="assignee-slot">${assigneeHtml}</span></div>
          <div class="item"><span class="k">${esc(t('meta.target'))}</span><span class="v">${fmtTime(o.targetCompletionAt)}<span class="sub">${esc(relTime(o.targetCompletionAt))}</span></span></div>
          <div class="item"><span class="k">${esc(t('meta.created'))}</span><span class="v">${fmtTime(o.createdAt)}<span class="sub">${esc(relTime(o.createdAt))}</span></span></div>
          <div class="item"><span class="k">${esc(t('meta.updated'))}</span><span class="v">${fmtTime(o.updatedAt)}<span class="sub">${esc(relTime(o.updatedAt))}</span></span></div>
          <div class="item"><span class="k">${esc(t('meta.holds'))}</span><span class="v">${holds.length ? holds.length + esc(t('meta.active')) : esc(t('meta.none'))}</span></div>
        </div>
        ${actions.length ? `<section class="detail-section"><h3>${esc(t('detail.actions'))}</h3>${actions.join('')}</section>` : ''}
        <section class="detail-section"><h3>${esc(t('detail.holds'))} ${holds.length ? `<span class="count">${holds.length}</span>` : ''}</h3>${holdsHtml}</section>
        <section class="detail-section"><h3>${esc(t('detail.qcs'))} ${qcs.length ? `<span class="count">${qcs.length}</span>` : ''}</h3>${qcHtml}</section>
        ${checklistSectionHtml()}
        ${manualSectionHtml()}
        <section class="detail-section" id="comments-section"><h3>${esc(t('detail.comments'))} ${state.comments && state.comments.length ? `<span class="count">${state.comments.length}</span>` : ''}</h3>${commentsHtml}</section>
        ${can('scan:use') ? `<section class="detail-section"><h3>${esc(t('detail.scan'))}</h3>${scanHtml}</section>` : ''}
        <section class="detail-section"><h3>${esc(t('detail.audit'))} ${state.audit.length ? `<span class="count">${state.audit.length}</span>` : ''}</h3>${auditHtml}</section>
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

