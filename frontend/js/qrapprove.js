'use strict';
                                                                        
                                                                               
                                                           
(function () {
  const $ = (id) => document.getElementById(id);
  const ticket = new URLSearchParams(location.search).get('t') || '';
  const msg = (t, good) => { const e = $('msg'); e.className = good ? 'err good' : 'err'; e.textContent = t; };

  const token = localStorage.getItem('cockpit_token') || '';
  let user = (function () { try { return JSON.parse(localStorage.getItem('cockpit_user') || 'null'); } catch { return null; } })();

  if (!ticket) {
    $('lead').textContent = 'Invalid sign-in QR (missing ticket). Ask the device to refresh its code.';
    return;
  }

  async function initApproval() {
    if (!user) {
                                                                              
                                                                              
      try {
        const r = await fetch('/api/v1/auth/me', {
          credentials: 'same-origin',
          headers: token ? { Authorization: 'Bearer ' + token } : {},
        });
        if (r.ok) {
          const d = await r.json();
          user = d.user || null;
          if (user) {
            localStorage.setItem('cockpit_user', JSON.stringify(user));
            localStorage.removeItem('cockpit_token');
          }
        }
      } catch { }
    }
    if (!user) {
                                                                                
                                                                                 
                                               
      location.replace('/launch.html?ticket=' + encodeURIComponent(ticket));
      return;
    }
    $('lead').textContent = 'A device scanned your sign-in QR. Sign it in with your cockpit account?';
    $('uname').textContent = user.displayName || user.username || '?';
    $('ureal').textContent = '@' + (user.username || '');
    $('bub').textContent = ((user.username || '?')[0] || '?').toUpperCase();
    $('who').hidden = false;
    $('row').hidden = false;
  }

  async function act(action) {
    $('approve').disabled = $('reject').disabled = true;
    try {
      const r = await fetch('/api/v1/qr-login/approve', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
        body: JSON.stringify({ ticket, action }),
      });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error((j.error && j.error.message) || 'Request failed.');
      if (action === 'approve') {
        $('row').hidden = true;
        $('lead').textContent = 'All set — the other screen is now signed in as you.';
        msg('You can close this page.', true);
      } else {
        $('row').hidden = true;
        $('lead').textContent = 'Rejected. The other screen will show a fresh code.';
        msg('Nothing was signed in.', true);
      }
    } catch (e) {
      msg(e.message || String(e));
      $('approve').disabled = $('reject').disabled = false;
    }
  }

  $('approve').addEventListener('click', () => act('approve'));
  $('reject').addEventListener('click', () => act('reject'));
  initApproval();
})();
