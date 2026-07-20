# OpenJourney JavaScript SDK

A lightweight, dependency-free SDK for integrating OpenJourney in-app messaging, web push, and event tracking into your web application.

## Installation

```bash
npm install @openjourney/browser
```

## Quick Start

### 1. Initialize the SDK

```javascript
import { OpenJourney } from '@openjourney/browser';

const oj = new OpenJourney({
  endpoint: 'https://api.example.com',
  apiKey: 'pk_web_your_public_key',
});
```

### 2. Track Events

```javascript
// Track a custom event
oj.track('user.signup', {
  plan: 'professional',
  referrer: 'newsletter',
});

// Identify a user
oj.identify('user_123', {
  name: 'Jane Doe',
  email: 'jane@example.com',
});

// Track an attribution event
oj.track('purchase', {
  product_id: 'sku_456',
  amount: 29.99,
});

// Manage consent
oj.setConsent('email', 'subscribed');
oj.setConsent('push', 'unsubscribed');
```

### 3. Fetch and Display In-App Messages

```javascript
// Fetch the inbox (anonymous user)
const messages = await oj.fetchInbox();
messages.forEach(msg => {
  console.log(`Message: ${msg.content.title}`);
  console.log(`Type: ${msg.message_type}`);
});

// For identified users, fetch with a server-minted token
const token = await fetch('/api/signin-app-token', {
  method: 'POST',
  body: JSON.stringify({ external_id: user.id }),
}).then(r => r.json()).then(d => d.token);

const messages = await oj.fetchInbox(token);
```

### 4. Report Engagement

```javascript
// Report when a user sees a message
await oj.reportImpression(messageId);

// Report when a user clicks a message
await oj.reportClick(messageId);

// Report when a user dismisses a message
await oj.reportDismiss(messageId);

// For identified users, pass the token
await oj.reportImpression(messageId, token);
await oj.reportClick(messageId, token);
await oj.reportDismiss(messageId, token);
```

## Web Push Setup

OpenJourney supports web push notifications via the VAPID wake-signal model: the push itself carries no payload, but signals your Service Worker to fetch the actual message from the in-app inbox.

### 1. Register the Service Worker

Copy `sw-webpush.example.js` to your app root (e.g., `public/sw.js`):

```javascript
cp node_modules/@openjourney/browser/sw-webpush.example.js public/sw.js
```

Register it in your main app:

```javascript
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js');
}
```

### 2. Configure the Service Worker

Edit `public/sw.js` and update the `CONFIG` object with your settings:

```javascript
const CONFIG = {
  apiEndpoint: 'https://api.example.com',
  apiKey: 'pk_web_your_public_key',
  defaultNotificationOptions: {
    icon: '/icon-192x192.png',
    badge: '/badge-72x72.png',
    requireInteraction: false,
  },
  fetchTimeout: 5000,
  messageTagPrefix: 'openjourney-msg-',
};
```

### 3. Subscribe to Push Notifications

Retrieve your VAPID public key from your OpenJourney dashboard and convert it to a Uint8Array:

```javascript
// Your VAPID public key (base64url-encoded)
const vapidPublicKey = 'YOUR_VAPID_PUBLIC_KEY_BASE64URL';

// Convert to Uint8Array
function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - base64String.length % 4) % 4);
  const base64 = (base64String + padding)
    .replace(/\-/g, '+')
    .replace(/_/g, '/');
  const rawData = window.atob(base64);
  const outputArray = new Uint8Array(rawData.length);
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i);
  }
  return outputArray;
}

// Subscribe
const registration = await navigator.serviceWorker.ready;
const subscription = await registration.pushManager.subscribe({
  userVisibleOnly: true,
  applicationServerKey: urlBase64ToUint8Array(vapidPublicKey),
});

// Send subscription to your backend, which then calls OpenJourney's API:
// POST /v1/device-tokens
// {
//   "platform": "web",
//   "provider": "webpush",
//   "token": subscription.endpoint,
//   "profile_id": "user_123"  // optional for identified users
// }
```

### Service Worker Features

The reference Service Worker (`sw-webpush.example.js`) provides:

