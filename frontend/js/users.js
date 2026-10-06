'use strict';

  

  function renderRoleLegend() {
    const host = $('#role-legend');
    const byRole = {};
    ROLES.forEach((r) => { byRole[r] = 0; });
    for (const u of state.users) if (byRole[u.role] !== undefined) byRole[u.role]++;
    const extras = (state.user && state.user.permissions && state.user.permissions.length) || 0;
    const extraNote = extras
      ? `<p class="muted u-note">Your account also receives <strong>${extras}</strong> permission${extras === 1 ? '' : 's'} through your department membership${extras === 1 ? '' : 's'} — on top of your role.</p>`
      : '';
    host.innerHTML = extraNote + ROLES.map((r) => {
      const perms = rolePerms[r] || [];
      const head = perms.slice(0, 4);
      const rest = perms.slice(4);
      return `<div class="u-role-row" data-role="${r}">
        <span class="u-role-id">${badge('role-' + r, r)}</span>
        <span class="cfg-pill neutral u-role-n">${byRole[r]} user${byRole[r] === 1 ? '' : 's'}</span>
        <span class="u-role-perms">
          ${head.map((p) => `<span class="perm">${esc(p)}</span>`).join('')}
          ${rest.length ? `<span class="u-more">${rest.map((p) => `<span class="perm">${esc(p)}</span>`).join('')}</span>
            <button type="button" class="u-exp" data-exp="${r}">+${rest.length} more</button>` : ''}
        </span>
      </div>`;
    }).join('');
    if (!host._expBound) {
      host._expBound = true;
      host.addEventListener('click', (e) => {
        const b = e.target.closest('[data-exp]');
        if (!b) return;
        const row = b.closest('.u-role-row');
        const on = row.classList.toggle('open');
        const n = row.querySelectorAll('.u-more .perm').length;
        b.textContent = on ? 'show less' : `+${n} more`;
      });
    }
  }

  var userDepartments = {};
  let userFilter = '';

  function bindUserFilter() {
    const inp = $('#user-filter');
    if (!inp || inp._udBound) return;
    inp._udBound = true;
    inp.addEventListener('input', () => { userFilter = inp.value; renderUsers(false); });
  }

  async function loadUserDepartment(userId) {
    try {
      const d = await api('GET', '/api/v1/users/' + userId + '/departments');
      userDepartments[userId] = d.departments || [];
    } catch {
      userDepartments[userId] = [];
    }
  }

  async function loadAllUserDepartments() {
    for (const u of state.users) {
      await loadUserDepartment(u.id);
    }
  }

  async function renderUsers(reload) {
    if (!can('users:list')) return;
    bindUserFilter();
    var canManage = can('users:manage');
    if (reload) {
      try {
        const d = await api('GET', '/api/v1/users');
        state.users = d.users || [];
        state.loadingUsers = false;
      } catch (err) {
        state.loadingUsers = false;
        toast(err.message, true);
        return;
      }
    }
    if (canManage) {
      await loadAllUserDepartments();
      await renderPairDevice();
    }
    await loadDepartments();

    const tbody = $('#users-table tbody');
    if (state.loadingUsers) { tbody.innerHTML = skeletonRows(4, 6); return; }

    if (!state.users.length) {
      tbody.innerHTML = `<tr><td colspan="6"><div class="empty">
        <div class="empty-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="7.5" r="3.5"/><path d="M16 20v-1.5a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4V20"/></svg></div>
        <h3>No users</h3><p>Add your first operator to get started.</p></div></td></tr>`;
      $('#user-count').textContent = '';
      renderRoleLegend();
      return;
    }

    const q = (userFilter || '').trim().toLowerCase();
    const shown = q
      ? state.users.filter((u) => {
          const depts = (userDepartments[u.id] || []).map((d) => d.name).join(' ');
          return (u.username + ' ' + (u.displayName || '') + ' ' + u.role + ' ' + depts).toLowerCase().includes(q);
        })
      : state.users;

    tbody.innerHTML = shown.map((u) => {
      const isMe = u.id === (state.user && state.user.id);
      const depts = userDepartments[u.id] || [];
      const initial = esc(((u.displayName || u.username || '?').trim()[0] || '?').toUpperCase());
      var actionsHtml = `<span class="row-actions ud-acts${canManage ? '' : ' static'}">`;
      if (canManage) {
        actionsHtml += `<button class="btn btn-ghost btn-sm" data-act="role" data-id="${u.id}">Role</button>`;
        actionsHtml += `<button class="btn btn-ghost btn-sm" data-act="pwd" data-id="${u.id}">Password</button>`;
        actionsHtml += `<button class="btn btn-ghost btn-sm" data-act="dept" data-id="${u.id}">Dept</button>`;
        actionsHtml += `<button class="btn btn-ghost btn-sm" data-act="pairphone" data-id="${u.id}" title="Issue a one-time QR + code that pairs a phone as this account">📱 Pair phone</button>`;
        if (!isMe) actionsHtml += `<button class="btn btn-ghost btn-sm danger-ghost" data-act="del" data-id="${u.id}">Delete</button>`;
      } else {
        actionsHtml += '<span class="muted">View only</span>';
      }
      actionsHtml += '</span>';
      return `
      <tr class="u-row">
        <td class="ud-id mono">${u.id}</td>
        <td>
          <span class="u-user">
            <span class="ud-ava role-${esc(u.role)}">${initial}</span>
            <span class="u-user-t">
              <span class="u-user-n">${esc(u.displayName || u.username)}${isMe ? ' <span class="chip chip-ok u-you">you</span>' : ''}</span>
              <span class="u-user-h mono">@${esc(u.username)}</span>
            </span>
          </span>
        </td>
        <td>${badge('role-' + esc(u.role), u.role)}</td>
        <td>${depts.length > 0 ? depts.map((d) => `<span class="dept-tag">${esc(d.name)}</span>`).join(' ') : '<span class="muted">—</span>'}</td>
        <td class="time">${fmtTime(u.createdAt)}</td>
        <td class="actions">${actionsHtml}</td>
      </tr>`;
    }).join('') || `<tr><td colspan="6"><div class="empty"><h3>No match</h3><p>No user matches “${esc(userFilter)}”.</p></div></td></tr>`;

    const uc = $('#user-count');
    if (uc) uc.textContent = q
      ? `${shown.length} of ${state.users.length} users`
      : `${state.users.length} user${state.users.length === 1 ? '' : 's'}`;
    renderRoleLegend();
    renderDepartmentPanel();
  }

  async function openCreateUser() {
    const res = await modal({
      title: 'Add user',
      description: 'Create an account and assign its role.',
      confirmLabel: 'Add user',
      fields: [
        { name: 'username', label: 'Username', required: true, autofocus: true, placeholder: 'jdoe' },
        { name: 'displayName', label: 'Display name', placeholder: 'Jane Doe' },
        { name: 'role', label: 'Role', type: 'select', options: ROLES, value: 'viewer' },
        { name: 'password', label: 'Password', type: 'password', required: true, hint: 'Minimum 8 characters.' },
      ],
    });
    if (!res) return;
    const ok = await act('POST', '/api/v1/users', {
      username: res.username.trim(),
      displayName: res.displayName.trim(),
      role: res.role,
      password: res.password,
    }, 'User created');
    if (ok) renderUsers(true);
  }

  async function loadDepartments() {
    try {
      var d = await api('GET', '/api/v1/departments');
      state.departments = d.departments || [];
    } catch {
      state.departments = [];
    }
  }

  async function openManageDepartments(userId) {
    const user = state.users.find((u) => u.id === userId);
    if (!user) return;
    var depts = userDepartments[userId] || [];
    var allDepts = state.departments || [];
    var fields = [{
      name: '_info',
      label: 'User: ' + user.username + ' (' + (user.displayName || '') + ')',
    }];
    for (var i = 0; i < allDepts.length; i++) {
      var d = allDepts[i];
      var checked = depts.some((ud) => ud.id === d.id);
      fields.push({
        name: 'dept_' + d.id,
        label: d.name + (d.description ? ' - ' + d.description : ''),
        type: 'checkbox',
        value: checked ? 'on' : '',
      });
    }
    var res = await modal({
      title: 'Manage Departments',
      description: 'Assign user to departments.',
      confirmLabel: 'Save',
      fields: fields,
    });
    if (!res) return;
    try {
      for (var i = 0; i < allDepts.length; i++) {
        var d = allDepts[i];
        var shouldBeIn = !!res['dept_' + d.id];
        var currentlyIn = depts.some((ud) => ud.id === d.id);
        if (shouldBeIn && !currentlyIn) {
          await api('POST', '/api/v1/departments/' + d.id + '/users', { userId: userId });
        } else if (!shouldBeIn && currentlyIn) {
          await api('DELETE', '/api/v1/departments/' + d.id + '/users/' + userId, {});
        }
      }
      await loadUserDepartment(userId);
      toast('Departments updated', 'success');
      renderUsers(false);
    } catch (err) {
      toast('Failed to update departments: ' + err.message, 'error');
    }
  }

  async function openCreateDepartment() {
    var res = await modal({
      title: 'New Department',
      description: 'Create a new department.',
      confirmLabel: 'Create',
      fields: [
        { name: 'name', label: 'Name', required: true, autofocus: true, placeholder: 'Department name' },
        { name: 'description', label: 'Description', placeholder: 'Optional description' },
      ],
    });
    if (!res) return;
    try {
      await api('POST', '/api/v1/departments', { name: res.name.trim(), description: res.description || '' });
      toast('Department created', 'success');
      await loadDepartments();
      renderUsers(false);
    } catch (err) {
      toast('Failed to create department: ' + err.message, 'error');
    }
  }

  function renderDepartmentPanel() {
    var panel = $('#department-panel');
    if (!panel) return;
    var depts = state.departments || [];
    var canManage = can('users:manage');
    var memberTotal = 0;
    for (var mi = 0; mi < depts.length; mi++) memberTotal += deptMemberCount(depts[mi].id);
    var html = `<div class="cfg-sec-head">
      <div class="cfg-sec-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 21V8.2L12 3.5l8.5 4.7V21"/><path d="M8.5 21v-5.5h7V21"/><path d="M3.5 21h17"/></svg></div>
      <div class="cfg-sec-txt"><h3>Departments</h3><p class="muted">Groups that grant extra permissions to their members.</p></div>
      <div class="cfg-sec-acts">
        <span class="cfg-pill neutral">${depts.length} dept${depts.length === 1 ? '' : 's'}${memberTotal ? ' · ' + memberTotal + ' seats' : ''}</span>
        ${canManage ? '<button type="button" class="btn btn-primary btn-sm" id="new-dept-btn">+ New Department</button>' : ''}
      </div>
    </div>`;
    if (canManage) {
      html += '<div class="dept-matrix-note muted"><span class="dot dot-brand" aria-hidden="true"></span> ' +
        esc(t('dept.hint')) + '</div>';
    }
    if (depts.length === 0) {
      html += '<p class="muted">No departments yet.</p>';
    } else {
      html += '<div class="dept-list">';
      for (var i = 0; i < depts.length; i++) {
        html += departmentCardHtml(depts[i], canManage);
      }
      html += '</div>';
    }
    panel.innerHTML = html;
    var newBtn = $('#new-dept-btn', panel);
    if (newBtn) newBtn.addEventListener('click', function() { openCreateDepartment(); });
    if (!canManage) return;
    $$('.dept-del-btn', panel).forEach(function(btn) {
      btn.addEventListener('click', async function(e) {
        e.stopPropagation();
        var id = Number(btn.dataset.id);
        var dept = (state.departments || []).find((d) => d.id === id);
        if (!dept) return;
        var ok = await confirmDialog('Delete Department', 'Delete "' + dept.name + '"?', 'Delete');
        if (!ok) return;
        try {
          await api('DELETE', '/api/v1/departments/' + id);
          toast('Department deleted', 'success');
          await loadDepartments();
          renderUsers(false);
        } catch (err) {
          toast('Delete failed: ' + err.message, 'error');
        }
      });
    });
    bindDeptPermPanel(panel);
  }

  function deptMemberCount(id) {
    var n = 0;
    for (var k in userDepartments) {
      if ((userDepartments[k] || []).some((ud) => ud.id === id)) n++;
    }
    return n;
  }

  function departmentCardHtml(d, canManage) {
    var perms = d.permissions || [];
    var members = deptMemberCount(d.id);
    var memberLabel = members === 1 ? t('dept.member1') : t('dept.member', { n: members });
    var body;
    if (canManage) {
      body = PERM_GROUPS.map((g) => `
        <div class="perm-group">
          <span class="perm-group-label">${esc(g.label)}</span>
          <div class="perm-opts">
            ${g.perms.map(([p, label]) => `
              <label class="perm-opt${perms.includes(p) ? ' on' : ''}" title="${esc(label)}">
                <input type="checkbox" class="dept-perm" data-dept="${d.id}" data-perm="${esc(p)}"${perms.includes(p) ? ' checked' : ''}>
                <span class="perm-opt-label">${esc(p)}</span>
              </label>`).join('')}
          </div>
        </div>`).join('');
    } else {
      body = perms.length
        ? '<div class="perm-list">' + perms.map((p) => `<span class="perm">${esc(p)}</span>`).join('') + '</div>'
        : '<p class="muted">' + esc(t('dept.none')) + '</p>';
    }
    return `<div class="dept-card" data-id="${d.id}">
      <div class="dept-card-head">
        <div class="dept-title">
          <strong>${escapeSI(d.name)}</strong>
          <span class="dept-count muted">${memberLabel}</span>
        </div>
        <div class="dept-head-actions">
          ${canManage ? `<button type="button" class="btn btn-sm btn-primary dept-perm-save hidden" data-id="${d.id}">${esc(t('dept.save'))}</button>` : ''}
          ${canManage ? `<button type="button" class="btn btn-sm btn-ghost danger-ghost dept-del-btn" data-id="${d.id}">Delete</button>` : ''}
        </div>
      </div>
      ${d.description ? `<p class="muted dept-desc">${escapeSI(d.description)}</p>` : ''}
      <div class="dept-matrix">${body}</div>
    </div>`;
  }

  function bindDeptPermPanel(panel) {
    $$('.dept-perm', panel).forEach((cb) => {
      cb.addEventListener('change', () => {
        var card = cb.closest('.dept-card');
        var opt = cb.closest('.perm-opt');
        if (opt) opt.classList.toggle('on', cb.checked);
        if (card) {
          var save = card.querySelector('.dept-perm-save');
          if (save) save.classList.remove('hidden');
        }
      });
    });
    $$('.dept-perm-save', panel).forEach((btn) => {
      btn.addEventListener('click', async () => {
        var id = Number(btn.dataset.id);
        var perms = $$('.dept-perm', panel)
          .filter((c) => Number(c.dataset.dept) === id && c.checked)
          .map((c) => c.dataset.perm);
        btn.disabled = true;
        try {
          await api('POST', '/api/v1/departments/' + id + '/permissions', { permissions: perms });
          await loadDepartments();
          renderUsers(false);
          toast(t('dept.saved'), 'success');
        } catch (err) {
          toast('Failed to save permissions: ' + err.message, 'error');
        } finally {
          btn.disabled = false;
        }
      });
    });
  }

async function renderPairDevice() {
    const card = $('#pair-device-card');
    if (!card) return;
    try {
      const d = await api('GET', '/api/v1/admin/pair-qr');
      $('#pair-qr-img').src = d.dataUrl;
      $('#pair-code').textContent = d.code;
      $('#pair-url').textContent = d.url;
      card.classList.remove('hidden');
    } catch (e) {
      card.classList.add('hidden'); // client-only mode off, or no permission
    }
    const btn = $('#pair-refresh-btn');
    if (btn && !btn.dataset.bound) {
      btn.dataset.bound = '1';
      btn.addEventListener('click', async () => {
        btn.disabled = true;
        try {
          await api('POST', '/api/v1/admin/pair-code');
          await renderPairDevice();
          toast('New pairing code issued', false);
        } catch (e) { toast(e.message, true); }
        btn.disabled = false;
      });
    }
  }

