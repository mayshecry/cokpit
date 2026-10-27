'use strict';

const Automations = {
  rules: [],
  runs: [],
  bound: false,
  loadSeq: 0,
  // Directory caches for the pickers (people, teams, customers, products).
  dir: { users: [], teams: [], customers: [], products: [], loadedAt: 0 },
  // Row to highlight after a save, so the user sees what they just created.
  recentId: 0,

  triggerLabels: {
    QC_FAILED: 'QC failure',
    SLA_BREACHED: 'SLA breached',
    HOLD_AGING: 'Aging hold',
  },
  actionLabels: {
    NOTIFY: 'Notify people',
    ASSIGN: 'Assign the order',
    ESCALATE: 'Raise escalation',
  },
  actionHints: {
    NOTIFY: 'Everyone you pick is notified. Pick as many people and teams as you like — a team means all of its members.',
    ASSIGN: 'The order goes to one person, or to one team. With a team, the member with the fewest open orders gets it.',
    ESCALATE: 'Raises an escalation. Optionally give it one owner (a person or a team).',
  },

  // Ready-made starting points. They only fill the dialog in; nothing is saved
  // until you press the create button.
  templates: [
    {
      id: 'sla-team', title: 'Late order → team', hint: 'Assign SLA breaches to the least busy member',
      apply: (dir) => ({
        name: 'SLA breach → team', trigger: 'SLA_BREACHED', action: 'ASSIGN',
        teams: dir.teams.length ? [dir.teams[0].value] : [], targets: [],
      }),
    },
    {
      id: 'qc-escalate', title: 'QC fail → escalate', hint: 'Raise L2 and tell the owner',
      apply: () => ({ name: 'QC failure → escalation', trigger: 'QC_FAILED', action: 'ESCALATE', escalationLevel: 'L2', targets: [], teams: [] }),
    },
    {
      id: 'hold-nudge', title: 'Aging hold → nudge', hint: 'Notify after 4 hours on hold',
      apply: (dir, me) => ({ name: 'Aging hold → nudge', trigger: 'HOLD_AGING', thresholdMinutes: 240, action: 'NOTIFY', targets: me ? [me] : [], teams: [] }),
    },
    {
      id: 'customer-watch', title: 'One customer watchdog', hint: 'Only that customer, routed to one person',
      apply: (dir, me) => ({
        name: 'Customer watchdog', trigger: 'SLA_BREACHED', action: 'NOTIFY',
        targets: me ? [me] : [], teams: [],
        customers: dir.customers.length ? [dir.customers[0].value] : [],
      }),
    },
  ],

  bind() {
    if (Automations.bound) return;
    Automations.bound = true;
    $('#automation-new').addEventListener('click', () => Automations.edit());
    $('#automation-evaluate').addEventListener('click', () => Automations.evaluate());
    $('#automation-refresh').addEventListener('click', () => Automations.load());
    $('#automation-table tbody').addEventListener('click', (e) => {
      if (e.target.closest('[data-empty-new]')) {
        Automations.edit();
        return;
      }
      const btn = e.target.closest('button[data-rule-id]');
      if (!btn) return;
      const id = Number(btn.dataset.ruleId);
      const rule = Automations.rules.find((r) => r.id === id);
      if (!rule) return;
      if (btn.dataset.ruleAction === 'edit') Automations.edit(rule);
      else if (btn.dataset.ruleAction === 'toggle') Automations.toggle(rule);
      else if (btn.dataset.ruleAction === 'duplicate') Automations.duplicate(rule);
      else if (btn.dataset.ruleAction === 'delete') Automations.remove(rule);
    });
  },

  async load() {
    Automations.bind();
    const seq = ++Automations.loadSeq;
    try {
      const [rules, runs] = await Promise.all([
        api('GET', '/api/v1/automations'),
        api('GET', '/api/v1/automation-runs?limit=100'),
      ]);
      if (seq !== Automations.loadSeq) return;
      Automations.rules = rules.rules || [];
      Automations.runs = runs.runs || [];
      Automations.render();
    } catch (err) {
      if (seq !== Automations.loadSeq) return;
      toast(err.message, true);
    }
  },

  // loadDirectory fills the pickers. Everything is optional: a missing endpoint
  // (permissions) just leaves that picker empty instead of breaking the dialog.
  async loadDirectory(force) {
    if (!force && Automations.dir.loadedAt && Date.now() - Automations.dir.loadedAt < 60000) return Automations.dir;
    Automations.dir = { users: [], teams: [], customers: [], products: [], loadedAt: 0 };
    if (!state.userBriefs || !state.userBriefs.length) {
      try { await loadUserBriefs(); } catch {}
    }
    Automations.dir.users = (state.userBriefs || []).map((u) => ({
      value: u.username,
      label: u.displayName && u.displayName !== u.username ? `${u.displayName} (@${u.username})` : `@${u.username}`,
      hint: u.role || '',
    }));
    const settle = await Promise.allSettled([
      api('GET', '/api/v1/departments'),
      api('GET', '/api/v1/customers'),
      api('GET', '/api/v1/products'),
    ]);
    const [teams, customers, products] = settle.map((r) => (r.status === 'fulfilled' ? r.value : null));
    Automations.dir.teams = ((teams && teams.departments) || []).map((d) => ({
      value: d.name, label: d.name, hint: d.description || 'team',
    }));
    const seen = new Set();
    Automations.dir.customers = ((customers && customers.customers) || [])
      .filter((c) => {
        const key = String(c.name || '').toLowerCase();
        if (!key || seen.has(key)) return false;
        seen.add(key);
        return true;
      })
      .map((c) => ({ value: c.name, label: c.name, hint: c.number ? `#${c.number}` : '' }));
    Automations.dir.products = ((products && products.products) || []).map((p) => ({
      value: p.code, label: `${p.code} — ${p.name}`, hint: p.department || '',
    }));
    Automations.dir.loadedAt = Date.now();
    return Automations.dir;
  },

  userLabel(username) {
    const found = (Automations.dir.users || []).find((u) => u.value === username);
    return found ? found.label : '@' + username;
  },

  // ---------------------------------------------------------------------
  // List
  // ---------------------------------------------------------------------
  recipientsHtml(rule) {
    const people = (rule.targets || []).map((u) => `<span class="recip recip-user">@${esc(u)}</span>`);
    const teams = (rule.teams || []).map((t) => `<span class="recip recip-team">${Automations.teamIcon()}${esc(t)}</span>`);
    let base = people.concat(teams).join('') || '<span class="muted">nobody</span>';
    if (rule.action === 'ESCALATE') {
      base = `<span class="recip recip-level">${esc(rule.escalationLevel || 'L1')}</span>${base}`;
    }
    const routing = [];
    Object.entries(rule.customerTargets || {}).forEach(([c, list]) => {
      routing.push(`<span class="recip-routing"><span class="muted">${esc(c)}</span> → ${(list || []).map((u) => '@' + esc(u)).join(', ')}</span>`);
    });
    Object.entries(rule.customerTeams || {}).forEach(([c, list]) => {
      routing.push(`<span class="recip-routing"><span class="muted">${esc(c)}</span> → ${(list || []).map(esc).join(', ')}</span>`);
    });
    return `<div class="recip-list">${base}</div>${routing.join('')}`;
  },

  teamIcon() {
    return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13A4 4 0 0 1 16 11"/></svg>';
  },

  scopeTags(rule) {
    const tags = [];
    const customers = rule.customers || [];
    const excluded = rule.excludedCustomers || [];
    if (customers.length) {
      customers.forEach((c) => tags.push(`<span class="tag scope-in">${esc(c)}</span>`));
    } else if (!excluded.length) {
      tags.push('<span class="tag scope-all">every customer</span>');
    }
    excluded.forEach((c) => tags.push(`<span class="tag exclude">not ${esc(c)}</span>`));
    (rule.departments || []).forEach((d) => tags.push(`<span class="tag scope-dept">${esc(d)}</span>`));
    (rule.products || []).forEach((p) => tags.push(`<span class="tag scope-prod">${esc(p)}</span>`));
    return tags.join('');
  },

  render() {
    const enabled = Automations.rules.filter((r) => r.enabled).length;
    const failed = Automations.runs.filter((r) => r.status === 'FAILED').length;
    const successful = Automations.runs.filter((r) => r.status === 'SUCCESS').length;
    const scoped = Automations.rules.filter((r) => (r.customers || []).length || (r.excludedCustomers || []).length
      || (r.departments || []).length || (r.products || []).length || (r.customerTargets && Object.keys(r.customerTargets).length)).length;
    const card = (value, cls, label, ico) => `<div class="stat ${cls}"><span class="num">${ico ? `<span class="stat-ico">${ico}</span>` : ''}${value}</span><span class="lbl">${label}</span></div>`;
    $('#automation-stats').innerHTML = [
      card(Automations.rules.length, 'accent', 'Rules total'),
      card(enabled, 'good', 'Armed'),
      card(scoped, 'info', 'Customer-scoped'),
      card(successful, 'info', 'Recent successes'),
      card(failed, failed ? 'bad' : 'good', 'Recent failures'),
    ].join('');
    $('#automation-rule-count').textContent = `${Automations.rules.length} rule${Automations.rules.length === 1 ? '' : 's'}`;

    const rows = Automations.rules.map((r) => `<tr class="rule-row rule-row-${r.action.toLowerCase()}${r.enabled ? '' : ' is-off'}${Automations.recentId === r.id ? ' is-new' : ''}" data-rule-row="${r.id}">
      <td class="mono num">${r.id}</td>
      <td>
        <strong class="rule-name">${esc(r.name)}</strong>
        ${r.message ? `<br><span class="muted rule-message">${esc(r.message)}</span>` : ''}
      </td>
      <td><span class="trigger-chip trigger-${r.trigger.toLowerCase()}">${esc(Automations.triggerLabels[r.trigger] || r.trigger)}</span></td>
      <td class="muted">${r.trigger === 'HOLD_AGING' ? `older than ${r.thresholdMinutes} min` : 'on the event'}</td>
      <td><span class="badge action-${r.action.toLowerCase()}">${esc(Automations.actionLabels[r.action] || r.action)}</span></td>
      <td>${Automations.recipientsHtml(r)}</td>
      <td><div class="automation-scope">${Automations.scopeTags(r)}</div></td>
      <td>
        <button type="button" class="switch" role="switch" aria-checked="${r.enabled}" aria-label="${r.enabled ? 'Disable' : 'Enable'} ${esc(r.name)}" data-rule-action="toggle" data-rule-id="${r.id}"><span class="switch-knob"></span></button>
      </td>
      <td class="actions"><div class="row-actions">
        <button type="button" class="btn btn-ghost btn-sm" data-rule-action="edit" data-rule-id="${r.id}">Edit</button>
        <button type="button" class="btn btn-ghost btn-sm" data-rule-action="duplicate" data-rule-id="${r.id}">Copy</button>
        <button type="button" class="btn btn-ghost btn-sm danger-ghost" data-rule-action="delete" data-rule-id="${r.id}">Delete</button>
      </div></td>
    </tr>`).join('');

    $('#automation-table tbody').innerHTML = rows || `<tr><td colspan="9">
      <div class="empty rule-empty-state">
        <div class="empty-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2 3 14h9l-1 8 10-12h-9l1-8Z"/></svg></div>
        <h3>No rules yet</h3>
        <p>An automation reacts once to a QC failure, an SLA breach or a hold that sits too long — notifying people, assigning the order or escalating it. You can narrow a rule to specific customers, departments or products.</p>
        <button type="button" class="btn btn-primary" data-empty-new>Create your first rule</button>
      </div>
    </td></tr>`;

    const history = Automations.runs.map((r) => `<tr>
      <td class="mono num">${r.id}</td>
      <td class="time">${esc(fmtCell(r.createdAt))}</td>
      <td>${esc(r.ruleName)}</td>
      <td><a class="mono" href="#/orders/${r.orderId}">${esc(r.orderNumber)}</a></td>
      <td>${esc(Automations.actionLabels[r.action] || r.action)}</td>
      <td><span class="run-status run-${(r.status || '').toLowerCase()}"><span class="run-dot"></span>${esc(r.status)}</span></td>
      <td class="automation-run-detail">${esc(r.error || r.detail || '—')}<br><span class="muted mono">${esc(r.triggerKey)}</span></td>
    </tr>`).join('');
    $('#automation-runs-table tbody').innerHTML = history || '<tr><td colspan="7"><div class="empty-inline">Nothing has run yet. Actions appear here as soon as a rule fires.</div></td></tr>';
  },

  // ---------------------------------------------------------------------
  // Editor
  // ---------------------------------------------------------------------
  async edit(rule) {
    const dir = await Automations.loadDirectory();
    const me = (state.user || {}).username;
    const current = rule || {
      name: '', trigger: 'QC_FAILED', action: 'NOTIFY', thresholdMinutes: 240,
      targets: me ? [me] : [], teams: [], message: '', escalationLevel: 'L1', enabled: true,
      customers: [], excludedCustomers: [], departments: [], products: [],
      customerTargets: {}, customerTeams: {},
    };

    const selectHtml = (id, name, options, value) => `<select id="${id}" name="${name}">${
      options.map((o) => `<option value="${esc(o.value)}"${o.value === value ? ' selected' : ''}>${esc(o.label)}</option>`).join('')
    }</select>`;

    const chipField = (id, label, hint, options, values, placeholder) => `
      <div class="field">
        <label for="${id}">${label}</label>
        ${ChipsPicker.html({ id, options, value: values, placeholder })}
        <span class="field-hint">${hint}</span>
      </div>`;

    const editor = `
      <div class="rule-editor" id="rule-editor">
        <div class="rule-sentence" id="rule-sentence" aria-live="polite"></div>

        ${rule ? '' : `<div class="rule-templates" id="rule-templates">
          <span class="rule-templates-label">Start from</span>
          ${Automations.templates.map((t) => `<button type="button" class="rule-tpl" data-template="${t.id}">
            <strong>${esc(t.title)}</strong><span>${esc(t.hint)}</span>
          </button>`).join('')}
        </div>`}

        <section class="rule-step">
          <header class="rule-step-head">
            <span class="rule-step-num">1</span>
            <div><h3>When should it fire?</h3><p>One rule reacts to one kind of event.</p></div>
          </header>
          <div class="field-grid">
            <div class="field">
              <label for="mf-name">Rule name</label>
              <input id="mf-name" name="name" type="text" value="${esc(current.name)}" placeholder="e.g. Late Kenter orders → Warehouse" autocomplete="off" spellcheck="false">
            </div>
            <div class="field">
              <label for="mf-trigger">Trigger</label>
              ${selectHtml('mf-trigger', 'trigger', Object.entries(Automations.triggerLabels).map(([value, label]) => ({ value, label })), current.trigger)}
            </div>
            <div class="field" id="field-threshold">
              <label for="mf-thresholdMinutes">Hold older than (minutes)</label>
              <input id="mf-thresholdMinutes" name="thresholdMinutes" type="number" min="1" max="525600" value="${esc(String(current.thresholdMinutes || 240))}">
            </div>
          </div>
        </section>

        <section class="rule-step">
          <header class="rule-step-head">
            <span class="rule-step-num">2</span>
            <div><h3>What should happen?</h3><p>Pick one action — create a second rule for a second action.</p></div>
          </header>
          <div class="field">
            <label for="mf-action">Action</label>
            ${selectHtml('mf-action', 'action', Object.entries(Automations.actionLabels).map(([value, label]) => ({ value, label })), current.action)}
            <span class="field-hint" id="action-hint">${esc(Automations.actionHints[current.action] || '')}</span>
          </div>
        </section>

        <section class="rule-step">
          <header class="rule-step-head">
            <span class="rule-step-num">3</span>
            <div><h3>Who is it for?</h3><p>People are individuals; a team expands to its members.</p></div>
          </header>
          <div class="field-grid">
            ${chipField('mf-targets', 'People', 'Everyone you add is notified or gets the order.', dir.users, current.targets || [], 'Search people…')}
            ${chipField('mf-teams', 'Teams', 'A team can be notified as a whole, or take the order as a group.', dir.teams, current.teams || [], 'Search teams…')}
          </div>
          <div class="field" id="field-escalation">
            <label for="mf-escalationLevel">Escalation level</label>
            ${selectHtml('mf-escalationLevel', 'escalationLevel', [
              { value: 'L1', label: 'L1 — team lead' },
              { value: 'L2', label: 'L2 — operations' },
              { value: 'L3', label: 'L3 — process owner' },
            ], current.escalationLevel || 'L1')}
          </div>
        </section>

        <section class="rule-step">
          <header class="rule-step-head">
            <span class="rule-step-num">4</span>
            <div><h3>Which orders?</h3><p>Leave everything empty to apply to every order.</p></div>
          </header>
          <div class="field-grid">
            ${chipField('mf-customers', 'Only these customers', 'Empty means every customer.', dir.customers, current.customers || [], 'Search customers…')}
            ${chipField('mf-excludedCustomers', 'Except these customers', 'Exclusions always win over inclusions.', dir.customers, current.excludedCustomers || [], 'Search customers…')}
            ${chipField('mf-departments', 'Only these departments', 'Matched on the department of the order\u2019s product.', dir.teams, current.departments || [], 'Search departments…')}
            ${chipField('mf-products', 'Only these products', 'Product codes on the order.', dir.products, current.products || [], 'Search products…')}
          </div>
        </section>

        <section class="rule-step">
          <header class="rule-step-head">
            <span class="rule-step-num">5</span>
            <div><h3>Different people per customer</h3><p>Optional. Replaces everyone above for that customer.</p></div>
          </header>
          <div class="rule-rows" id="rule-routing-rows">${Automations.routingRows(current)}</div>
          <div><button type="button" class="btn btn-secondary btn-sm" id="rule-add-routing">＋ Add a customer</button></div>
        </section>

        <section class="rule-step rule-step-compact">
          <div class="field">
            <label for="mf-message">Note on every notification</label>
            <textarea id="mf-message" name="message" rows="2" placeholder="e.g. Please pick this up before the 16:00 courier.">${esc(current.message || '')}</textarea>
          </div>
          <label class="switch-row" for="mf-enabled">
            <input type="checkbox" id="mf-enabled" name="enabled" class="switch-input"${current.enabled ? ' checked' : ''}>
            <span class="switch" aria-hidden="true"><span class="switch-knob"></span></span>
            <span class="switch-text"><strong>Rule enabled</strong><small>Runs on the next matching event. Turn it off to keep it without firing.</small></span>
          </label>
        </section>

        <section class="rule-step rule-step-preview">
          <header class="rule-step-head">
            <span class="rule-step-num rule-step-num-ghost">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>
            </span>
            <div><h3>Try it before you save</h3><p>Runs against the orders that exist right now. Nothing is sent.</p></div>
          </header>
          <div><button type="button" class="btn btn-secondary btn-sm" id="rule-preview-run">Check against current orders</button></div>
          <div id="rule-preview" class="rule-preview hidden"></div>
        </section>
      </div>`;

    const res = await modal({
      title: rule ? `Edit rule ${rule.id}` : 'New automation rule',
      description: rule ? '' : 'Describe what should happen once, and it happens on every matching order.',
      confirmLabel: rule ? 'Save rule' : 'Create rule',
      wide: true,
      fields: [],
      bodyHtml: editor,
      onMount: (form, root) => Automations.bindEditor(root, form, current, dir),
      collect: (data, form, root) => Automations.draftRule(form, root),
      validate: (data, form, root) => Automations.validateDraft(data, root),
    });
    if (!res) return;
    Automations.recentId = rule ? rule.id : 0;
    try {
      const saved = await api('POST', rule ? `/api/v1/automations/${rule.id}` : '/api/v1/automations', res);
      if (!rule && saved && saved.rule && saved.rule.id) Automations.recentId = saved.rule.id;
      toast(rule ? 'Rule saved' : 'Rule created — it is armed');
      await Automations.load();
    } catch (err) {
      toast(err.message, true);
    }
  },

  bindEditor(root, form, current, dir) {
    ['targets', 'teams', 'customers', 'excludedCustomers', 'departments', 'products'].forEach((name) => {
      const wrap = $('#mf-' + name, root);
      if (!wrap) return;
      ChipsPicker.bind(wrap, {
        onChange: (values) => {
          if (name === 'customers' || name === 'excludedCustomers') {
            // An overlap is rejected by the server; resolve it here so the user
            // never hits that wall.
            const other = name === 'customers' ? 'excludedCustomers' : 'customers';
            const otherWrap = $('#mf-' + other, root);
            const overlaps = values.filter((v) => ChipsPicker.values(otherWrap)
              .some((o) => o.toLowerCase() === v.toLowerCase()));
            if (overlaps.length) {
              ChipsPicker.set(otherWrap, ChipsPicker.values(otherWrap)
                .filter((o) => !overlaps.some((v) => v.toLowerCase() === o.toLowerCase())));
              toast(`${overlaps.join(', ')} moved out of “${other === 'customers' ? 'Only these' : 'Except these'} customers” — one list only.`);
            }
          }
          Automations.updateSummary(root, form);
        },
      });
    });
    Automations.bindRoutingRows(root, current, dir);
    $('#rule-add-routing', root).addEventListener('click', () => {
      Automations.addRoutingRow(root, current, dir);
      Automations.refreshRoutingCustomers(root);
      Automations.updateSummary(root, form);
    });

    form.addEventListener('input', () => Automations.updateSummary(root, form));
    form.addEventListener('change', () => { Automations.updateSummary(root, form); });
    root.addEventListener('chipchange', () => Automations.updateSummary(root, form));

    $('#rule-preview-run', root).addEventListener('click', async () => {
      const panel = $('#rule-preview', root);
      const btn = $('#rule-preview-run', root);
      btn.disabled = true;
      panel.classList.remove('hidden');
      panel.innerHTML = '<div class="rule-preview-loading"><span class="spinner"></span>Checking the live orders…</div>';
      try {
        const out = await api('POST', '/api/v1/automations/preview', Automations.draftRule(form, root));
        panel.innerHTML = Automations.renderPreview(out);
      } catch (err) {
        panel.innerHTML = `<div class="rule-preview-empty rule-warn">${esc(err.message)}</div>`;
      } finally {
        btn.disabled = false;
      }
    });

    // Cmd/Ctrl+Enter saves, like the rest of the app.
    form.addEventListener('keydown', (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
        e.preventDefault();
        form.requestSubmit();
      }
    });

    Automations.refreshRoutingCustomers(root);
    Automations.syncActionUi(root, form, current);
    form.elements.action.addEventListener('change', () => Automations.syncActionUi(root, form, current));
    form.elements.trigger.addEventListener('change', () => Automations.syncActionUi(root, form, current));

    if (!current.id) {
      $$('[data-template]', root).forEach((btn) => {
        btn.addEventListener('click', () => Automations.applyTemplate(root, form, btn.dataset.template));
      });
    }
    Automations.updateSummary(root, form);
    const nameInput = $('#mf-name', root);
    if (nameInput) nameInput.focus();
  },

  applyTemplate(root, form, id) {
    const tpl = Automations.templates.find((t) => t.id === id);
    if (!tpl) return;
    const me = (state.user || {}).username;
    const values = tpl.apply(Automations.dir, me);
    form.elements.name.value = values.name || '';
    if (values.trigger) form.elements.trigger.value = values.trigger;
    if (values.action) form.elements.action.value = values.action;
    if (values.thresholdMinutes) form.elements.thresholdMinutes.value = String(values.thresholdMinutes);
    if (values.escalationLevel) form.elements.escalationLevel.value = values.escalationLevel;
    ['targets', 'teams', 'customers', 'excludedCustomers'].forEach((key) => {
      if (!(key in values)) return;
      const wrap = $('#mf-' + key, root);
      if (wrap) ChipsPicker.set(wrap, values[key] || []);
    });
    $$('[data-template]', root).forEach((b) => b.classList.toggle('is-active', b.dataset.template === id));
    Automations.syncActionUi(root, form, { ...values });
    Automations.updateSummary(root, form);
    const nameInput = $('#mf-name', root);
    if (nameInput) nameInput.focus();
    toast(`Template applied — adjust it and save.`);
  },

  // The live sentence: the whole rule in one readable line.
  updateSummary(root, form) {
    const el = $('#rule-sentence', root);
    if (!el || !form || !form.elements.action) return;
    const draft = Automations.draftRule(form, root);
    const problems = [];
    if (!draft.name) problems.push('give the rule a name');
    const total = draft.targets.length + draft.teams.length;
    if (draft.action === 'NOTIFY' && total === 0) problems.push('pick at least one person or team');
    if (draft.action === 'ASSIGN' && total !== 1) problems.push('pick exactly one person or team');
    if (draft.action === 'ESCALATE' && total > 1) problems.push('pick at most one owner');
    const when = {
      QC_FAILED: 'a QC check fails',
      SLA_BREACHED: 'an order breaches its SLA',
      HOLD_AGING: `a hold has been open for more than ${draft.thresholdMinutes} min`,
    }[draft.trigger] || 'an event happens';
    const who = [
      ...draft.targets.map((u) => `<b>${esc(Automations.userLabel(u))}</b>`),
      ...draft.teams.map((t) => `<b class="rs-team">${Automations.teamIcon()}${esc(t)}</b>`),
    ];
    const verb = draft.action === 'ASSIGN' ? 'hand the order to' : draft.action === 'ESCALATE'
      ? `escalate at ${esc(draft.escalationLevel)} to` : 'notify';
    const whoHtml = who.length ? who.join(' <i>and</i> ') : '<span class="rs-missing">nobody yet</span>';
    const scope = [];
    if (draft.customers.length) scope.push(`for ${draft.customers.map((c) => `<b>${esc(c)}</b>`).join(', ')}`);
    else scope.push('for every customer');
    if (draft.excludedCustomers.length) scope.push(`except ${draft.excludedCustomers.map((c) => `<b>${esc(c)}</b>`).join(', ')}`);
    if (draft.departments.length) scope.push(`in ${draft.departments.map((d) => `<b>${esc(d)}</b>`).join(', ')}`);
    if (draft.products.length) scope.push(`for ${draft.products.map((p) => `<b>${esc(p)}</b>`).join(', ')}`);
    const routingKeys = Object.keys(draft.customerTargets || {}).concat(Object.keys(draft.customerTeams || {}));
    const extra = routingKeys.length ? `<span class="rs-note">· ${routingKeys.length} customer override${routingKeys.length === 1 ? '' : 's'}</span>` : '';
    el.innerHTML = `<span class="rs-kw">When</span> ${esc(when)} <span class="rs-scope">${scope.join(' ')}</span>
      <span class="rs-arrow">→</span> <span class="rs-kw">${verb}</span> ${whoHtml} ${extra}
      <span class="rs-state ${problems.length ? 'is-draft' : 'is-ready'}">${problems.length ? '· ' + esc(problems[0]) : '· ready to save'}</span>`;
    el.classList.toggle('is-ready', problems.length === 0);
  },

  // syncActionUi keeps the recipient pickers honest while the dialog is open.
  syncActionUi(root, form, current) {
    const action = form.elements.action.value;
    const trigger = form.elements.trigger.value;
    const limit = action === 'ASSIGN' || action === 'ESCALATE' ? 1 : 0;
    const countOf = (name) => ChipsPicker.values($('#mf-' + name, root)).length;
    ['targets', 'teams'].forEach((name) => {
      const wrap = $('#mf-' + name, root);
      if (wrap) {
        wrap.dataset.max = String(limit);
        wrap._canAdd = () => limit === 0 || countOf('targets') + countOf('teams') < limit;
      }
      if (wrap && wrap._chipRefresh) wrap._chipRefresh();
    });
    const hint = $('#action-hint', root);
    if (hint) hint.textContent = Automations.actionHints[action] || '';
    const levelField = $('#field-escalation', root);
    if (levelField) levelField.classList.toggle('hidden', action !== 'ESCALATE');
    const thresholdField = $('#field-threshold', root);
    if (thresholdField) thresholdField.classList.toggle('hidden', trigger !== 'HOLD_AGING');

    // Never drop a selection behind the user's back — explain what no longer fits.
    const total = countOf('targets') + countOf('teams');
    const err = $('#modal-error', root);
    if (err) {
      if (limit > 0 && total > limit) {
        err.textContent = action === 'ASSIGN'
          ? `ASSIGN hands the order to one person or team, but ${total} are selected. Remove ${total - limit} — or switch the action to “Notify people”.`
          : 'An escalation can have at most one owner. Remove the extra people or teams.';
        err.classList.remove('hidden');
      } else if ((err.textContent || '').includes('ASSIGN hands') || (err.textContent || '').includes('at most one owner')) {
        err.textContent = '';
        err.classList.add('hidden');
      }
    }
    $$('[data-routing-row]', root).forEach((row) => {
      const peopleWrap = $('[data-row-people] .chip-pick', row);
      const teamsWrap = $('[data-row-teams] .chip-pick', row);
      const rowCount = () => ChipsPicker.values(peopleWrap).length + ChipsPicker.values(teamsWrap).length;
      [peopleWrap, teamsWrap].forEach((wrap) => {
        if (!wrap) return;
        wrap.dataset.max = String(limit);
        wrap._canAdd = () => limit === 0 || rowCount() < limit;
        if (wrap._chipRefresh) wrap._chipRefresh();
      });
    });
    if (current) current.action = action;
  },

  routingRows(current) {
    const dir = Automations.dir;
    const names = Array.from(new Set([
      ...Object.keys(current.customerTargets || {}),
      ...Object.keys(current.customerTeams || {}),
    ]));
    if (!names.length) return '<div class="rule-empty" data-routing-empty>No overrides — everyone above is used for every matching order.</div>';
    return names.map((name) => Automations.routingRowHtml(name,
      (current.customerTargets || {})[name] || [],
      (current.customerTeams || {})[name] || [], dir)).join('');
  },

  routingRowHtml(customer, people, teams, dir) {
    const options = (dir.customers || []).map((c) => `<option value="${esc(c.value)}"${c.value === customer ? ' selected' : ''}>${esc(c.label)}</option>`).join('');
    const known = (dir.customers || []).some((c) => c.value === customer);
    return `<div class="rule-row" data-routing-row>
      <div class="rule-row-cust">
        <label>Customer</label>
        <select data-row-customer>
          <option value="">Pick a customer…</option>${options}${customer && !known ? `<option value="${esc(customer)}" selected>${esc(customer)}</option>` : ''}
        </select>
        <span class="muted">replaces the recipients above</span>
      </div>
      <div>
        <label>People</label>
        <div data-row-people>${ChipsPicker.html({ id: 'row-people-' + Math.random().toString(36).slice(2), options: dir.users, value: people, placeholder: 'Search people…' })}</div>
      </div>
      <div>
        <label>Teams</label>
        <div data-row-teams>${ChipsPicker.html({ id: 'row-teams-' + Math.random().toString(36).slice(2), options: dir.teams, value: teams, placeholder: 'Search teams…' })}</div>
      </div>
      <button type="button" class="btn btn-ghost btn-sm danger-ghost rule-row-remove" data-row-remove aria-label="Remove this customer override">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
      </button>
    </div>`;
  },

  bindRoutingRows(root, current, dir) {
    $$('[data-routing-row]', root).forEach((row) => {
      const peopleWrap = $('[data-row-people] .chip-pick', row);
      const teamsWrap = $('[data-row-teams] .chip-pick', row);
      if (peopleWrap) ChipsPicker.bind(peopleWrap, {});
      if (teamsWrap) ChipsPicker.bind(teamsWrap, {});
      const remove = $('[data-row-remove]', row);
      if (remove && !remove.dataset.bound) {
        remove.dataset.bound = '1';
        remove.addEventListener('click', () => {
          row.classList.add('is-leaving');
          setTimeout(() => {
            row.remove();
            Automations.hideRoutingEmpty(root);
            Automations.updateSummary(root, $('#modal-form'));
          }, 120);
        });
      }
    });
    Automations.hideRoutingEmpty(root);
  },

  hideRoutingEmpty(root) {
    const wrap = $('#rule-routing-rows', root);
    if (!wrap) return;
    const rows = $$('[data-routing-row]', wrap);
    const empty = $('[data-routing-empty]', wrap);
    if (rows.length && empty) empty.remove();
    if (!rows.length && !empty) {
      wrap.innerHTML = '<div class="rule-empty" data-routing-empty>No overrides — everyone above is used for every matching order.</div>';
    }
  },

  addRoutingRow(root, current, dir) {
    const wrap = $('#rule-routing-rows', root);
    if (!wrap) return;
    const empty = $('[data-routing-empty]', wrap);
    if (empty) empty.remove();
    wrap.insertAdjacentHTML('beforeend', Automations.routingRowHtml('', [], [], dir));
    Automations.bindRoutingRows(root, current, dir);
    const rows = $$('[data-routing-row]', wrap);
    const last = rows[rows.length - 1];
    if (last) {
      last.classList.add('is-entering');
      const sel = $('[data-row-customer]', last);
      if (sel) sel.focus();
    }
  },

  // refreshRoutingCustomers keeps the customer dropdowns free of duplicates.
  refreshRoutingCustomers(root) {
    const rows = $$('[data-routing-row]', root);
    const used = new Set();
    rows.forEach((row) => {
      const sel = $('[data-row-customer]', row);
      if (!sel) return;
      Array.from(sel.options).forEach((opt) => {
        if (!opt.value) return;
        opt.disabled = used.has(opt.value) && opt.value !== sel.value;
      });
      if (sel.value) used.add(sel.value);
    });
  },

  // draftRule builds the API body from the live dialog. Used both for saving and
  // for the dry-run check, so the preview always matches what will be saved.
  draftRule(form, root) {
    const routing = { customerTargets: {}, customerTeams: {} };
    $$('[data-routing-row]', root).forEach((row) => {
      const sel = $('[data-row-customer]', row);
      const name = sel ? String(sel.value || '').trim() : '';
      if (!name) return;
      const people = ChipsPicker.values($('[data-row-people] .chip-pick', row));
      const teams = ChipsPicker.values($('[data-row-teams] .chip-pick', row));
      if (people.length) routing.customerTargets[name] = people;
      if (teams.length) routing.customerTeams[name] = teams;
    });
    const value = (name, fallback) => (form.elements[name] ? form.elements[name].value : fallback);
    const trigger = value('trigger', 'QC_FAILED');
    return {
      name: String(value('name', '')).trim(),
      trigger,
      action: value('action', 'NOTIFY'),
      thresholdMinutes: trigger === 'HOLD_AGING' ? Number(value('thresholdMinutes', '0')) || 0 : 0,
      targets: ChipsPicker.values($('#mf-targets', root)),
      teams: ChipsPicker.values($('#mf-teams', root)),
      message: String(value('message', '')).trim(),
      escalationLevel: value('escalationLevel', 'L1'),
      enabled: !!(form.elements.enabled && form.elements.enabled.checked),
      customers: ChipsPicker.values($('#mf-customers', root)),
      excludedCustomers: ChipsPicker.values($('#mf-excludedCustomers', root)),
      departments: ChipsPicker.values($('#mf-departments', root)),
      products: ChipsPicker.values($('#mf-products', root)),
      customerTargets: routing.customerTargets,
      customerTeams: routing.customerTeams,
    };
  },

  // validateDraft mirrors the server's rules so a mistake is explained in the
  // dialog instead of coming back as a 400.
  validateDraft(draft, root) {
    if (!draft.name) return 'Give the rule a name so you recognise it later.';
    const total = (draft.targets || []).length + (draft.teams || []).length;
    if (draft.action === 'NOTIFY' && total === 0) {
      return 'Pick at least one person or team to notify.';
    }
    if (draft.action === 'ASSIGN' && total !== 1) {
      return 'ASSIGN hands the order to exactly one person or team. Pick one — or switch the action to “Notify people” if several should hear about it.';
    }
    if (draft.action === 'ESCALATE' && total > 1) {
      return 'An escalation can have at most one owner (a person or a team).';
    }
    const included = draft.customers || [];
    const excluded = draft.excludedCustomers || [];
    const overlap = included.filter((c) => excluded.some((e) => e.toLowerCase() === c.toLowerCase()));
    if (overlap.length) {
      return `${overlap.join(', ')} is both included and excluded. Remove it from one of the two lists.`;
    }
    const problems = [];
    const check = (label, users, teams) => {
      const sum = (users || []).length + (teams || []).length;
      if (draft.action === 'ASSIGN' && sum !== 1) problems.push(`${label} needs exactly one person or team.`);
      if (draft.action === 'ESCALATE' && sum > 1) problems.push(`${label} can have at most one person or team.`);
      if (draft.action === 'NOTIFY' && sum === 0) problems.push(`${label} has nobody to notify.`);
      if (included.length && !included.some((c) => c.toLowerCase() === label.toLowerCase())) {
        problems.push(`${label} is not in “Only these customers”, so its routing would never be used.`);
      }
      if (excluded.some((c) => c.toLowerCase() === label.toLowerCase())) {
        problems.push(`${label} is excluded, so its routing would never be used.`);
      }
    };
    Object.keys(draft.customerTargets || {}).forEach((c) => check(c, draft.customerTargets[c], (draft.customerTeams || {})[c]));
    Object.keys(draft.customerTeams || {}).forEach((c) => {
      if (!(draft.customerTargets || {})[c]) check(c, [], draft.customerTeams[c]);
    });
    if (problems.length) return problems[0];
    const emptyRow = $$('[data-routing-row]', root || document).some((row) => {
      const sel = $('[data-row-customer]', row);
      if (!sel || !sel.value) return false;
      const people = ChipsPicker.values($('[data-row-people] .chip-pick', row));
      const teams = ChipsPicker.values($('[data-row-teams] .chip-pick', row));
      return people.length + teams.length === 0;
    });
    if (emptyRow) return 'A per-customer row has a customer but nobody to notify or assign. Add a person/team or remove the row.';
    return '';
  },

  renderPreview(out) {
    const rule = out.rule || {};
    const matched = out.matched || 0;
    const shown = out.candidates || [];
    const items = shown.map((c) => {
      const who = (c.recipients || []).map((u) => `<span class="recip recip-user">@${esc(u)}</span>`).join('')
        || (c.assignee ? `<span class="recip recip-user">@${esc(c.assignee)}</span>` : '<span class="muted">nobody</span>');
      return `<li class="rule-preview-item">
        <a class="rule-preview-order mono" href="#/orders/${c.orderId}">${esc(c.orderNumber)}</a>
        ${c.customerName ? `<span class="rule-preview-cust">${esc(c.customerName)}</span>` : ''}
        <span class="rule-preview-detail">${esc(c.detail || '')}</span>
        <span class="rule-preview-reach">${rule.action === 'ASSIGN' ? 'goes to' : 'reaches'} ${who}</span>
        ${c.problem ? `<span class="rule-warn">${esc(c.problem)}</span>` : ''}
      </li>`;
    }).join('');
    const more = matched > shown.length ? `<p class="muted rule-preview-more">…and ${matched - shown.length} more.</p>` : '';
    const warnings = (out.warnings || []).map((w) => `<li>${esc(w)}</li>`).join('');
    return `<div class="rule-preview-head">
        <span class="rule-preview-count ${matched ? 'has-hits' : ''}">${matched}</span>
        <span class="rule-preview-title">order${matched === 1 ? '' : 's'} would be acted on right now</span>
      </div>
      ${items ? `<ul class="rule-preview-list">${items}</ul>${more}`
        : '<div class="rule-preview-empty">No order matches right now — the rule stays armed for the next matching event.</div>'}
      ${warnings ? `<div class="rule-preview-notes"><strong>Notes</strong><ul>${warnings}</ul></div>` : ''}`;
  },

  async duplicate(rule) {
    const copy = JSON.parse(JSON.stringify(rule));
    copy.name = `${rule.name} (copy)`;
    copy.id = undefined;
    try {
      const saved = await api('POST', '/api/v1/automations', copy);
      if (saved && saved.rule) Automations.recentId = saved.rule.id;
      toast('Rule copied');
      await Automations.load();
    } catch (err) { toast(err.message, true); }
  },

  async toggle(rule) {
    try {
      await api('POST', `/api/v1/automations/${rule.id}`, { ...rule, enabled: !rule.enabled });
      Automations.recentId = rule.id;
      toast(rule.enabled ? 'Rule disabled' : 'Rule enabled — it is armed');
      await Automations.load();
    } catch (err) { toast(err.message, true); }
  },

  async remove(rule) {
    const yes = await confirmDialog('Delete automation rule', `Delete “${rule.name}” and its execution history?`, 'Delete rule');
    if (!yes) return;
    try {
      await api('DELETE', `/api/v1/automations/${rule.id}`);
      toast('Rule deleted');
      await Automations.load();
    } catch (err) { toast(err.message, true); }
  },

  async evaluate() {
    const btn = $('#automation-evaluate');
    btn.disabled = true;
    btn.classList.add('is-busy');
    try {
      const d = await api('POST', '/api/v1/automations/evaluate', {});
      toast(`Checked every rule: ${d.executed || 0} action${d.executed === 1 ? '' : 's'} executed`);
      await Automations.load();
    } catch (err) { toast(err.message, true); }
    finally {
      btn.disabled = false;
      btn.classList.remove('is-busy');
    }
  },
};
