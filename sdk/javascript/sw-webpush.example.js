/**
 * sw-webpush.example.js — Reference Service Worker for OpenJourney web push
 *
 * This is a minimal example Service Worker that handles push notifications
 * via the web push wake-signal model: the push itself carries no content,
 * but signals the Service Worker to fetch the actual message from the
 * OpenJourney in-app inbox edge.
 *
 * Usage:
 * 1. Copy this file to your app root as sw.js (or similar)
 * 2. Register in your main app:
 *    if ('serviceWorker' in navigator) {
 *      navigator.serviceWorker.register('/sw.js');
 *    }
 * 3. On app load, call registerSubscription() to subscribe to push:
 *    const subscription = await navigator.serviceWorker.ready
 *      .then(r => r.pushManager.subscribe({
 *        userVisibleOnly: true,
 *        applicationServerKey: vapidPublicKeyAsUint8Array,
 *      }));
 *    await openjourney.registerSubscription(subscription, profileID);
 * 4. Incoming push events trigger the 'push' handler below, which fetches
 *    and displays the message.
 */

/**
 * ============================================================================
 * Configuration
 * ============================================================================
 * Customize these values for your deployment.
 */

const CONFIG = {
  // OpenJourney backend URL (no trailing slash)
  apiEndpoint: 'https://api.example.com',

  // Public API key for the web SDK (messages:read scope)
  apiKey: 'pk_web_...',

  // Default notification options (can be overridden per message)
  defaultNotificationOptions: {
    icon: '/icon-192x192.png',
    badge: '/badge-72x72.png',
    requireInteraction: false,
  },

  // Fetch timeout (ms)
  fetchTimeout: 5000,

  // Message dedupe key (avoid showing same message twice)
  messageTagPrefix: 'openjourney-msg-',
};

/**
 * ============================================================================
 * Push Event Handler
 * ============================================================================
 * When a push wake signal arrives (empty body), fetch the inbox and show
 * the first message.
 */

self.addEventListener('push', (event) => {
  console.log('[ServiceWorker] push event received');

  event.waitUntil(
    fetchAndShowMessage().catch((err) => {
      console.error('[ServiceWorker] push handler error:', err);
      // Optionally show a generic notification on error
      return self.registration.showNotification('New message', {
        body: 'Tap to open',
        icon: CONFIG.defaultNotificationOptions.icon,
        badge: CONFIG.defaultNotificationOptions.badge,
      });
    })
  );
});

/**
 * ============================================================================
 * Notification Click Handler
 * ============================================================================
 * When user clicks the notification, navigate to the app (or a specific URL
 * if the message includes an action_url).
 */

self.addEventListener('notificationclick', (event) => {
  console.log('[ServiceWorker] notification clicked:', event.notification.tag);

  event.notification.close();

  const url = event.notification.data?.action_url || '/';

  event.waitUntil(
    // If app window is already open, focus it; otherwise open new
    clients
      .matchAll({ type: 'window', includeUncontrolled: true })
      .then((clientList) => {
        for (const client of clientList) {
          if (client.url === url && 'focus' in client) {
            // Report engagement: click
            reportEngagement(event.notification.data?.id, 'click').catch(
              (err) => console.error('Failed to report click:', err)
            );
            return client.focus();
          }
        }
        // No matching window; open new one
        if (clients.openWindow) {
          reportEngagement(event.notification.data?.id, 'click').catch(
            (err) => console.error('Failed to report click:', err)
          );
          return clients.openWindow(url);
        }
      })
  );
});

/**
 * ============================================================================
 * Notification Close Handler (optional)
 * ============================================================================
 * If you want to report "dismissed" when user closes the notification
 * without clicking it, add this handler. Note: some browsers don't support
 * the 'notificationclose' event.
 */

self.addEventListener('notificationclose', (event) => {
  console.log('[ServiceWorker] notification dismissed:', event.notification.tag);

  const msgID = event.notification.data?.id;
  if (msgID) {
    reportEngagement(msgID, 'dismiss').catch((err) =>
      console.error('Failed to report dismiss:', err)
    );
  }
});

/**
 * ============================================================================
 * Helpers
 * ============================================================================
 */

/**
 * Fetch the inbox and display the first message as a notification.
 */
async function fetchAndShowMessage() {
  const messages = await fetchInbox();

  if (!messages || messages.length === 0) {
    console.log('[ServiceWorker] inbox is empty');
    return;
  }

  const msg = messages[0];
  console.log('[ServiceWorker] showing message:', msg.id);

  const opts = {
    ...CONFIG.defaultNotificationOptions,
    body: msg.content.body || 'New message',
    image: msg.content.image_url,
    icon: msg.content.icon_url || CONFIG.defaultNotificationOptions.icon,
    badge: msg.content.badge_url || CONFIG.defaultNotificationOptions.badge,
    tag: `${CONFIG.messageTagPrefix}${msg.id}`,
    data: {
      id: msg.id,
      action_url: msg.content.action_url,
    },
  };

  // Show notification
  await self.registration.showNotification(msg.content.title, opts);

  // Optionally report impression (message shown)
  await reportEngagement(msg.id, 'impression').catch((err) =>
    console.error('Failed to report impression:', err)
  );
}

