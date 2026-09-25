'use strict';

  function alertForNewNotifications() {
    const fresh = (state.notifications || []).filter((n) => !n.readAt && !state.prevNotifIds.has(n.id));
    state.prevNotifIds = new Set((state.notifications || []).map((n) => n.id));
    if (!fresh.length) return;
    const alertsOn = localStorage.getItem('cockpit_alerts') === 'on';
    if (!alertsOn) return;
    if (document.hidden) {
      const body = (fresh[0].body || fresh[0].kind || '') + (fresh[0].orderNumber ? ' · ' + fresh[0].orderNumber : '');
      if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
        try { new Notification('Cockpit', { body, tag: 'cockpit-mention' }); } catch {  }
      }
    }
    beep();
  }
  

  function renderNotifications() {
    const badge = $('#bell-badge');
    if (state.unread > 0) {
      badge.textContent = state.unread > 99 ? '99+' : String(state.unread);
      badge.classList.remove('hidden');
    } else {
      badge.classList.add('hidden');
    }

    const list = $('#notif-list');
    const notes = state.notifications || [];
    list.innerHTML = notes.length
      ? notes.map((n) => `
          <div class="notif-item${n.readAt ? '' : ' unread'}${n.kind === 'flag' ? ' is-flag' : ''}" data-id="${n.orderId}" tabindex="0" role="button">
            <div class="notif-title">${n.kind === 'flag' ? '<span class="flag-ico" aria-hidden="true">⚑</span> ' : n.kind === 'goal' ? '<span class="goal-ico" aria-hidden="true">🎯</span> ' : ''}${esc(n.body || n.kind)}</div>
            <div class="notif-meta">${esc(n.orderNumber)} · ${esc(relTime(n.createdAt))}${n.readAt ? '' : ' · <strong>new</strong>'}</div>
          </div>`).join('')
      : '<p class="empty-inline">Nothing here yet. Mentions of your username and flagged steps will show up.</p>';
  }

  async function loadNotifications(silent) {
    try {
      const d = await api('GET', '/api/v1/notifications');
      state.notifications = d.notifications || [];
      state.unread = d.unread || 0;
      renderNotifications();
      alertForNewNotifications();
      updateBadges();
    } catch (err) {
      if (!silent) toast(err.message, true);
    }
  }

  async function markNotificationsRead() {
    const ids = (state.notifications || []).filter((n) => !n.readAt).map((n) => n.id);
    if (!ids.length) return;
    try {
      await api('POST', '/api/v1/notifications/read', { ids });
      await loadNotifications(true);
    } catch (err) {
      toast(err.message, true);
    }
  }

