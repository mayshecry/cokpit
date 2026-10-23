'use strict';

                                                                       
                        
const Automations = {
  rules: [],
  runs: [],
  bound: false,
  loadSeq: 0,

  triggerLabels: {
    QC_FAILED: 'QC failure',
    SLA_BREACHED: 'SLA breached',
    HOLD_AGING: 'Aging hold',
  },
  actionLabels: {
    NOTIFY: 'Notify users',
    ASSIGN: 'Assign order',
    ESCALATE: 'Raise escalation',
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

  render() {
    const enabled = Automations.rules.filter((r) => r.enabled).length;
    const failed = Automations.runs.filter((r) => r.status === 'FAILED').length;
    const successful = Automations.runs.filter((r) => r.status === 'SUCCESS').length;
    const card = (value, cls, label) => `<div class="stat ${cls}"><span class="num">${value}</span><span class="lbl">${label}</span></div>`;
    $('#automation-stats').innerHTML = [
      card(Automations.rules.length, 'accent', 'Total rules'),
      card(enabled, 'good', 'Enabled'),
      card(Automations.rules.length - enabled, 'neutral', 'Disabled'),
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
      <td>${esc(r.action === 'ESCALATE' ? `${r.escalationLevel}${r.targets.length ? ' → @' + r.targets[0] : ''}` : (r.targets || []).map((u) => '@' + u).join(', ') || '—')}</td>
      <td>${badge(r.enabled ? 'good' : 'neutral', r.enabled ? 'Enabled' : 'Disabled')}</td>
      <td class="actions"><div class="row-actions">
        <button type="button" class="btn btn-ghost btn-sm" data-rule-action="edit" data-rule-id="${r.id}">Edit</button>
        <button type="button" class="btn btn-ghost btn-sm" data-rule-action="toggle" data-rule-id="${r.id}">${r.enabled ? 'Disable' : 'Enable'}</button>
        <button type="button" class="btn btn-ghost btn-sm danger-ghost" data-rule-action="delete" data-rule-id="${r.id}">Delete</button>
      </div></td>
    </tr>`).join('');
    $('#automation-table tbody').innerHTML = rows || `<tr><td colspan="8"><div class="empty"><h3>No automation rules</h3><p>Create a rule to notify, assign or escalate when an order needs attention.</p></div></td></tr>`;

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

  async edit(rule) {
    if (!state.userBriefs || !state.userBriefs.length) await loadUserBriefs();
    const current = rule || {
      name: '', trigger: 'QC_FAILED', action: 'NOTIFY', thresholdMinutes: 240,
      targets: [(state.user || {}).username].filter(Boolean), message: '', escalationLevel: 'L1', enabled: true,
    };
    const res = await modal({
      title: rule ? `Edit rule ${rule.id}` : 'New automation rule',
      description: 'Each rule performs one action once per source event. Use separate rules for multiple actions.',
      confirmLabel: rule ? 'Save rule' : 'Create rule', wide: true,
      fields: [
        { name: 'name', label: 'Rule name', required: true, autofocus: true, value: current.name },
        { name: 'trigger', label: 'Trigger', type: 'select', value: current.trigger,
          options: Object.entries(Automations.triggerLabels).map(([value, label]) => ({ value, label })) },
        { name: 'thresholdMinutes', label: 'Hold age threshold (minutes)', type: 'number', value: String(current.thresholdMinutes || 240),
          hint: 'Used only by the Aging hold trigger.' },
        { name: 'action', label: 'Action', type: 'select', value: current.action,
          options: Object.entries(Automations.actionLabels).map(([value, label]) => ({ value, label })) },
        { name: 'targets', label: 'Target usernames', required: current.action !== 'ESCALATE', value: (current.targets || []).join(', '),
          placeholder: 'alice, bob', hint: 'Comma-separated. Notify allows several, assign exactly one, escalation zero or one.' },
        { name: 'escalationLevel', label: 'Escalation level', type: 'select', value: current.escalationLevel || 'L1',
          options: [{ value: 'L1', label: 'L1 — team lead' }, { value: 'L2', label: 'L2 — operations' }, { value: 'L3', label: 'L3 — process owner' }] },
        { name: 'message', label: 'Notification / escalation note', type: 'textarea', value: current.message || '' },
        { name: 'enabled', label: 'Rule enabled', type: 'checkbox', checked: !!current.enabled },
      ],
    });
    if (!res) return;
    const body = {
      name: res.name.trim(), trigger: res.trigger, action: res.action,
      thresholdMinutes: res.trigger === 'HOLD_AGING' ? Number(res.thresholdMinutes) : 0,
      targets: res.targets.split(/[\s,;]+/).map((v) => v.trim()).filter(Boolean),
      message: res.message.trim(), escalationLevel: res.escalationLevel, enabled: res.enabled === 'on',
    };
    try {
      await api('POST', rule ? `/api/v1/automations/${rule.id}` : '/api/v1/automations', body);
      toast(rule ? 'Automation rule updated' : 'Automation rule created');
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