/**
 * Fetch the in-app inbox (up to 10 messages).
 */
async function fetchInbox() {
  const controller = new AbortController();
  const timeoutID = setTimeout(() => controller.abort(), CONFIG.fetchTimeout);

  try {
    const anonymousID = getOrCreateAnonymousID();
    const url = new URL('/v1/messages/inbox', CONFIG.apiEndpoint);
    url.searchParams.set('limit', '10');

    const resp = await fetch(url.toString(), {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${CONFIG.apiKey}`,
        'X-SDK-Anonymous-ID': anonymousID,
      },
      signal: controller.signal,
    });

    clearTimeout(timeoutID);

    if (!resp.ok) {
      throw new Error(
        `fetchInbox failed: ${resp.status} ${resp.statusText}`
      );
    }

    return await resp.json();
  } catch (err) {
    clearTimeout(timeoutID);
    throw err;
  }
}

/**
 * Report engagement: impression, click, or dismiss.
 */
async function reportEngagement(messageID, action) {
  if (!messageID) return;

  const controller = new AbortController();
  const timeoutID = setTimeout(() => controller.abort(), CONFIG.fetchTimeout);

  try {
    const anonymousID = getOrCreateAnonymousID();
    const url = new URL(
      `/v1/messages/${messageID}/${action}`,
      CONFIG.apiEndpoint
    );

    const resp = await fetch(url.toString(), {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${CONFIG.apiKey}`,
        'X-SDK-Anonymous-ID': anonymousID,
      },
      body: JSON.stringify({}),
      signal: controller.signal,
    });

    clearTimeout(timeoutID);

    if (!resp.ok) {
      throw new Error(
        `reportEngagement(${action}) failed: ${resp.status} ${resp.statusText}`
      );
    }

    console.log(`[ServiceWorker] reported ${action} for message ${messageID}`);
  } catch (err) {
    clearTimeout(timeoutID);
    throw err;
  }
}

/**
 * Get or create a durable anonymous ID for this browser.
 * Stores it in IndexedDB so it persists across app reloads.
 */
async function getOrCreateAnonymousID() {
  const db = new Promise((resolve, reject) => {
    const req = indexedDB.open('openjourney', 1);
    req.onerror = () => reject(req.error);
    req.onsuccess = () => resolve(req.result);
    req.onupgradeneeded = () => {
      req.result.createObjectStore('state');
    };
  });

  const objStore = (await db).transaction('state').objectStore('state');
  let id = await new Promise((resolve, reject) => {
    const req = objStore.get('anonymous_id');
    req.onerror = () => reject(req.error);
    req.onsuccess = () => resolve(req.result?.value);
  });

  if (!id) {
    id = self.crypto.randomUUID
      ? self.crypto.randomUUID()
      : `anon-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    await new Promise((resolve, reject) => {
      const req = (await db)
        .transaction('state', 'readwrite')
        .objectStore('state')
        .put({ value: id }, 'anonymous_id');
      req.onerror = () => reject(req.error);
      req.onsuccess = () => resolve();
    });
  }

  return id;
}

/**
 * ============================================================================
 * Subscription Registration Helper (call from main app thread)
 * ============================================================================
 * Usage:
 *   const registration = await navigator.serviceWorker.ready;
 *   const subscription = await registration.pushManager.subscribe({
 *     userVisibleOnly: true,
 *     applicationServerKey: vapidPublicKeyAsUint8Array,
 *   });
 *   await registerSubscription(subscription, profileID);
 */

async function registerSubscription(subscription, profileID) {
  const controller = new AbortController();
  const timeoutID = setTimeout(() => controller.abort(), CONFIG.fetchTimeout);

  try {
    const anonymousID = getOrCreateAnonymousID();
    const resp = await fetch(
      new URL('/v1/device-tokens', CONFIG.apiEndpoint).toString(),
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${CONFIG.apiKey}`,
        },
        body: JSON.stringify({
          profile_id: profileID || 'anon-' + anonymousID,
          platform: 'web',
          provider: 'webpush',
          token: subscription.endpoint,
        }),
        signal: controller.signal,
      }
    );

    clearTimeout(timeoutID);

    if (!resp.ok) {
      throw new Error(
        `registerSubscription failed: ${resp.status} ${resp.statusText}`
      );
    }

    console.log('[app] subscription registered');
  } catch (err) {
    clearTimeout(timeoutID);
    console.error('[app] registerSubscription error:', err);
    throw err;
  }
}

// Export for app to use (if running in a module context)
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { registerSubscription };
}
