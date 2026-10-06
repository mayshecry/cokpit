'use strict';
// Pairing flow for client-only mode. Talks to the Go webview host via the
// clientMachine() / clientPairSave(json) bindings (bound by client/main.go).
// In a plain browser it falls back to sessionStorage so the flow can be demoed.
(function () {
  const $ = (id) => document.getElementById(id);
  const code = $('code'), go = $('go'), msg = $('msg');

  const savePair = (json) => {
    if (window.clientPairSave) return window.clientPairSave(json);
    sessionStorage.setItem('ck-enabled', '1');
  };

  const fail = (t) => { msg.className = 'err'; msg.textContent = t; };
  const ok = (t) => { msg.className = 'err ok'; msg.textContent = t; };

  go.addEventListener('click', async () => {
    const c = code.value.trim().toUpperCase();
    if (c.length < 6) { fail('Enter the pairing code exactly as shown (ABCD-EFGH).'); return; }
    go.disabled = true; fail('');
    try {
      const machine = window.clientMachine ? await window.clientMachine() : 'browser-demo';
      const name = window.clientMachine ? '' : 'browser demo';
      const r = await fetch('/api/v1/pair', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: c, machine, name }),
      });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) { fail(j && j.error ? j.error : 'Pairing failed.'); go.disabled = false; return; }
      // IMPORTANT: save the machine we SENT — response field is only a fallback.
      await savePair(JSON.stringify({ clientId: j.clientId, key: j.key, machine: machine, name: j.name || name, savedAt: new Date().toISOString() }));
      // The native host (clientPairSave) navigates the webview with a signed
      // URL that sets the cookie. Fall back to a manual redirect (browser demo).
      ok('Client linked — starting the application…');
      setTimeout(() => { if (!window.clientPairSave) location.assign('/'); }, 400);
    } catch (e) {
      fail('Could not reach the server: ' + e);
      go.disabled = false;
    }
  });

  code.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); go.click(); } });
  code.addEventListener('input', () => {
    // accept 8 alphanumeric chars (new numeric codes and legacy alnum codes),
    // display grouped 4-4
    const raw = code.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
    code.value = raw.length > 4 ? raw.slice(0, 4) + '-' + raw.slice(4) : raw;
  });
  code.focus();
})();
