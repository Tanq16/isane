const VAPID_KEY_URL = '/api/push/vapid-key';
const SUBSCRIBE_URL = '/api/push/subscribe';
const ICON_URL = '/static/icons/icon-192.png';

function decodeKey(value) {
    const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), '=');
    const raw = self.atob(padded);
    const bytes = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) {
        bytes[i] = raw.charCodeAt(i);
    }
    return bytes;
}

async function present(event) {
    let payload = null;
    try {
        payload = event.data ? event.data.json() : null;
    } catch {
        payload = null;
    }

    const n = (payload && payload.notification) || {};
    const x = (payload && payload.x) || {};

    const options = {
        body: n.body || '',
        icon: ICON_URL,
        badge: ICON_URL,
        data: { navigate: n.navigate || '/', ...x },
    };
    if (x.container_id) {
        options.tag = x.container_id;
        options.renotify = true;
    }

    await self.registration.showNotification(n.title || 'Isane', options);

    if (n.app_badge !== undefined && self.navigator.setAppBadge) {
        const count = Number(n.app_badge);
        if (count > 0) {
            await self.navigator.setAppBadge(count);
        } else if (self.navigator.clearAppBadge) {
            await self.navigator.clearAppBadge();
        }
    }
}

async function focusOrOpen(url) {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of windows) {
        if (client.url.startsWith(self.registration.scope)) {
            client.navigate(url);
            return client.focus();
        }
    }
    return self.clients.openWindow(url);
}

function keyOf(body) {
    if (typeof body === 'string') {
        return body.trim() || null;
    }
    if (!body || typeof body !== 'object') {
        return null;
    }
    return body.key || body.vapid_public_key || body.public_key || null;
}

async function resubscribe() {
    const response = await fetch(VAPID_KEY_URL);
    if (!response.ok) {
        return;
    }
    const key = keyOf(await response.json());
    if (!key) {
        return;
    }
    const subscription = await self.registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: decodeKey(key),
    });
    const json = subscription.toJSON();
    if (!json.endpoint || !json.keys || !json.keys.p256dh || !json.keys.auth) {
        return;
    }
    await fetch(SUBSCRIBE_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            endpoint: json.endpoint,
            keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
        }),
    });
}

self.addEventListener('push', (event) => {
    event.waitUntil(present(event));
});

self.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const data = event.notification.data || {};
    event.waitUntil(focusOrOpen(data.navigate || '/'));
});

self.addEventListener('pushsubscriptionchange', (event) => {
    event.waitUntil(resubscribe());
});
