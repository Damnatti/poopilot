const CACHE_NAME = 'poopilot-v5';
const SHELL_URLS = [
  '/',
  '/index.html',
  '/css/app.css',
  '/js/app.js',
  '/js/rtc.js',
  '/js/terminal.js',
  '/js/approval.js',
  '/js/protocol.js',
  '/vendor/xterm.min.js',
  '/vendor/xterm.min.css',
  '/vendor/xterm-addon-fit.min.js',
  '/manifest.json',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(SHELL_URLS))
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    )
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // Don't cache API calls (relay, offer, answer)
  if (url.pathname.startsWith('/relay/') || url.pathname === '/offer' || url.pathname === '/answer') {
    return;
  }

  event.respondWith(
    fetch(event.request)
      .then((resp) => {
        const clone = resp.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
        return resp;
      })
      .catch(() =>
        caches.match(event.request).then((cached) =>
          cached || new Response('Offline', { status: 503 })
        )
      )
  );
});
