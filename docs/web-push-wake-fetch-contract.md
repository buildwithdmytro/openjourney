# Web Push Wake-Signal Flow & In-App Inbox Fetch

## Overview

Web push in OpenJourney v1 is a **wake signal** model: the push notification itself contains no content payload. Instead, it serves as a signal to wake the browser, where a Service Worker fetches the actual message content from the in-app inbox edge.

This design:
- Eliminates payload encryption complexity (no RFC 8291 AES-128-GCM in v1)
- Uses VAPID (RFC 8292) JWT authentication over HTTPS only
- Keeps push messages small and simple
- Offloads content rendering to the client

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│ OpenJourney Backend                                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  1. Journey/Campaign triggers in-app message                     │
│     delivery via policy.Evaluate (suppression, fatigue, consent) │
│                                                                   │
│  2. DeliverNext sends via webpush provider:                      │
│     - Resolves subscription endpoint from device_tokens         │
│     - BuildRequest creates VAPID JWT (P-256 ECDSA)              │
│     - POSTs to subscription endpoint with TTL header            │
│     - Empty body (wake signal)                                  │
│                                                                   │
│  3. Engagement events: impression/click/dismiss                 │
│     - Client reports via POST /v1/messages/{id}/impression      │
│     - Stored as accepted events, projected to display state     │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
                        ↓ (HTTPS + VAPID)
┌─────────────────────────────────────────────────────────────────┐
│ Web Push Service (e.g., FCM, APNS, Browser Push Service)        │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│ Receives VAPID-signed push, routes to browser                   │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
                        ↓ (Browser Push API)
┌─────────────────────────────────────────────────────────────────┐
│ Browser / Web App                                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  1. Service Worker 'push' event fires (empty body)              │
│                                                                   │
│  2. Service Worker: fetchInbox()                                │
│     - GET /v1/messages/inbox                                   │
│     - Include anonymous_id or SignInAppToken                   │
│     - Bounded result (e.g., LIMIT 10)                          │
│                                                                   │
│  3. Service Worker receives in-app message(s)                   │
│     - Renders notification from first undismissed message      │
│     (or shows custom UX: badge, banner, native notification)    │
│                                                                   │
│  4. User interacts: click → reports engagement                  │
│     - POST /v1/messages/{id}/impression (already shown)        │
│     - POST /v1/messages/{id}/click (user clicked)              │
│     - POST /v1/messages/{id}/dismiss (user dismissed)          │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Device Token Registration

A web subscription is registered like any other device token:

```http
POST /v1/device-tokens
Authorization: Bearer <api-key>
Content-Type: application/json

{
  "profile_id": "user-123-uuid",
  "platform": "web",
  "provider": "webpush",
  "token": "https://push.example.com/v1/push/abc123def456..."
}
```

The `token` field holds the **subscription endpoint** (a full HTTPS URL) returned by the browser's `PushManager.subscribe()`.

The SDK's `registerSubscription()` helper handles this:

```javascript
const subscription = await serviceWorkerRegistration.pushManager.subscribe({
  userVisibleOnly: true,
  applicationServerKey: vapidPublicKey,
});

// SDK registers with backend
await openjourney.registerSubscription(subscription, profile.id);
```

## Push Wake Signal

When a message is sent via the `webpush` provider:

1. **Sending Identity** stores VAPID keys:
   - `channel='push'`, `provider='webpush'`
   - `config.vapid_private_ref` → private key (secret reference)
   - `config.vapid_public_ref` → public key (secret reference)
   - `config.vapid_subject` → mailto or URI for VAPID header

2. **BuildRequest** (RFC 8292):
   ```
   POST <subscription-endpoint>
   Authorization: vapid t=<JWT>,k=<base64-public-key>
   TTL: 24
   Content-Length: 0
   
   <empty body>
   ```

3. **JWT Claims**:
   ```json
   {
     "sub": "mailto:sender@example.com",
     "iat": 1626000000,
     "exp": 1626000300
   }
   ```
   - Signed with P-256 ECDSA private key
   - Expiry: typically 12 hours (configurable)

4. **Response**:
   - `201 Created` or `2xx` → message delivered
   - `404 Not Found` or `410 Gone` → subscription invalid → retire device token
   - `4xx` or `5xx` other → retry/fail per delivery attempt state machine

## In-App Inbox Edge

The public inbox edge is at `GET /v1/messages/inbox`:

```http
GET /v1/messages/inbox?limit=10&token=<SignInAppToken>
X-SDK-Anonymous-ID: <uuid>
```

**Query Parameters:**
- `limit` (optional, default 50, max 100) — number of messages to fetch
- `token` (optional) — for known subjects (`external_id`); mirrors `SignInAppToken` from `SignFormToken`

**Authentication:**
- **Anonymous subject** (high-entropy `anonymous_id`):
  - SDK public `messages:read` key + `X-SDK-Anonymous-ID` header
  - Scoped to the app derived from the key
  - Returns only that `anonymous_id`'s inbox

- **Known subject** (`external_id`):
  - Requires `SignInAppToken(tenant, app, subject, exp, secret)` (HMAC-SHA256, base64url)
  - Token is minted by the customer backend (e.g., at login)
  - Verified server-side against the shared secret
  - Returns only that `external_id`'s inbox (IDOR-safe)

