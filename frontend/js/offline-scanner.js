'use strict';

                                                                               
                                                                              
                                                              
const OfflineScanner = (() => {
  const STORAGE_PREFIX = 'cockpit_offline_scans_v1:';
  const MAX_QUEUE = 500;
  const MAX_BACKOFF = 5 * 60 * 1000;
  let owner = '';
  let items = [];
  let syncing = false;
  let unreachable = false;
  let panelOpen = false;
  let initialized = false;
  let shellRegistered = false;
  let syncTimer = null;

  const username = () => (state.user && state.user.username) || '';
  const storageKey = (user) => STORAGE_PREFIX + (user || username());
  const online = () => navigator.onLine !== false;

  function makeID() {
    if (window.crypto && typeof window.crypto.randomUUID === 'function') return window.crypto.randomUUID();
    return 'scan-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);
  }

  function create(code) {
    return {
      id: makeID(),
      code: String(code || '').trim(),
      scannedAt: new Date().toISOString(),
      queuedAt: new Date().toISOString(),
      status: 'pending',
      attempts: 0,
      nextAttemptAt: 0,
      lastError: '',
    };
  }

  function validItem(item) {
    return item && typeof item.id === 'string' && item.id.length <= 128
      && typeof item.code === 'string' && item.code.trim() && item.code.length <= 256
      && typeof item.scannedAt === 'string' && Number.isFinite(Date.parse(item.scannedAt));
  }

  function read(user) {
    if (!user) return [];
    try {
      const parsed = JSON.parse(localStorage.getItem(storageKey(user)) || '[]');
      if (!Array.isArray(parsed)) return [];
      return parsed.filter(validItem).slice(0, MAX_QUEUE).map((item) => ({
        ...item,
        code: item.code.trim(),
        status: item.status === 'failed' ? 'failed' : 'pending',
        attempts: Math.max(0, Number(item.attempts) || 0),
        nextAttemptAt: Math.max(0, Number(item.nextAttemptAt) || 0),
        lastError: String(item.lastError || ''),
      }));
    } catch {
      return [];
    }
  }

  function load(force) {
    const user = username();
    if (!force && user === owner) return;
    owner = user;
    items = read(user);
    panelOpen = false;
    render();
  }

  function write(next) {
    try {
      localStorage.setItem(storageKey(owner), JSON.stringify(next));
      return true;
    } catch {
      return false;
    }
  }

  function update(id, patch) {
    const next = items.map((item) => item.id === id ? { ...item, ...patch } : item);
    if (!write(next)) return false;
    items = next;
    render();
    return true;
  }

  function remove(id) {
    const next = items.filter((item) => item.id !== id);
    if (!write(next)) return false;
    items = next;
    render();
    return true;
  }

  function enqueue(scan) {
    load();
    if (!owner || !can('scan:use')) return null;
    if (items.length >= MAX_QUEUE || items.some((item) => item.id === scan.id)) return null;
    const item = { ...scan, status: 'pending', lastError: scan.lastError || '' };
    const next = items.concat(item);
    if (!write(next)) return null;
    items = next;
    panelOpen = true;
    render();
    if (online()) schedule(2000);
    return item;
  }

  function queueableError(err) {
    return !err || !err.status || err.status >= 500 || err.status === 408 || err.status === 429;
  }

  function markOnline() {
    unreachable = false;
    render();
    schedule(250);
  }

  function markUnreachable() {
    unreachable = true;
    render();
  }

  function schedule(delay) {
    clearTimeout(syncTimer);
    if (!online() || !state.user || !items.length) return;
    syncTimer = setTimeout(() => sync(false), Math.max(0, delay || 0));
  }

  function scheduleNext() {
    const waiting = items.filter((item) => item.status === 'pending');
    if (!waiting.length || !online()) return;
    const next = Math.min(...waiting.map((item) => item.nextAttemptAt || 0));
    schedule(Math.max(0, next - Date.now()));
  }

  async function sync(manual) {
    load();
    if (syncing || !items.length || !online() || !state.user || !can('scan:use')) return;
    syncing = true;
    clearTimeout(syncTimer);
    render();
    let synced = 0;
    let failed = 0;
    let deferredUntil = 0;
    const candidates = items.filter((item) => item.status === 'pending'
      && (manual || !item.nextAttemptAt || item.nextAttemptAt <= Date.now()));

    for (const candidate of candidates) {
      try {
        await api('POST', '/api/v1/scan', {
          code: candidate.code,
          clientEventId: candidate.id,
          scannedAt: candidate.scannedAt,
        });
        unreachable = false;
        if (!remove(candidate.id)) throw new Error('Could not update the local scan queue');
        synced++;
      } catch (err) {
        if (queueableError(err)) {
          const attempts = candidate.attempts + 1;
          const delay = Math.min(MAX_BACKOFF, 5000 * Math.pow(2, Math.min(attempts - 1, 6)));
          deferredUntil = Date.now() + delay;
          unreachable = true;
          update(candidate.id, {
            attempts,
            nextAttemptAt: deferredUntil,
            status: 'pending',
            lastError: 'Server unreachable; retry automatically',
          });
          break;
        }
        if (err.status === 401) {
          update(candidate.id, { nextAttemptAt: 0, lastError: 'Sign in to continue syncing' });
          break;
        }
        failed++;
        update(candidate.id, {
          attempts: candidate.attempts + 1,
          nextAttemptAt: 0,
          status: 'failed',
          lastError: err.message || 'Sync rejected',
        });
      }
    }

    syncing = false;
    render();
    if (synced) toast(`Synced ${synced} offline scan${synced === 1 ? '' : 's'}${failed ? `; ${failed} need attention` : ''}`);
    else if (manual && failed) toast(`${failed} scan${failed === 1 ? '' : 's'} need attention`, true);
    if (deferredUntil) schedule(deferredUntil - Date.now());
    else scheduleNext();
  }

  function retry(id) {
    const item = items.find((scan) => scan.id === id);
    if (!item) return;
    update(id, { status: 'pending', attempts: 0, nextAttemptAt: 0, lastError: '' });
    sync(true);
  }

  function discard(id) {
    const item = items.find((scan) => scan.id === id);
    if (!item || !window.confirm(`Discard queued scan ${item.code}? This cannot be undone.`)) return;
    remove(id);
  }

  function render() {
    const status = document.getElementById('scan-network');
    const toggle = document.getElementById('scan-queue-toggle');
    const syncButton = document.getElementById('scan-sync');
    const panel = document.getElementById('scan-queue-panel');
    const list = document.getElementById('scan-queue-list');
    if (!status || !toggle || !syncButton || !panel || !list) return;

    const failed = items.filter((item) => item.status === 'failed').length;
    const mode = !online() ? 'offline' : unreachable ? 'degraded' : 'online';
    status.className = 'scan-network ' + mode;
    status.innerHTML = `<span class="scan-network-dot" aria-hidden="true"></span>${!online() ? 'Offline' : unreachable ? 'Server unreachable' : 'Online'}`;
    toggle.textContent = `${items.length} queued${failed ? ` · ${failed} failed` : ''}`;
    toggle.classList.toggle('has-failed', failed > 0);
    toggle.setAttribute('aria-expanded', String(panelOpen && items.length > 0));
    syncButton.classList.toggle('hidden', !items.length || !online());
    syncButton.disabled = syncing;
    syncButton.textContent = syncing ? 'Syncing…' : 'Sync now';
    if (!items.length) panelOpen = false;
    panel.classList.toggle('hidden', !panelOpen || !items.length);

    list.innerHTML = items.map((item) => {
      const when = new Date(item.scannedAt).toLocaleString();
      const stateLabel = item.status === 'failed' ? 'Needs attention'
        : item.attempts ? `Retry ${item.attempts} scheduled` : 'Waiting to sync';
      return `<li class="scan-queue-item ${item.status === 'failed' ? 'failed' : ''}">
        <div class="scan-queue-copy">
          <strong class="mono">${esc(item.code)}</strong>
          <span>${esc(when)} · ${esc(stateLabel)}</span>
          ${item.lastError ? `<small>${esc(item.lastError)}</small>` : ''}
        </div>
        <div class="scan-queue-actions">
          <button type="button" class="btn btn-ghost btn-sm" data-scan-retry="${esc(item.id)}" ${syncing || !online() ? 'disabled' : ''}>Retry</button>
          <button type="button" class="btn btn-ghost btn-sm bad-t" data-scan-discard="${esc(item.id)}" ${syncing ? 'disabled' : ''}>Discard</button>
        </div>
      </li>`;
    }).join('');
  }

  function registerShell() {
    if (shellRegistered || !('serviceWorker' in navigator)) return;
    shellRegistered = true;
    navigator.serviceWorker.register('/sw.js').catch(() => {
                                                                            
    });
  }

  function activate() {
    load();
    registerShell();
    render();
    if (online() && state.user) schedule(750);
  }

  function init() {
    if (initialized) return;
    initialized = true;
    window.addEventListener('offline', () => { clearTimeout(syncTimer); render(); });
    window.addEventListener('online', () => { unreachable = false; render(); schedule(250); });
    window.addEventListener('storage', (event) => {
      if (event.key === storageKey()) load(true);
    });
    document.getElementById('scan-queue-toggle').addEventListener('click', () => {
      panelOpen = !panelOpen;
      render();
    });
    document.getElementById('scan-sync').addEventListener('click', () => sync(true));
    document.getElementById('scan-queue-list').addEventListener('click', (event) => {
      const retryButton = event.target.closest('[data-scan-retry]');
      const discardButton = event.target.closest('[data-scan-discard]');
      if (retryButton) retry(retryButton.dataset.scanRetry);
      else if (discardButton) discard(discardButton.dataset.scanDiscard);
    });
    setInterval(() => { if (online() && items.length) sync(false); }, 15000);
  }

  return {
    init,
    activate,
    create,
    enqueue,
    sync,
    markOnline,
    markUnreachable,
    isOffline: () => !online(),
    queueableError,
    count: () => items.length,
  };
})();

window.OfflineScanner = OfflineScanner;
OfflineScanner.init();
