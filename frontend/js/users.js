'use strict';

  

  function renderRoleLegend() {
    const byRole = { viewer: 0, operator: 0, qc: 0, npi: 0, admin: 0 };
    for (const u of state.users) if (byRole[u.role] !== undefined) byRole[u.role]++;
    $('#role-legend').innerHTML = ROLES.map((r) => `
      <div class="role-card">
        <div class="head">${badge('role-' + r, r)}<span class="n">${byRole[r]} user${byRole[r] === 1 ? '' : 's'}</span></div>
        <div class="perm-list">${rolePerms[r].map((p) => `<span class="perm">${esc(p)}</span>`).join('')}</div>
      </div>`).join('');
  }

  var userDepartments = {};

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
    }
    await loadDepartments();

    const tbody = $('#users-table tbody');
    if (state.loadingUsers) { tbody.innerHTML = skeletonRows(4, 7); return; }

    if (!state.users.length) {
      tbody.innerHTML = `<tr><td colspan="7"><div class="empty">
        <div class="empty-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="7.5" r="3.5"/><path d="M16 20v-1.5a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4V20"/></svg></div>
        <h3>No users</h3><p>Add your first operator to get started.</p></div></td></tr>`;
      $('#user-count').textContent = '';
      renderRoleLegend();
      return;
    }

    tbody.innerHTML = state.users.map((u) => {
      const isMe = u.id === (state.user && state.user.id);
      const depts = userDepartments[u.id] || [];
      var actionsHtml = '<span class="row-actions">';
      if (canManage) {
        actionsHtml += `<button class="btn btn-secondary btn-sm" data-act="role" data-id="${u.id}">Role</button>`;
        actionsHtml += `<button class="btn btn-secondary btn-sm" data-act="pwd" data-id="${u.id}">Password</button>`;
        actionsHtml += `<button class="btn btn-secondary btn-sm" data-act="dept" data-id="${u.id}">Dept</button>`;
        if (!isMe) actionsHtml += `<button class="btn btn-secondary btn-sm danger-ghost" data-act="del" data-id="${u.id}">Delete</button>`;
      } else {
        actionsHtml += '<span class="muted">View only</span>';
      }
      actionsHtml += '</span>';
      return `
      <tr>
        <td class="mono">${u.id}</td>
        <td class="strong">${esc(u.username)}${isMe ? ' <span class="muted">(you)</span>' : ''}</td>
        <td>${esc(u.displayName || '—')}</td>
        <td>${badge('role-' + esc(u.role), u.role)}</td>
        <td>${depts.length > 0 ? depts.map((d) => `<span class="dept-tag">${esc(d.name)}</span>`).join(' ') : '<span class="muted">—</span>'}</td>
        <td class="time">${fmtTime(u.createdAt)}</td>
        <td class="actions">${actionsHtml}</td>
      </tr>`;
    }).join('');

    $('#user-count').textContent = `${state.users.length} user${state.users.length === 1 ? '' : 's'}`;
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
    var html = '<div class="dept-panel-header"><h3>Departments</h3>';
    if (canManage) {
      html += '<button type="button" class="btn btn-sm btn-primary" id="new-dept-btn">+ New Department</button>';
    }
    html += '</div>';
    if (depts.length === 0) {
      html += '<p class="muted">No departments yet.</p>';
    } else {
      html += '<div class="dept-list">';
      for (var i = 0; i < depts.length; i++) {
        var d = depts[i];
        html += '<div class="dept-card" data-id="' + d.id + '">' +
          '<div class="dept-card-head"><strong>' + escapeSI(d.name) + '</strong>';
        if (canManage) {
          html += '<button type="button" class="btn btn-sm btn-ghost danger-ghost dept-del-btn" data-id="' + d.id + '">Delete</button>';
        }
        html += '</div>' +
          (d.description ? '<p class="muted dept-desc">' + escapeSI(d.description) + '</p>' : '') +
          '</div>';
      }
      html += '</div>';
    }
    panel.innerHTML = html;
    if (!canManage) return;
    var newBtn = $('#new-dept-btn', panel);
    if (newBtn) newBtn.addEventListener('click', function() { openCreateDepartment(); });
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
  }

  

