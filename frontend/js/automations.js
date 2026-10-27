'use strict';

const Automations = {
  rules: [],
  runs: [],
  bound: false,
  loadSeq: 0,
  // Directory caches for the pickers (people, teams, customers, products).
  dir: { users: [], teams: [], customers: [], products: [], loadedAt: 0 },

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

  bind() {
    if (Automations.bound) return;
    Automations.bound = true;
    $('#automation-new').addEventListener('click', () => Automations.edit());
    $('#automation-evaluate').addEventListener('click', () => Automations.evaluate());
    $('#automation-refresh').addEventListener('click', () => Automations.load());
    $('#automation-table tbody').addEventListener('click', (e) => {
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

  // loadDirectory fills the pickers. Everything is optional: a missing
  // endpoint (permissions) just leaves that picker empty instead of breaking
  // the dialog.
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

  // scopeTags renders the "where does this apply" summary of one rule.
  scopeTags(rule) {
    const tags = [];
    const customers = rule.customers || [];
    const excluded = rule.excludedCustomers || [];
    if (customers.length) {
      customers.forEach((c) => tags.push(`<span class="tag">${esc(c)}</span>`));
    } else if (!excluded.length) {
      tags.push('<span class="tag">All customers</span>');
    }
    excluded.forEach((c) => tags.push(`<span class="tag exclude">not ${esc(c)}</span>`));
    (rule.departments || []).forEach((d) => tags.push(`<span class="tag">dept ${esc(d)}</span>`));
    (rule.products || []).forEach((p) => tags.push(`<span class="tag">${esc(p)}</span>`));
    return tags.join('');
  },

  targetCell(rule) {
    const people = (rule.targets || []).map((u) => '@' + esc(u));
    const teams = (rule.teams || []).map((t) => `<span class="tag team">${esc(t)}</span>`);
    let base = people.concat(teams).join(' ') || '—';
    if (rule.action === 'ESCALATE') base = `${esc(rule.escalationLevel || 'L1')}${base === '—' ? '' : ' → ' + base}`;
    const routing = rule.customerTargets && Object.keys(rule.customerTargets).length
      ? Object.entries(rule.customerTargets).map(([c, list]) => `${esc(c)} → ${(list || []).map((u) => '@' + esc(u)).join(', ')}`).join('<br>')
      : '';
    const teamRouting = rule.customerTeams && Object.keys(rule.customerTeams).length
      ? Object.entries(rule.customerTeams).map(([c, list]) => `${esc(c)} → ${(list || []).join(', ')}`).join('<br>')
      : '';
    const extra = [routing, teamRouting].filter(Boolean).join('<br>');
    return extra ? `${base}<br><span class="muted">${extra}</span>` : base;
  },

  render() {
    const enabled = Automations.rules.filter((r) => r.enabled).length;
    const failed = Automations.runs.filter((r) => r.status === 'FAILED').length;
    const successful = Automations.runs.filter((r) => r.status === 'SUCCESS').length;
    const scoped = Automations.rules.filter((r) => (r.customers || []).length || (r.excludedCustomers || []).length
      || (r.departments || []).length || (r.products || []).length).length;
    const card = (value, cls, label) => `<div class="stat ${cls}"><span class="num">${value}</span><span class="lbl">${label}</span></div>`;
    $('#automation-stats').innerHTML = [
      card(Automations.rules.length, 'accent', 'Total rules'),
      card(enabled, 'good', 'Enabled'),
      card(scoped, 'info', 'Customer-scoped'),
      card(successful, 'info', 'Recent successes'),
      card(failed, failed ? 'bad' : 'good', 'Recent failures'),
    ].join('');
    $('#automation-rule-count').textContent = `${Automations.rules.length} rule${Automations.rules.length === 1 ? '' : 's'}`;

    const rows = Automations.rules.map((r) => `<tr>
      <td class="mono num">${r.id}</td>
      <td><strong>${esc(r.name)}</strong>${r.message ? `<br><span class="muted">${esc(r.message)}</span>` : ''}</td>
      <td>${esc(Automations.triggerLabels[r.trigger] || r.trigger)}</td>
      <td>${r.trigger === 'HOLD_AGING' ? `After ${r.thresholdMinutes} min` : 'On event'}</td>
      <td>${esc(Automations.actionLabels[r.action] || r.action)}</td>
      <td>${Automations.targetCell(r)}</td>
      <td><div class="automation-scope">${Automations.scopeTags(r)}</div></td>
      <td>${badge(r.enabled ? 'good' : 'neutral', r.enabled ? 'Enabled' : 'Disabled')}</td>
      <td class="actions"><div class="row-actions">
        <button type="button" class="btn btn-ghost btn-sm" data-rule-action="edit" data-rule-id="${r.id}">Edit</button>
        <button type="button" class="btn btn-ghost btn-sm" data-rule-action="duplicate" data-rule-id="${r.id}">Copy</button>
        <button type="button" class="btn btn-ghost btn-sm" data-rule-action="toggle" data-rule-id="${r.id}">${r.enabled ? 'Disable' : 'Enable'}</button>
        <button type="button" class="btn btn-ghost btn-sm danger-ghost" data-rule-action="delete" data-rule-id="${r.id}">Delete</button>
      </div></td>
    </tr>`).join('');
    $('#automation-table tbody').innerHTML = rows || `<tr><td colspan="9"><div class="empty"><h3>No automation rules</h3><p>Create a rule to notify, assign or escalate when an order needs attention.</p></div></td></tr>`;

    const history = Automations.runs.map((r) => `<tr>
      <td class="mono num">${r.id}</td>
      <td class="time">${esc(fmtCell(r.createdAt))}</td>
      <td>${esc(r.ruleName)}</td>
      <td><a class="mono" href="#/orders/${r.orderId}">${esc(r.orderNumber)}</a></td>
      <td>${esc(Automations.actionLabels[r.action] || r.action)}</td>
      <td>${badge(r.status === 'SUCCESS' ? 'good' : r.status === 'FAILED' ? 'bad' : 'warn', r.status)}</td>
      <td class="automation-run-detail">${esc(r.error || r.detail || '—')}<br><span class="muted mono">${esc(r.triggerKey)}</span></td>
    </tr>`).join('');
    $('#automation-runs-table tbody').innerHTML = history || '<tr><td colspan="7" class="muted">No actions have run yet.</td></tr>';
  },

  // ---------------------------------------------------------------------
  // Rule editor
  // ---------------------------------------------------------------------
  async edit(rule) {
    const dir = await Automations.loadDirectory();
    const current = rule || {
      name: '', trigger: 'QC_FAILED', action: 'NOTIFY', thresholdMinutes: 240,
      targets: [(state.user || {}).username].filter(Boolean), teams: [], message: '',
      escalationLevel: 'L1', enabled: true, customers: [], excludedCustomers: [],
      departments: [], products: [], customerTargets: {}, customerTeams: {},
    };

    const routingRows = () => Automations.routingRows(current);

    const afterHtml = `
      <section class="rule-section">
        <h3>Where it applies</h3>
        <p class="rule-section-hint">Leave everything empty to apply to every order. Exclusions always win over inclusions.</p>
        <div class="field">
          <label for="mf-customers">Only these customers</label>
          ${ChipsPicker.html({ id: 'mf-customers', options: dir.customers, value: current.customers || [], placeholder: 'Search customers…', emptyHint: 'Empty = all customers' })}
        </div>
        <div class="field">
          <label for="mf-excludedCustomers">Except these customers</label>
          ${ChipsPicker.html({ id: 'mf-excludedCustomers', options: dir.customers, value: current.excludedCustomers || [], placeholder: 'Search customers…', emptyHint: 'Nobody excluded' })}
        </div>
        <div class="field">
          <label for="mf-departments">Only these departments</label>
          ${ChipsPicker.html({ id: 'mf-departments', options: dir.teams, value: current.departments || [], placeholder: 'Search departments…', emptyHint: 'Uses the department of the order\u2019s product' })}
        </div>
        <div class="field">
          <label for="mf-products">Only these products</label>
          ${ChipsPicker.html({ id: 'mf-products', options: dir.products, value: current.products || [], placeholder: 'Search products…', emptyHint: 'Empty = every product' })}
        </div>
      </section>
      <section class="rule-section">
        <h3>Different people per customer</h3>
        <p class="rule-section-hint">Optional. When an order belongs to a listed customer, these people replace the default recipients above. Great for “Kenter goes to Alice, Dela goes to Bob”.</p>
        <div class="rule-rows" id="rule-routing-rows">${routingRows()}</div>
        <div><button type="button" class="btn btn-secondary btn-sm" id="rule-add-routing">＋ Add customer</button></div>
      </section>
      <section class="rule-section">
        <h3>Check before you save</h3>
        <p class="rule-section-hint">Runs the rule against the orders that exist right now and shows who it would reach. Nothing is saved or sent.</p>
        <div><button type="button" class="btn btn-secondary btn-sm" id="rule-preview-run">Check against current orders</button></div>
        <div id="rule-preview" class="rule-preview hidden"></div>
      </section>`;

    const fields = [
      { name: 'name', label: 'Rule name', required: true, autofocus: true, value: current.name },
      { name: 'trigger', label: 'Trigger', type: 'select', value: current.trigger,
        options: Object.entries(Automations.triggerLabels).map(([value, label]) => ({ value, label })) },
      { name: 'thresholdMinutes', label: 'Hold age threshold (minutes)', type: 'number',
        value: String(current.thresholdMinutes || 240), hint: 'Used only by the Aging hold trigger.' },
      { name: 'action', label: 'Action', type: 'select', value: current.action,
        options: Object.entries(Automations.actionLabels).map(([value, label]) => ({ value, label })),
        hint: Automations.actionHints[current.action] },
      { name: 'targets', label: 'People', type: 'chips', value: current.targets || [],
        options: dir.users, placeholder: 'Search people…',
        emptyHint: 'A person is an individual user', max: current.action === 'ASSIGN' ? 1 : 0 },
      { name: 'teams', label: 'Teams (departments)', type: 'chips', value: current.teams || [],
        options: dir.teams, placeholder: 'Search teams…',
        emptyHint: 'A team expands to its members', max: current.action === 'ASSIGN' ? 1 : 0 },
      { name: 'escalationLevel', label: 'Escalation level', type: 'select', value: current.escalationLevel || 'L1',
        options: [{ value: 'L1', label: 'L1 — team lead' }, { value: 'L2', label: 'L2 — operations' }, { value: 'L3', label: 'L3 — process owner' }] },
      { name: 'message', label: 'Notification / escalation note', type: 'textarea', value: current.message || '' },
      { name: 'enabled', label: 'Rule enabled', type: 'checkbox', checked: !!current.enabled },
    ];

    const res = await modal({
      title: rule ? `Edit rule ${rule.id}` : 'New automation rule',
      description: 'A rule performs one action once per source event. Create separate rules for different actions.',
      confirmLabel: rule ? 'Save rule' : 'Create rule',
      wide: true,
      fields,
      afterHtml,
      onMount: (form, root) => {
        // Bind the scope pickers that live in afterHtml.
        ['customers', 'excludedCustomers', 'departments', 'products'].forEach((name) => {
          const wrap = $('#mf-' + name, root);
          ChipsPicker.bind(wrap, {
            onChange: (values) => {
              // An overlap is rejected by the server; resolve it here instead of
              // letting the user hit a validation wall.
              if (name === 'customers' || name === 'excludedCustomers') {
                const other = name === 'customers' ? 'excludedCustomers' : 'customers';
                const otherWrap = $('#mf-' + other, root);
                const overlaps = values.filter((v) => ChipsPicker.values(otherWrap)
                  .some((o) => o.toLowerCase() === v.toLowerCase()));
                overlaps.forEach((v) => ChipsPicker.set(otherWrap, ChipsPicker.values(otherWrap)
                  .filter((o) => o.toLowerCase() !== v.toLowerCase())));
                if (overlaps.length) toast(`${overlaps.join(', ')} removed from “${other === 'customers' ? 'only these' : 'except these'} customers”.`, false);
              }
              if (name === 'customers' || name === 'excludedCustomers') Automations.refreshRoutingCustomers(root, current);
            },
          });
        });
        Automations.bindRoutingRows(root, current, dir);
        $('#rule-add-routing', root).addEventListener('click', () => {
          Automations.addRoutingRow(root, current, dir);
          Automations.refreshRoutingCustomers(root, current);
        });
        $('#rule-preview-run', root).addEventListener('click', async () => {
          const panel = $('#rule-preview', root);
          const btn = $('#rule-preview-run', root);
          btn.disabled = true;
          panel.classList.remove('hidden');
          panel.innerHTML = '<span class="muted">Checking…</span>';
          try {
            const draft = Automations.draftRule(form, root);
            const out = await api('POST', '/api/v1/automations/preview', draft);
            panel.innerHTML = Automations.renderPreview(out);
          } catch (err) {
            panel.innerHTML = `<span class="rule-warn">${esc(err.message)}</span>`;
          } finally {
            btn.disabled = false;
          }
        });
        Automations.syncActionUi(root, form, current);
        form.elements.action.addEventListener('change', () => Automations.syncActionUi(root, form, current));
        form.elements.trigger.addEventListener('change', () => Automations.syncActionUi(root, form, current));
      },
      collect: (data, form, root) => Automations.draftRule(form, root),
      validate: (data, form, root) => Automations.validateDraft(data, root),
    });
    if (!res) return;
    try {
      await api('POST', rule ? `/api/v1/automations/${rule.id}` : '/api/v1/automations', res);
      toast(rule ? 'Automation rule updated' : 'Automation rule created');
      await Automations.load();
    } catch (err) {
      toast(err.message, true);
    }
  },

  // syncActionUi keeps the recipient pickers honest while the dialog is open:
  // ASSIGN allows one recipient, ESCALATE at most one, NOTIFY as many as you
  // like, and the escalation level only matters for ESCALATE.
  syncActionUi(root, form, current) {
    const action = form.elements.action.value;
    const trigger = form.elements.trigger.value;
    const limit = action === 'ASSIGN' || action === 'ESCALATE' ? 1 : 0;
    const countOf = (name) => ChipsPicker.values($('#mf-' + name, root)).length;
    ['targets', 'teams'].forEach((name) => {
      const wrap = $('#mf-' + name, root);
      if (wrap) {
        wrap.dataset.max = String(limit);
        // One budget shared by both pickers, so "one person OR one team" holds.
        wrap._canAdd = () => limit === 0 || countOf('targets') + countOf('teams') < limit;
      }
      if (wrap && wrap._chipRefresh) wrap._chipRefresh();
    });
    const actionField = form.elements.action.closest('.field');
    const hint = actionField && $('.field-hint', actionField);
    if (hint) hint.textContent = Automations.actionHints[action] || '';
    const levelField = form.elements.escalationLevel.closest('.field');
    if (levelField) levelField.classList.toggle('hidden', action !== 'ESCALATE');
    const thresholdField = form.elements.thresholdMinutes.closest('.field');
    if (thresholdField) thresholdField.classList.toggle('hidden', trigger !== 'HOLD_AGING');
    // Never drop a selection behind the user's back — explain what no longer
    // fits and let them remove it.
    const people = ChipsPicker.values($('#mf-targets', root));
    const teams = ChipsPicker.values($('#mf-teams', root));
    const total = people.length + teams.length;
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
    // Per-customer rows respect the same shared budget.
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
    current.action = action;
  },

  routingRows(current) {
    const dir = Automations.dir;
    const names = Array.from(new Set([
      ...Object.keys(current.customerTargets || {}),
      ...Object.keys(current.customerTeams || {}),
    ]));
    if (!names.length) return '<div class="rule-empty" data-routing-empty>No per-customer routing yet — everyone above is used for every matching order.</div>';
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
        <span class="muted">replaces the default recipients</span>
      </div>
      <div>
        <label>People</label>
        <div data-row-people>${ChipsPicker.html({ id: 'row-people-' + Math.random().toString(36).slice(2), options: dir.users, value: people, placeholder: 'Search people…' })}</div>
      </div>
      <div>
        <label>Teams</label>
        <div data-row-teams>${ChipsPicker.html({ id: 'row-teams-' + Math.random().toString(36).slice(2), options: dir.teams, value: teams, placeholder: 'Search teams…' })}</div>
      </div>
      <button type="button" class="btn btn-ghost btn-sm danger-ghost rule-row-remove" data-row-remove>Remove</button>
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
          row.remove();
          Automations.refreshRoutingCustomers(root, current);
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
      wrap.innerHTML = '<div class="rule-empty" data-routing-empty>No per-customer routing yet — everyone above is used for every matching order.</div>';
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
    const action = form.elements.action.value;
    const trigger = form.elements.trigger.value;
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
    return {
      name: String(form.elements.name.value || '').trim(),
      trigger,
      action,
      thresholdMinutes: trigger === 'HOLD_AGING' ? Number(form.elements.thresholdMinutes.value) || 0 : 0,
      targets: ChipsPicker.values($('#mf-targets', root)),
      teams: ChipsPicker.values($('#mf-teams', root)),
      message: String(form.elements.message.value || '').trim(),
      escalationLevel: form.elements.escalationLevel.value,
      enabled: !!form.elements.enabled.checked,
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
    const people = (draft.targets || []).length;
    const teams = (draft.teams || []).length;
    const total = people + teams;
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
    // Per-customer rows.
    const problems = [];
    const check = (label, users, teams2) => {
      const sum = (users || []).length + (teams2 || []).length;
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
      const people2 = ChipsPicker.values($('[data-row-people] .chip-pick', row));
      const teams3 = ChipsPicker.values($('[data-row-teams] .chip-pick', row));
      return people2.length + teams3.length === 0;
    });
    if (emptyRow) return 'A per-customer row has a customer but nobody to notify or assign. Add a person/team or remove the row.';
    return '';
  },

  renderPreview(out) {
    const rule = out.rule || {};
    const matched = out.matched || 0;
    const rows = (out.candidates || []).map((c) => {
      const who = (c.recipients || []).map((u) => '@' + esc(u)).join(', ') || (c.assignee ? '@' + esc(c.assignee) : '—');
      return `<li><strong>${esc(c.orderNumber)}</strong>${c.customerName ? ` · ${esc(c.customerName)}` : ''} — ${esc(c.detail || '')}<br>
        <span class="muted">${rule.action === 'ASSIGN' ? 'goes to ' : 'reaches '}${who}</span>
        ${c.problem ? `<br><span class="rule-warn">${esc(c.problem)}</span>` : ''}</li>`;
    }).join('');
    const more = matched > (out.candidates || []).length ? `<p class="muted">…and ${matched - (out.candidates || []).length} more.</p>` : '';
    const warnings = (out.warnings || []).map((w) => `<li class="rule-warn">${esc(w)}</li>`).join('');
    return `<h4>Would act on ${matched} order${matched === 1 ? '' : 's'} right now</h4>
      ${rows ? `<ul>${rows}</ul>${more}` : '<p class="muted">No order matches right now — the rule stays armed for the next matching event.</p>'}
      ${warnings ? `<h4>Notes</h4><ul>${warnings}</ul>` : ''}`;
  },

  async duplicate(rule) {
    const copy = JSON.parse(JSON.stringify(rule));
    copy.name = `${rule.name} (copy)`;
    copy.id = undefined;
    try {
      await api('POST', '/api/v1/automations', copy);
      toast('Automation rule copied');
      await Automations.load();
    } catch (err) { toast(err.message, true); }
  },

  async toggle(rule) {
    try {
      await api('POST', `/api/v1/automations/${rule.id}`, { ...rule, enabled: !rule.enabled });
      toast(rule.enabled ? 'Automation rule disabled' : 'Automation rule enabled');
      await Automations.load();
    } catch (err) { toast(err.message, true); }
  },

  async remove(rule) {
    const yes = await confirmDialog('Delete automation rule', `Delete “${rule.name}” and its execution history?`, 'Delete rule');
    if (!yes) return;
    try {
      await api('DELETE', `/api/v1/automations/${rule.id}`);
      toast('Automation rule deleted');
      await Automations.load();
    } catch (err) { toast(err.message, true); }
  },

  async evaluate() {
    const btn = $('#automation-evaluate');
    btn.disabled = true;
    try {
      const d = await api('POST', '/api/v1/automations/evaluate', {});
      toast(`Automation cycle complete: ${d.executed || 0} action${d.executed === 1 ? '' : 's'} executed`);
      await Automations.load();
    } catch (err) { toast(err.message, true); }
    finally { btn.disabled = false; }
  },
};
