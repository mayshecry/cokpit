'use strict';
(function () {
  const $ = (id) => document.getElementById(id);
  const codeEl = $('code'), goEl = $('go'), msgEl = $('msg'), leadEl = $('lead'), formEl = $('form');
  const LS_PAIR = 'ck-pair', LS_MACHINE = 'ck-machine';

  const fail = (t) => { msgEl.className = 'err'; msgEl.textContent = t; };
  const ok = (t) => { msgEl.className = 'err ok'; msgEl.textContent = t; };
  const start = (t) => { msgEl.className = 'err'; msgEl.innerHTML = '<span class="spin"></span> ' + t; };

  function sha256Bytes(data) {
    const K = [2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79, 83, 89, 97, 101, 103, 107, 109, 113, 127, 131, 137, 139, 149, 151, 157, 163, 167, 173, 179, 181, 191, 193, 197, 199, 211, 223, 227, 229, 233, 239, 241, 251, 257, 263, 269, 271, 277, 281, 283, 293, 307, 311]
      .map(p => {
        const c = Math.cbrt(p); const f = c - Math.floor(c);
        return Math.floor(f * 4294967296) >>> 0;
      });
    let h = [2, 3, 5, 7, 11, 13, 17, 19].map(p => {
      const s = Math.sqrt(p); const f = s - Math.floor(s);
      return Math.floor(f * 4294967296) >>> 0;
    });
    const byts = new Uint8Array(data);
    const withPad = Math.floor((byts.length + 8) / 64) * 64 + 64;
    const m = new Uint8Array(withPad);
    m.set(byts); m[byts.length] = 0x80;
    const bits = byts.length * 8;
    m[withPad - 4] = (bits >>> 24) & 255; m[withPad - 3] = (bits >>> 16) & 255;
    m[withPad - 2] = (bits >>> 8) & 255; m[withPad - 1] = bits & 255;
    const w = new Uint32Array(64);
    const rr = (x, n) => (x >>> n) | (x << (32 - n));
    for (let off = 0; off < withPad; off += 64) {
      for (let i = 0; i < 16; i++) w[i] = (m[off + i * 4] << 24) | (m[off + i * 4 + 1] << 16) | (m[off + i * 4 + 2] << 8) | m[off + i * 4 + 3];
      for (let i = 16; i < 64; i++) {
        const s0 = rr(w[i - 15], 7) ^ rr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
        const s1 = rr(w[i - 2], 17) ^ rr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
        w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
      }
      let [a, b, c, d, e, f, g, hh] = h;
      for (let i = 0; i < 64; i++) {
        const S1 = rr(e, 6) ^ rr(e, 11) ^ rr(e, 25);
        const ch = (e & f) ^ (~e & g);
        const t1 = (hh + S1 + ch + K[i] + w[i]) >>> 0;
        const S0 = rr(a, 2) ^ rr(a, 13) ^ rr(a, 22);
        const maj = (a & b) ^ (a & c) ^ (b & c);
        const t2 = (S0 + maj) >>> 0;
        hh = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0;
      }
      h = [h[0] + a, h[1] + b, h[2] + c, h[3] + d, h[4] + e, h[5] + f, h[6] + g, h[7] + hh].map(v => v >>> 0);
    }
    const out = new Uint8Array(32);
    h.forEach((v, i) => { out[i * 4] = v >>> 24; out[i * 4 + 1] = v >>> 16; out[i * 4 + 2] = v >>> 8; out[i * 4 + 3] = v; });
    return out;
  }
  const hexOf = (buf) => Array.from(new Uint8Array(buf)).map(b => b.toString(16).padStart(2, '0')).join('');
  function hmacSha256Fallback(keyBytes, msgBytes) {
    const k = keyBytes.length > 64 ? sha256Bytes(keyBytes) : keyBytes.slice();
    const mb = typeof msgBytes === 'string' ? utf8(msgBytes) : msgBytes;
    const pad = new Uint8Array(64).fill(0); pad.set(k);
    const ipad = pad.map(x => x ^ 0x36), opad = pad.map(x => x ^ 0x5c);
    const inner = new Uint8Array([...ipad, ...mb]);
    const innerHash = sha256Bytes(inner);
    return sha256Bytes(new Uint8Array([...opad, ...innerHash]));
  }
  const utf8 = (s) => new TextEncoder().encode(s);

  async function hmacHex(keyHex, msg) {
    const kb = new Uint8Array(keyHex.match(/.{2}/g).map(x => parseInt(x, 16)));
    if (window.crypto && window.crypto.subtle) {
      try {
        const k = await crypto.subtle.importKey('raw', kb, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
        return hexOf(await crypto.subtle.sign('HMAC', k, utf8(msg)));
      } catch (e) { }
    }
    return hexOf(hmacSha256Fallback(kb, utf8(msg)));
  }

  let pairedUser = null;
  const machine = () => {
    let m = localStorage.getItem(LS_MACHINE);
    if (!m) {
      const b = new Uint8Array(16); crypto.getRandomValues(b);
      m = hexOf(b);
      localStorage.setItem(LS_MACHINE, m);
    }
    return m;
  };
  const loadPair = () => { try { return JSON.parse(localStorage.getItem(LS_PAIR) || 'null'); } catch { return null; } };
  const savePair = (p) => localStorage.setItem(LS_PAIR, JSON.stringify(p));

  async function pair(code) {
    const r = await fetch('/api/v1/pair', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, machine: machine(), name: 'phone' }),
    });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error((j.error && j.error.message) || 'Pairing failed.');
    savePair({ clientId: j.clientId, key: j.key, machine: machine(), savedAt: new Date().toISOString() });
    if (j.user) {
                                                                      
      localStorage.removeItem('cockpit_token');
      localStorage.setItem('cockpit_user', JSON.stringify(j.user));
      pairedUser = j.user;
    }
  }

  async function launch(p) {
    const ts = String(Math.floor(Date.now() / 1000));
    const proof = await hmacHex(p.key, ts + '|' + p.machine);
    const r = await fetch('/api/v1/launch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ckid: p.clientId, ts, mach: p.machine, proof }),
    });
    if (r.status !== 204) {
      if (r.status === 403) localStorage.removeItem(LS_PAIR);
      const j = await r.json().catch(() => ({}));
      throw new Error((j.error && j.error.message) || 'Launch failed.');
    }
  }

  async function approveTicket(ticket) {
    const token = localStorage.getItem('cockpit_token') || '';
    const r = await fetch('/api/v1/qr-login/approve', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
      body: JSON.stringify({ ticket, action: 'approve' }),
    });
    if (!r.ok) {
      const j = await r.json().catch(() => ({}));
      throw new Error((j.error && j.error.message) || 'Approval failed.');
    }
  }

  function successScreen(ticket) {
    formEl.hidden = true;
    ok(pairedUser
      ? (ticket ? ('Linked & signed in as ' + (pairedUser.username || 'you') + ' — the other screen is now logged in ✓')
                : ('Linked & signed in as ' + (pairedUser.username || 'you') + ' ✓'))
      : (ticket ? 'Device linked — approve on this phone to finish.' : 'Device linked — signing you in…'));
    leadEl.textContent = ticket
      ? 'All set — the other device is now signed in with your account. You can close this page.'
      : (pairedUser ? 'Signed in — opening cockpit…' : 'Device linked — opening login…');
  }

  async function run() {
    const q = new URLSearchParams(location.search);
    const urlCode = (q.get('code') || '').trim();
    const ticket = (q.get('ticket') || '').trim();
    try {
      if (urlCode) {
        start('Linking this device…');
        await pair(urlCode);
      } else {
        const p = loadPair();
        if (p && p.clientId && p.key && p.machine) {
          start('Checking this device…');
          await launch(p);
        }
      }
      const p = loadPair();
      if (!p || !p.clientId) { showForm(urlCode); return; }

      if (ticket) {
        start('Approving sign-in…');
        try {
          await approveTicket(ticket);
          ok(pairedUser ? ('Signed in as ' + pairedUser.username + ' — the other screen is logged in ✓')
                        : 'Approved — the other screen is now logged in ✓');
          leadEl.textContent = 'All set — you can close this page.';
          if (pairedUser) setTimeout(() => location.replace('/'), 1500);
          return;
        } catch (e) {
          ok('Device linked. Sign in once below, then re-scan the QR.');
          location.assign(ticket ? ('/qr-approve.html?t=' + encodeURIComponent(ticket)) : '/');
          return;
        }
      }

      successScreen(false);
      location.replace('/');
    } catch (e) {
      fail(e.message || String(e));
      showForm(urlCode);
    }
  }

  function showForm(prefill) {
    leadEl.textContent = 'Enter the one-time pairing code — an admin shows it in Users → Pair phone (account-linked) or in the server log.';
    if (prefill) codeEl.value = prefill.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8).replace(/(.{4})(.{4})/, '$1-$2');
    formEl.hidden = false;
    codeEl.focus();
  }

  goEl.addEventListener('click', async () => {
    const ticket = new URLSearchParams(location.search).get('ticket') || '';
    const c = codeEl.value.trim().toUpperCase();
    if (c.replace(/[^A-Z0-9]/g, '').length < 6) { fail('Enter the pairing code exactly as issued (1234-5678).'); return; }
    goEl.disabled = true; fail(''); start('Linking…');
    try {
      await pair(c);
      if (ticket) {
        try { await approveTicket(ticket); } catch (e2) { }
        ok(pairedUser ? ('Linked & signed in as ' + pairedUser.username + ' ✓') : 'Device linked ✓');
        leadEl.textContent = pairedUser ? 'Signed in — opening cockpit…' : 'Linked — opening cockpit…';
        location.assign('/');
        return;
      }
      location.assign('/');
    } catch (e) { fail(e.message || String(e)); goEl.disabled = false; }
  });

  codeEl.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); goEl.click(); } });
  codeEl.addEventListener('input', () => {
    const raw = codeEl.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
    codeEl.value = raw.length > 4 ? raw.slice(0, 4) + '-' + raw.slice(4) : raw;
  });

  run();
})();
