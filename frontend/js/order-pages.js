/* Full-page order workspace with tabbed pages.
   Wraps the existing detail panel — does not replace order logic. */
(function () {
  const view = document.getElementById('view-orders');
  const panel = document.getElementById('detail-panel');
  const split = document.getElementById('orders-split');
  if (!view || !panel) return;

  let building = false;
  let lastFp = '';
  let lastOrder = '';
  let pageIndex = 0;

  const isOpen = () =>
    !panel.classList.contains('hidden') && panel.childElementCount > 0;

  function sectionTitle(section) {
    const h3 = section.querySelector('h3');
    if (!h3) return 'Details';
    const clone = h3.cloneNode(true);
    clone.querySelectorAll('.count, .section-actions, button, .btn').forEach((n) => n.remove());
    return (clone.textContent || '').replace(/\s+/g, ' ').trim() || 'Details';
  }

  function fingerprint() {
    const order = panel.querySelector('.detail-head h2')?.textContent?.trim() || '';
    const titles = [...panel.querySelectorAll('.detail-section')]
      .filter((s) => !s.closest('.order-page'))
      .map(sectionTitle)
      .join('|');
    const pagedTitles = [...panel.querySelectorAll('.order-tab')].map((t) => t.textContent.trim()).join('|');
    return order + '::' + (titles || pagedTitles);
  }

  function currentOrder() {
    return panel.querySelector('.detail-head h2')?.textContent?.trim() || '';
  }

  function closeOrder() {
    const closeBtn = panel.querySelector(
      '.detail-head .icon-btn, .detail-head [aria-label*="lose" i], .detail-head [aria-label*="luit" i], [data-close], .detail-close'
    );
    if (closeBtn) {
      closeBtn.click();
      return;
    }
    panel.classList.add('hidden');
    panel.innerHTML = '';
    view.classList.remove('order-open');
    split?.classList.add('no-detail');
    document.body.classList.remove('order-workspace');
  }

  function setPage(i, { focus } = {}) {
    const pages = [...panel.querySelectorAll('.order-page')];
    const tabs = [...panel.querySelectorAll('.order-tab')];
    if (!pages.length) return;
    pageIndex = Math.max(0, Math.min(i, pages.length - 1));
    pages.forEach((p, n) => p.classList.toggle('is-active', n === pageIndex));
    tabs.forEach((t, n) => {
      t.classList.toggle('is-active', n === pageIndex);
      t.setAttribute('aria-selected', n === pageIndex ? 'true' : 'false');
      t.tabIndex = n === pageIndex ? 0 : -1;
    });
    const foot = panel.querySelector('.order-pager-foot');
    if (foot) {
      const prev = foot.querySelector('[data-order-prev]');
      const next = foot.querySelector('[data-order-next]');
      const status = foot.querySelector('[data-order-status]');
      if (prev) prev.disabled = pageIndex === 0;
      if (next) next.disabled = pageIndex === pages.length - 1;
      if (status) status.textContent = `${pageIndex + 1} / ${pages.length}`;
      foot.querySelectorAll('[data-order-dot]').forEach((d, n) => {
        d.classList.toggle('is-active', n === pageIndex);
      });
    }
    const active = pages[pageIndex];
    if (active) active.scrollTop = 0;
    if (focus) tabs[pageIndex]?.focus();
  }

  function teardown() {
    const pagesRoot = panel.querySelector('.order-pages');
    if (!pagesRoot) return;
    const scroll = panel.querySelector('.detail-scroll');
    const host = scroll || panel;
    [...pagesRoot.querySelectorAll('.order-page')].forEach((page) => {
      while (page.firstChild) host.appendChild(page.firstChild);
    });
    panel.querySelector('.order-tabs')?.remove();
    panel.querySelector('.order-pager-foot')?.remove();
    panel.querySelector('.order-back')?.remove();
    pagesRoot.remove();
    panel.classList.remove('paged');
    if (scroll) scroll.hidden = false;
  }

  function build() {
    building = true;
    try {
      teardown();

      const head = panel.querySelector('.detail-head');
      const scroll = panel.querySelector('.detail-scroll');
      const host = scroll || panel;
      const kids = [...host.children].filter(
        (el) =>
          !el.classList.contains('order-tabs') &&
          !el.classList.contains('order-pages') &&
          !el.classList.contains('order-pager-foot') &&
          !el.classList.contains('order-back') &&
          !el.classList.contains('detail-head')
      );

      const sections = kids.filter((el) => el.classList.contains('detail-section'));
      const overview = kids.filter((el) => !el.classList.contains('detail-section'));

      const pages = [];
      if (overview.length) {
        pages.push({ title: 'Overview', nodes: overview });
      }
      sections.forEach((sec) => {
        pages.push({ title: sectionTitle(sec), nodes: [sec] });
      });
      if (!pages.length) return;

      const back = document.createElement('button');
      back.type = 'button';
      back.className = 'order-back';
      back.innerHTML =
        '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6"/></svg><span>All orders</span>';
      back.addEventListener('click', closeOrder);

      const tabs = document.createElement('div');
      tabs.className = 'order-tabs';
      tabs.setAttribute('role', 'tablist');
      tabs.setAttribute('aria-label', 'Order sections');

      const pagesRoot = document.createElement('div');
      pagesRoot.className = 'order-pages';

      pages.forEach((page, i) => {
        const tab = document.createElement('button');
        tab.type = 'button';
        tab.className = 'order-tab';
        tab.setAttribute('role', 'tab');
        tab.id = 'order-tab-' + i;
        tab.setAttribute('aria-controls', 'order-page-' + i);
        tab.textContent = page.title;
        tab.addEventListener('click', () => setPage(i));
        tabs.appendChild(tab);

        const pane = document.createElement('div');
        pane.className = 'order-page';
        pane.id = 'order-page-' + i;
        pane.setAttribute('role', 'tabpanel');
        pane.setAttribute('aria-labelledby', tab.id);
        page.nodes.forEach((n) => pane.appendChild(n));
        pagesRoot.appendChild(pane);
      });

      const foot = document.createElement('div');
      foot.className = 'order-pager-foot';
      const prev = document.createElement('button');
      prev.type = 'button';
      prev.className = 'btn btn-ghost';
      prev.dataset.orderPrev = '1';
      prev.innerHTML = '<span aria-hidden="true">←</span> Previous';
      prev.addEventListener('click', () => setPage(pageIndex - 1));

      const mid = document.createElement('div');
      mid.className = 'order-pager-mid';
      const dots = document.createElement('div');
      dots.className = 'order-dots';
      dots.setAttribute('aria-hidden', 'true');
      pages.forEach((_, i) => {
        const dot = document.createElement('button');
        dot.type = 'button';
        dot.className = 'order-dot';
        dot.dataset.orderDot = '1';
        dot.addEventListener('click', () => setPage(i));
        dots.appendChild(dot);
      });
      const status = document.createElement('span');
      status.className = 'muted';
      status.dataset.orderStatus = '1';
      mid.appendChild(dots);
      mid.appendChild(status);

      const next = document.createElement('button');
      next.type = 'button';
      next.className = 'btn btn-primary';
      next.dataset.orderNext = '1';
      next.innerHTML = 'Next <span aria-hidden="true">→</span>';
      next.addEventListener('click', () => setPage(pageIndex + 1));

      foot.appendChild(prev);
      foot.appendChild(mid);
      foot.appendChild(next);

      if (head) panel.insertBefore(back, head);
      else panel.insertBefore(back, panel.firstChild);

      const afterHead = head ? head.nextSibling : back.nextSibling;
      panel.insertBefore(tabs, afterHead);
      panel.insertBefore(pagesRoot, tabs.nextSibling);
      panel.appendChild(foot);

      if (scroll) scroll.hidden = true;
      panel.classList.add('paged');

      if (pageIndex >= pages.length) pageIndex = 0;
      setPage(pageIndex);
    } finally {
      building = false;
    }
  }

  function sync() {
    if (building) return;
    const open = isOpen();
    view.classList.toggle('order-open', open);
    document.body.classList.toggle('order-workspace', open);

    if (!open) {
      lastFp = '';
      return;
    }

    const order = currentOrder();
    const fp = fingerprint();
    if (order !== lastOrder) {
      pageIndex = 0;
      lastOrder = order;
    }
    if (fp === lastFp && panel.classList.contains('paged')) return;
    lastFp = fp;
    build();
    lastFp = fingerprint();
  }

  const obs = new MutationObserver(() => {
    if (building) return;
    sync();
  });
  obs.observe(panel, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ['class'],
  });

  document.addEventListener('keydown', (e) => {
    if (!isOpen() || !view.classList.contains('order-open')) return;
    if (document.getElementById('palette-root') && !document.getElementById('palette-root').classList.contains('hidden')) return;
    if (document.getElementById('modal-root') && !document.getElementById('modal-root').classList.contains('hidden')) return;
    const tag = (e.target && e.target.tagName) || '';
    if (/^(INPUT|TEXTAREA|SELECT)$/.test(tag) || e.target?.isContentEditable) return;

    if (e.key === 'ArrowRight' || e.key === 'PageDown') {
      e.preventDefault();
      setPage(pageIndex + 1);
    } else if (e.key === 'ArrowLeft' || e.key === 'PageUp') {
      e.preventDefault();
      setPage(pageIndex - 1);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      closeOrder();
    }
  });

  sync();
})();