- **Push handling:** When a VAPID wake signal arrives, fetches the in-app inbox and displays the first message as a notification.
- **Engagement tracking:** Reports impression (when shown), click (when opened), and dismiss (when closed) events.
- **Customizable notifications:** Reads message content (title, body, image, icon) and shows platform notifications.
- **Action routing:** Supports `action_url` in message payload to navigate to a specific URL on click.
- **Anonymous ID persistence:** Uses IndexedDB to maintain a durable anonymous ID across app reloads.

### How the Wake-Signal Model Works

1. **Message delivery:** When an in-app message is triggered for a web push recipient, OpenJourney sends a VAPID-signed wake signal (empty body) to the subscription endpoint.
2. **Service Worker receives:** Your Service Worker's `push` event handler fires and calls `fetchAndShowMessage()`.
3. **Fetch inbox:** The handler fetches the in-app inbox from the OpenJourney public edge.
4. **Display:** The first undismissed message is displayed as a notification.
5. **Engagement:** When the user interacts with the notification, the handler reports impression/click/dismiss events to the backend.

This approach avoids payload encryption complexity while ensuring your app stays in sync with the latest inbox state.

## SDK API Reference

### Constructor Options

```typescript
type ClientOptions = {
  endpoint: string;           // OpenJourney API endpoint
  apiKey: string;             // Public API key
  batchSize?: number;         // Event batch size (default 25, max 75)
  flushIntervalMs?: number;   // Flush interval in ms (default 10000)
  storage?: Storage;          // localStorage or custom impl (default: localStorage)
  fetch?: typeof fetch;       // Custom fetch impl (default: globalThis.fetch)
  now?: () => Date;           // Custom Date source
  randomUUID?: () => string;  // Custom UUID source
};
```

### Methods

#### `identify(externalID: string, attributes?: Record<string, unknown>): string | undefined`

Identify the current user with an external ID and optional attributes.

#### `reset(): void`

Clear the current user identity and generate a new anonymous ID.

#### `getAnonymousId(): string`

Get the current anonymous ID.

#### `getExternalId(): string | undefined`

Get the current external ID (if identified).

#### `track(eventType: string, payload?: Record<string, unknown>, options?: {...}): string`

Track a custom event. Returns the event's idempotency key.

#### `setAttributes(attributes: Record<string, unknown>): string`

Set user attributes (shorthand for `track('profile.updated', { attributes })`).

#### `alias(namespace: string, value: string): string`

Create an identity alias.

#### `merge(sourceExternalID: string): string`

Merge another external ID into the current user.

#### `setConsent(channel: string, state: 'subscribed' | 'unsubscribed', options?: {...}): string`

Record a consent change (e.g., email subscription, push opt-in).

#### `async fetchInbox(token?: string): Promise<InAppMessage[]>`

Fetch the user's in-app inbox. For identified users, pass the server-minted `SignInAppToken`.

#### `async reportImpression(messageId: string, token?: string): Promise<void>`

Report that a message was displayed.

#### `async reportClick(messageId: string, token?: string): Promise<void>`

Report that a message was clicked.

#### `async reportDismiss(messageId: string, token?: string): Promise<void>`

Report that a message was dismissed.

#### `async flush(): Promise<void>`

Manually flush queued events.

#### `destroy(): void`

Stop the SDK and clear timers.

## Message Type

```typescript
type InAppMessage = {
  id: string;
  message_type: "modal" | "banner" | "fullscreen" | "card";
  content: Record<string, unknown>;
  status: string;
  rank: number;
  categories: string[];
  start_at: string;
  expires_at?: string | null;
  displayed_at?: string | null;
  clicked_at?: string | null;
  dismissed_at?: string | null;
};
```

## Error Handling

The SDK throws errors for invalid input and network failures. Wrap async calls in try-catch:

```javascript
try {
  const messages = await oj.fetchInbox(token);
} catch (err) {
  if (err.message.includes('Unauthorized')) {
    console.log('Token invalid or expired');
  } else {
    console.error('Inbox fetch failed:', err);
  }
}
```

## Development

### Building

```bash
npm run build
```

### Testing

```bash
npm test
```

### Type Checking

TypeScript types are included in the distribution.

## Browser Support

Requires ES2020+. Tested on:
- Chrome/Edge 92+
- Firefox 89+
- Safari 15+

## License

See LICENSE file.
