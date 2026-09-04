'use strict';

  

  function renderRoleLegend() {
    const byRole = { viewer: 0, operator: 0, qc: 0, admin: 0 };
    for (const u of state.users) if (byRole[u.role] !== undefined) byRole[u.role]++;
    $('#role-legend').innerHTML = ROLES.map((r) => `
      <div class="role-card">
        <div class="head">${badge('role-' + r, r)}<span class="n">${byRole[r]} user${byRole[r] === 1 ? '' : 's'}</span></div>
        <div class="perm-list">${rolePerms[r].map((p) => `<span class="perm">${esc(p)}</span>`).join('')}</div>
      </div>`).join('');
  }

  async function renderUsers(reload) {
    if (!can('users:manage')) return;
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

    tbody.innerHTML = state.users.map((u) => {
      const isMe = u.id === (state.user && state.user.id);
      return `
      <tr>
        <td class="mono">${u.id}</td>
        <td class="strong">${esc(u.username)}${isMe ? ' <span class="muted">(you)</span>' : ''}</td>
        <td>${esc(u.displayName || '—')}</td>
        <td>${badge('role-' + esc(u.role), u.role)}</td>
        <td class="time">${fmtTime(u.createdAt)}</td>
        <td class="actions"><span class="row-actions">
          <button class="btn btn-secondary btn-sm" data-act="role" data-id="${u.id}">Role</button>
          <button class="btn btn-secondary btn-sm" data-act="pwd" data-id="${u.id}">Password</button>
          ${isMe ? '' : `<button class="btn btn-secondary btn-sm danger-ghost" data-act="del" data-id="${u.id}">Delete</button>`}
        </span></td>
      </tr>`;
    }).join('');

    $('#user-count').textContent = `${state.users.length} user${state.users.length === 1 ? '' : 's'}`;
    renderRoleLegend();
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

  

