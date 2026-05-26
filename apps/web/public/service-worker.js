// Coding Anywhere service worker.
//
// Entry points:
//   - install      Mark this version active immediately.
//   - activate     Take over the page so new clients receive push.
//   - push         Render a system notification from the JSON payload.
//   - notificationclick   Focus an existing tab or open the URL.

self.addEventListener('install', () => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener('push', (event) => {
  let payload = { title: 'Coding Anywhere', body: '' };
  try {
    if (event.data) payload = { ...payload, ...event.data.json() };
  } catch {
    // ignore — fall back to defaults.
  }
  const title = payload.title || 'Coding Anywhere';
  const options = {
    body: payload.body || '',
    tag: payload.tag || 'coding-anywhere',
    data: { url: payload.url || '/' },
    icon: payload.icon || '/favicon-192x192.png',
    badge: payload.badge || '/favicon-32x32.png',
    renotify: true,
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = (event.notification.data && event.notification.data.url) || '/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((all) => {
      for (const client of all) {
        try {
          const u = new URL(client.url);
          if (u.origin === self.location.origin) {
            client.focus();
            client.postMessage({ type: 'navigate', url: target });
            return;
          }
        } catch {
          // continue
        }
      }
      return self.clients.openWindow(target);
    }),
  );
});
