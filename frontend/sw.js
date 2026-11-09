'use strict';

const CACHE = 'cockpit-scan-shell-v8';
const SHELL = [
  '/',
  '/index.html',
  '/styles.css',
  '/fonts/Inter-var.woff2',
  '/fonts/Inter-italic-var.woff2',
  '/fonts/SpaceGrotesk-var.woff2',
  '/fonts/JetBrainsMono-var.woff2',
  '/scan.html',
  '/js/scan.js',
  '/js/core.js',
  '/js/offline-scanner.js',
  '/js/notifications.js',
  '/js/auth.js',
  '/js/orders.js',
  '/js/ordercreate.js',
  '/js/detail.js',
  '/js/checklists.js',
  '/js/manuals.js',
  '/js/work.js',
  '/js/workload.js',
  '/js/dashboard.js',
  '/js/dayplan.js',
  '/js/quality.js',
  '/js/qcbench.js',
  '/js/si.js',
  '/js/users.js',
  '/js/devices.js',
  '/js/config.js',
  '/js/capa.js',
  '/js/automations.js',
  '/js/app.js',
  '/js/order-pages.js',
  '/js/ui.js',
];

function cacheKey(request) {
  const url = new URL(request.url);
  url.search = '';
  url.hash = '';
  return url.href;
}

self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE);
    await Promise.allSettled(SHELL.map((path) => cache.add(path)));
    await self.skipWaiting();
  })());
});

self.addEventListener('activate', (event) => {
  event.waitUntil(caches.keys().then((keys) => Promise.all(keys
    .filter((key) => key.startsWith('cockpit-scan-shell-') && key !== CACHE)
    .map((key) => caches.delete(key)))).then(() => self.clients.claim()));
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (request.method !== 'GET') return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;

  if (request.mode === 'navigate') {
    event.respondWith((async () => {
      try {
        return await fetch(request);
      } catch {
        return (await caches.match(url.pathname)) || (await caches.match('/index.html')) || Response.error();
      }
    })());
    return;
  }

  event.respondWith((async () => {
    const key = cacheKey(request);
    const cached = await caches.match(key);
    if (cached) return cached;
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(CACHE);
      await cache.put(key, response.clone());
    }
    return response;
  })());
});