**Rate Limiting:**
- `IPRateLimiter` applies (transparent 429 on abuse)
- Configurable per-IP token bucket

**Response** (200 OK):
```json
[
  {
    "id": "msg-uuid",
    "profile_id": "prof-uuid",
    "message_type": "card",
    "content": {
      "title": "...",
      "body": "...",
      "image_url": "...",
      "action_url": "..."
    },
    "rank": 10,
    "categories": ["onboarding"],
    "start_at": "2024-06-10T12:00:00Z",
    "expires_at": "2024-06-20T12:00:00Z",
    "status": "delivered",
    "displayed_at": null,
    "clicked_at": null,
    "dismissed_at": null,
    "created_at": "2024-06-10T12:00:00Z",
    "updated_at": "2024-06-10T12:00:00Z"
  }
]
```

**Filtering (automatic):**
- Only `dismissed_at IS NULL` (undismissed)
- Only `start_at <= now() AND (expires_at IS NULL OR expires_at > now())` (in-window)
- Ordered by `rank DESC` (highest rank first)

## Engagement Reporting

The public engagement edge is at `POST /v1/messages/{id}/{action}`:

**Actions:**
- `impression` — message shown to user (first view)
- `click` — user clicked the message
- `dismiss` — user dismissed the message

**Request:**
```http
POST /v1/messages/msg-uuid/click
Authorization: Bearer <api-key>
X-SDK-Anonymous-ID: <uuid>
X-SDK-Timestamp: <unix-ms>

{}
```

Or for known subjects:
```http
POST /v1/messages/msg-uuid/click?token=<SignInAppToken>
X-SDK-Timestamp: <unix-ms>

{}
```

**Response** (204 No Content or 200 OK)

**Under the hood:**
1. Validates message belongs to caller's subject (IDOR check)
2. Emits `domain.Event` of type `message.impression|clicked|dismissed`
3. Calls `Store.AcceptEvents` with public principal (`ActorType:"public"`)
4. Projector picks up event → stamps `displayed_at`/`clicked_at`/`dismissed_at` on `inapp_messages` row
5. State transitions are monotonic (never regress, e.g., dismissed→displayed blocked)

## Deduplication & Idempotency

- **Device token registration**: `UNIQUE (tenant_id, app_id, token)` ensures one subscription per endpoint per app; re-registering updates `last_seen_at`
- **Engagement reports**: multiple reports of the same action are idempotent (timestamp checks prevent regressing state)

## Security Properties

1. **No raw payloads**: wake signal is empty; content fetched server-to-client over HTTPS
2. **No payload encryption**: VAPID is TLS only (RFC 8292), not RFC 8291 (which requires encryption key exchange)
3. **IDOR-safe**: `external_id` inbox requires a server-minted, short-lived token bound to the subject
4. **SSRF-safe**: WebPush outbound uses guarded HTTP transport (`NewHTTPProviderAdapter`)
5. **Rate-limited**: public edge uses `IPRateLimiter`
6. **Consent + suppression**: fatigue caps, suppression, and consent apply to in-app (shared policy path)
7. **Event-sourced**: engagement state is immutable, append-only at the event layer; state transitions are monotonic

## Example: Reference Service Worker

See `sdk/javascript/sw-webpush.example.js` for a complete reference implementation:

```javascript
// In the push event handler, fetch the inbox and show a notification
self.addEventListener('push', async (event) => {
  const inbox = await openjourney.fetchInbox();
  if (inbox.length === 0) {
    console.log('No messages in inbox');
    return;
  }

  const msg = inbox[0];
  const opts = {
    body: msg.content.body,
    image: msg.content.image_url,
    icon: msg.content.icon_url,
    badge: msg.content.badge_url,
    tag: msg.id,
    requireInteraction: false,
  };

  event.waitUntil(
    self.registration.showNotification(msg.content.title, opts)
  );
});

// Report engagement on notification click
self.addEventListener('notificationclick', async (event) => {
  const msg = event.notification.data;
  if (msg.action_url) window.open(msg.action_url);
  await openjourney.reportClick(msg.id);
  event.notification.close();
});
```

## Configuration

### Sending Identity (VAPID)

Create a VAPID keypair once and store it as a sending identity:

```
$ openssl ecparam -name prime256v1 -genkey -noout -out vapid_key.pem
$ openssl ec -in vapid_key.pem -pubout -outform DER | tail -c 65 | base64 -w0
$ # Extract private key as base64url
```

Then register in OpenJourney:

```http
POST /v1/sending-identities
Authorization: Bearer <admin-key>

{
  "channel": "push",
  "provider": "webpush",
  "config": {
    "vapid_private_ref": "ENV_VAR_NAME_OR_SECRET_REF",
    "vapid_public_ref": "ENV_VAR_NAME_OR_SECRET_REF",
    "vapid_subject": "mailto:noreply@example.com"
  }
}
```

### SignInAppToken Secret

The customer backend uses a shared secret to mint tokens:

```javascript
const token = SignInAppToken(
  tenantID,
  appID,
  externalID,
  expiresIn,  // e.g., 30 minutes
  secret      // shared with backend
);
```

The browser includes it on fetch:

```javascript
const inbox = await openjourney.fetchInbox({ token });
```

---

**Status**: Stable (v1)  
**Dependencies**: stdlib crypto/ecdsa, HTTPS only  
**Payload Encryption**: Deferred to later native work (RFC 8291)
