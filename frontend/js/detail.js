'use strict';

  

  function closeDetail() {
    state.selectedId = null;
    state.detail = null;
    state.audit = [];
    state.checklist = [];
    state.orderManuals = [];
    stopChecklistPolling();
    $('#detail-panel').classList.add('hidden');
    $('#orders-split').classList.add('no-detail');
    syncURL(); 
    renderTable();
  }

  
  
  function drawerInputActive() {
    const a = document.activeElement;
    return !!(a && $('#detail-panel').contains(a)
      && (/^(INPUT|SELECT|TEXTAREA)$/.test(a.tagName) || a.isContentEditable));
  }

  async function openDetail(id) {
    state.selectedId = id;
    state.comments = [];
    syncURL();
    renderTable();
    renderDetail();
    try {
      const [d, a, c] = await Promise.all([
        api('GET', '/api/v1/orders/' + id),
        api('GET', '/api/v1/orders/' + id + '/audit'),
        api('GET', '/api/v1/orders/' + id + '/checklist'),
      ]);
      state.detail = d.order;
      state.audit = a.auditLogs || [];
      state.checklist = c.items || [];
      if (!drawerInputActive()) renderDetail();
      renderTable();
      loadComments(id);
      loadOrderManuals(id, true);
      startChecklistPolling(id);
    } catch (err) {
      if (err && err.status === 404) {
        toast('That order no longer exists', true);
        closeDetail();
        return;
      }
      toast(err.message, true);
    }
  }

  function commentsListHtml(list) {
    if (!list.length) return '<p class="empty-inline">No comments yet.</p>';
    return list.map(renderCommentHtml).join('');
  }

  function renderCommentHtml(c) {
    return `
      <div class="list-item comment-item${c.user === state.user.username ? ' mine' : ''}">
        <div>
          <div class="reason">${esc(c.body)}</div>
          <div class="meta-line">${esc(c.user)} · ${fmtTime(c.createdAt)}</div>
          ${(c.mentions || []).length ? `<div class="meta-line mentions">→ ${c.mentions.map((m) => '@' + esc(m)).join(' ')}</div>` : ''}
        </div>
      </div>`;
  }

  async function loadComments(id) {
    try {
      const d = await api('GET', `/api/v1/orders/${id}/comments`);
      state.comments = d.comments || [];
      const box = $('#comments-container');
      if (box) box.innerHTML = commentsListHtml(state.comments);
      const h = $('#comments-section .count');
      if (h) h.textContent = state.comments.length;
      if (!state.comments.length) {
        const sec = $('#comments-section');
        if (sec) {
          const empty = sec.querySelector('.empty-inline');
          if (empty) empty.textContent = 'No comments yet.';
        }
      }
    } catch (err) {
      toast(err.message, true);
    }
  }

  let checklistTimer = null;
  function startChecklistPolling(id) {
    stopChecklistPolling();
    checklistTimer = setInterval(async () => {
      if (!state.token || state.selectedId !== id) return stopChecklistPolling();
      await loadChecklist(id, true);
    }, 5000);
  }
  function stopChecklistPolling() {
    if (checklistTimer) { clearInterval(checklistTimer); checklistTimer = null; }
  }

  function checklistItemsHtml(items, emptyMsg) {
    if (!items.length) return `<p class="empty-inline">${esc(emptyMsg || 'No pick lines.')}</p>`;
    return `<div class="checklist-list">${items.map((it) => {
      const done = !!it.checkedBy;
      const mine = done && it.checkedBy === state.user.username;
      return `
        <div class="checklist-item ${done ? 'done' : ''}${mine ? ' mine' : ''}" data-item="${it.id}">
          <div class="checklist-main">
            <span class="checklist-loc">${esc(it.locatie || '—')}</span>
            <span class="checklist-art">${esc(it.artikel)}</span>
            <span class="checklist-desc">${esc(it.omschrijving || '')}</span>
            <span class="checklist-qty">${it.aantal > 0 ? '×' + it.aantal : ''}</span>
          </div>
          <div class="checklist-side">
            ${done
              ? `<span class="checklist-by">✓ ${esc(it.checkedBy)}<span class="muted"> · ${fmtTime(it.checkedAt)}</span></span>`
              : '<span class="checklist-by muted">open</span>'}
            ${can('pick:use') ? `<button type="button" class="btn btn-secondary btn-sm" data-act="checklist-toggle" data-id="${it.id}" data-action="${done ? 'untick' : 'tick'}">${done ? 'Undo' : 'Tick off'}</button>` : ''}
          </div>
        </div>`;}).join('')}</div>`;
  }

  function checklistSectionHtml() {
    const items = state.checklist || [];
    const doneCount = items.filter((it) => it.checkedBy).length;
    const emptyMsg = can('pick:use')
      ? 'No pick checklist yet — upload het dagelijkse verkoopregels-Excel onder Configuration om er een te genereren.'
      : 'No pick checklist yet.';
    return `<section class="detail-section" id="checklist-section"><h3>Pick checklist ${items.length ? `<span class="count">${doneCount}/${items.length}</span>` : ''}</h3>${checklistItemsHtml(items, emptyMsg)}</section>`;
  }

  
  
  async function doChecklistToggle(btn) {
    const action = btn.dataset.action === 'untick' ? 'untick' : 'tick';
    const id = Number(btn.dataset.id);
    if (!id) return false;
    btn.disabled = true;
    try {
      await api('POST', `/api/v1/checklist/${id}/${action}`);
      
      
      toast(
        action === 'untick' ? t('toast.unticked') : t('toast.ticked'),
        false,
        {
          label: t('toast.undo'),
          fn: () => {
            doChecklistToggle({ dataset: { id: String(id), action: action === 'tick' ? 'untick' : 'tick' } })
              .then(async (ok) => {
                if (!ok) return;
                if (state.selectedId) await loadChecklist(state.selectedId, true);
                if (location.hash.startsWith('#/checklists')) await loadChecklistsView(true);
              });
          },
        },
      );
      return true;
    } catch (err) {
      toast(err.message, true);
      btn.disabled = false;
      return false;
    }
  }

  async function loadChecklist(id, silent) {
    if (!id) return;
    try {
      const c = await api('GET', `/api/v1/orders/${id}/checklist`);
      if (state.selectedId !== id) return;
      state.checklist = c.items || [];
      
      
      const sec = $('#checklist-section');
      if (sec) sec.outerHTML = checklistSectionHtml();
      else renderDetail();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

