# Phase 4 (slice) Implementation Plan: SMS & Push Channels

Status: not started. Builds on completed Phase 2 (campaigns, email/webhook delivery,
`delivery_attempts`, `message.*` engagement events, SES SNS callbacks), Phase 3 (durable
journeys with channel-aware message nodes), and Phase 4 experimentation & analytics
(see [`v1-milestone-4-plan.md`](./v1-milestone-4-plan.md) and its audit). Delivers the
**second channel-sequence step** of `plan.md` §5.9 ("1. Email and webhook ✅ → 2. SMS and
mobile/web push → 3. In-app"): **SMS and push** as first-class delivery channels, with the
full loop — outbound from campaigns *and* journeys (variant-aware, into the M4 reports),
inbound opt-out (STOP) + delivery receipts, device-token lifecycle + an SDK sync contract,
push invalid-token retirement, and rich structured push payloads.

The other Phase 4 slices — forms/pages/scoring, imports/connectors, the extension protocol,
content cards / in-app — remain **deferred to later milestones**.

This is a **recipe book**, like the Phase 2, 3, and 4 plans. Every task references a recipe and
ends with a **Done when** check. **If a task feels ambiguous, open the named existing file,
copy it, rename, and change the fields.** Recipes 6.1–6.20 from the prior plans still apply
verbatim; this plan adds channel recipes 6.21–6.25.

> **Milestone 10.0 comes first and is non-negotiable.** It widens the closed channel/provider
> enums, adds the generic HTTP-provider adapter seam, and collapses the *duplicated*
> provider→adapter switch into a single registry. Do not start 10.1 until 10.0 is green — every
> later task adds a `case` to that registry, and doing it twice (as the code does today) is the
> exact drift hazard the M4 review warned about.

## Design decisions (locked)

1. **One generic HTTP-provider adapter core, driven by named provider *profiles*.** SMS and
   push senders are HTTP POSTs, structurally identical to the existing webhook adapter
   (`internal/channels/webhook.go`): resolve endpoint + auth from `SendingIdentity.Config`, POST
   a JSON body, classify the response into `DeliveryError{Retryable}`. The **only** per-provider
   difference is (a) how the request body is shaped and (b) how the response/provider-id/
   invalid-token signal is parsed. That difference is a small pure `ProviderProfile` strategy.
   **Ship profiles:** `twilio` (SMS), `fcm` + `apns` (push), `http` (generic gateway), `fake`
   (tests). New providers = a new profile, **no new plumbing**. This honors `plan.md` §5.9's
   adapter contract (`ValidateConfiguration/ValidateMessage/Send/ParseCallback/ClassifyError`)
   without an SDK per vendor.
   - **APNs caveat:** real APNs is HTTP/2 + ES256 JWT. The `apns` profile builds the correct
     request/headers/`:path`; the transport may use the stdlib HTTP/2-capable client. If JWT
     minting proves heavy for one iteration, ship the profile against a documented HTTP/2 push
     gateway seam and split the direct-APNs-JWT work into 10.5.x — **do not** fake compliance.
2. **Channel + provider are first-class enums; widen the four closed CHECKs.** Today
   `sending_identities.channel/provider` and `templates.channel` + the templates body-presence
   CHECK admit only `email`/`webhook`/`ses` (`010_templates.sql:5,9,22,31-32`). Widen each to add
   `sms`/`push` (and the new provider strings). **Enumerate every value the code writes** — the
   recurring M3/M4 hazard.
3. **Endpoints resolve per-channel from durable identity, never ad-hoc.** SMS endpoint = the
   profile's phone in E.164, resolved by a **channel-aware** resolver mirroring `GetProfileEmails`
   (`internal/postgres/campaigns.go:340-344`). Push endpoint = a device token from a **new
   `device_tokens` table**; a push send **fans out to a profile's active tokens** (one send +
   one disposition row per token, for retirement granularity). The journey message node already
   resolves `sms → attrs['phone']` (`internal/journey/nodes.go:510-516`); add a `push` arm that
   fans out to tokens.
4. **Device tokens are authoritative in Postgres with an SDK sync contract.** A
   register/refresh/deactivate API (`/v1/device-tokens`) upserts one active row per
   `(tenant, app_id, token)`; an idempotent SDK **sync** endpoint lets a client reconcile its
   tokens; tokens carry `platform` (ios/android/web), `provider`, `status`, `last_seen_at`.
   Invalid-token retirement is driven by **provider truth only** (send responses + receipts),
   never by transient errors.
5. **Opt-out is compliance-critical and flows through the existing consent + suppression
   model.** `consent_ledger` already has a `channel` dimension (`001_kernel.sql:92-108`) and
   `suppressions` is already keyed on `(channel, endpoint)` (`011_delivery_policy.sql`). An
   inbound SMS **STOP** → a canonical `consent.changed(state='unsubscribed', channel='sms')`
   event **and** a `suppressions` row (`reason='unsubscribe'`); **START/UNSTOP** resubscribes.
   The policy suppression gate (`internal/policy/policy.go:37`) already blocks suppressed
   endpoints per channel — SMS/push get compliance for free **provided callers pass the right
   channel string**.
6. **Provider callbacks are normalized to canonical `message.*` events**, mirroring
   `handleSESCallback` (`internal/httpapi/callbacks.go:72`). New sibling handlers
   `POST /v1/callbacks/sms/{provider}` and `POST /v1/callbacks/push/{provider}` **verify the
   provider signature/auth before trusting the payload** (a spoofed STOP could suppress an
   arbitrary number), then call `AcceptEvents` with `message.delivered/failed/bounced` or the
   opt-out event. This reuses the whole M4 fact-projection + reports path unchanged.
7. **Rich push payload is a structured, versioned template field.** Add `title_template` +
   `push_data jsonb` (`{actions, image, url/deep_link, collapse_key, badge, sound}`) to
   `templates`, and `Title` + `Data` fields to `ports.RenderedMessage`. Render passes Liquid over
   title/body and each templated `data` value. SMS uses `text_template`/`body_template` only
   (note GSM-7 vs UCS-2 length; full segmentation/cost accounting is deferred).
8. **Reuse M4 machinery wholesale — no reports changes.** Variant/experiment stamping,
   `engagement_facts` (already has a `channel` column, `020_analytics_facts.sql:22`), conversion
   attribution, and the reports service are all channel-generic. SMS/push sends flow into the
   same fact/disposition tables and reports group by channel with **no new OLTP scans**.
9. **Collapse the duplicated adapter switch into one registry.** Provider→adapter selection is
   copy-pasted in `internal/campaigns/deliver.go:380-401` and `internal/journey/deliver.go:106-131`.
   Introduce a single `internal/channels/registry.go` built once per process and used by both,
   so a new provider is registered **once**. Behavior for email/webhook must be byte-identical
   (regression test required).

---

## 1. Architecture

```mermaid
flowchart TD
    subgraph Author[Authoring]
        SI[SendingIdentity: channel+provider+config] --> TPL[Template: sms text / push title+body+data]
        DEVREG[SDK register/refresh token] --> DT[(device_tokens: active per profile+platform)]
    end

    subgraph Resolve[Channel-aware endpoint resolution]
        CAMP[campaign dispatch] -->|channel=sms| PHONE[attrs.phone E.164]
        CAMP -->|channel=push| TOKS[active device_tokens fan-out]
        JNODE[journey message node] --> PHONE
        JNODE --> TOKS
    end

    subgraph Send[Delivery — reuses Phase 2/3 + M4 variants]
        PHONE --> REG[channels.Registry.For channel,provider]
        TOKS --> REG
        REG --> PROF[HTTP-provider adapter + profile: twilio/fcm/apns]
        PROF -->|Send| PROV[(provider API)]
        PROF -->|message.sent w/ channel+variant| ING[AcceptEvents]
        PROF -->|invalid token| RETIRE[retire device_token]
    end

    subgraph Inbound[Provider callbacks — signature-verified]
        PROV -->|DLR / receipt| CB[/v1/callbacks/sms|push/{provider}]
        PROV -->|STOP keyword| CB
        CB -->|message.delivered/failed/bounced| ING
        CB -->|consent.changed unsubscribed + suppress| ING
        CB -->|invalid token| RETIRE
    end

    subgraph Report[Reuse M4 — scan-free]
        ING --> PROJ[ProjectEvent → engagement_facts / conversion_facts]
        PROJ --> RPT[reports grouped by channel]
    end
```

**Reused unchanged:** the `ChannelAdapter` interface (`internal/ports/adapter.go`), all policy
gates (suppression/consent/fatigue are channel-keyed; **fatigue is deliberately cross-channel**,
`internal/postgres/delivery.go:82-93`), quiet hours (`internal/journey/quiethours.go`), the
`consent.changed` + `message.bounced → suppressions` projections (`internal/postgres/store.go`),
the SES-callback normalization pattern (`internal/httpapi/callbacks.go`), M4 variant stamping +
`engagement_facts`/`conversion_facts` + the reports service, RBAC/scopes, telemetry (already
dimensioned by a `channel` attribute), and the React `requestJSON` + section patterns.

### 1.1 Where each channel's data lives

| Concern | Email (today) | SMS (new) | Push (new) |
|---|---|---|---|
| Recipient endpoint | `attributes->>'email'` | `attributes->>'phone'` (E.164) | `device_tokens` (fan-out, per token) |
| Sender identity | `from_address` | `from_address` = sender number/short-code | `from_address` NULL; app/bundle in `config` |
| Content | subject+html+text | `text`/`body` only | `title`+`body`+`data` jsonb |
| Provider | `ses` | `twilio` / `http` | `fcm` / `apns` / `http` |
| Inbound | SNS `POST /v1/callbacks/ses` | `POST /v1/callbacks/sms/{provider}` (STOP, DLR) | `POST /v1/callbacks/push/{provider}` (receipt, invalid-token) |
| Opt-out | bounce/complaint → suppression | STOP → consent.changed + suppression | (token retirement, not opt-out) |
| Reports | `delivery_attempts` + `engagement_facts` | same, `channel='sms'` | same, `channel='push'` |

---

## 2. Schema (new migrations)

Next numbers after `020_analytics_facts.sql`. Conventions as always: `IF NOT EXISTS`, uuid PKs,
`timestamptz`, tenant/workspace FKs, CHECK-constrained enums that enumerate **every** value the
code writes.

### 2.1 `021_sms_push_channels.sql`
```sql
-- Widen channel/provider enums (email/webhook/ses unchanged; add sms/push + new providers).
ALTER TABLE sending_identities DROP CONSTRAINT IF EXISTS sending_identities_channel_check;
ALTER TABLE sending_identities ADD CONSTRAINT sending_identities_channel_check
    CHECK (channel IN ('email','webhook','sms','push'));
ALTER TABLE sending_identities DROP CONSTRAINT IF EXISTS sending_identities_provider_check;
ALTER TABLE sending_identities ADD CONSTRAINT sending_identities_provider_check
    CHECK (provider IN ('ses','webhook','twilio','fcm','apns','http','fake'));
-- from_address is already NULLable (010_templates.sql:6) and doubles as the SMS sender-id;
-- push leaves it NULL. The existing UNIQUE(tenant_id, channel, from_address) still applies
-- (Postgres treats NULL from_address rows as distinct, so multiple push identities are allowed).

-- Templates: admit sms/push and their content requirements.
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_channel_check;
ALTER TABLE templates ADD CONSTRAINT templates_channel_check
    CHECK (channel IN ('email','webhook','sms','push'));
ALTER TABLE templates ADD COLUMN IF NOT EXISTS title_template text;   -- push notification title
ALTER TABLE templates ADD COLUMN IF NOT EXISTS push_data jsonb;       -- {actions,image,url,collapse_key,badge,sound}

-- Body-presence CHECK is an UNNAMED inline constraint (010_templates.sql:31-32); its generated
-- name is typically `templates_check`. Confirm with `\d templates` and DROP the exact name,
-- then recreate with sms/push arms.
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_check;
ALTER TABLE templates ADD CONSTRAINT templates_body_presence_check CHECK (
       (channel='email'   AND html_template IS NOT NULL)
    OR (channel='webhook' AND body_template IS NOT NULL)
    OR (channel='sms'     AND COALESCE(text_template, body_template) IS NOT NULL)
    OR (channel='push'    AND title_template IS NOT NULL AND COALESCE(body_template, text_template) IS NOT NULL)
);
```

### 2.2 `022_device_tokens.sql`
```sql
CREATE TABLE IF NOT EXISTS device_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK (platform IN ('ios','android','web')),
    provider text NOT NULL CHECK (provider IN ('fcm','apns','http','fake')),
    token text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, token)         -- one row per physical token; upsert on re-register
);
CREATE INDEX IF NOT EXISTS device_tokens_active_idx
    ON device_tokens (tenant_id, workspace_id, profile_id, status) WHERE status = 'active';

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read',
    'device_tokens:read','device_tokens:write'
];
```

---

## 3. The two seams to get right

### 3.1 Adapter registry + provider profile (`internal/channels/registry.go`, `httpprovider.go`)
```
// registry.go — built ONCE per process, used by BOTH campaigns and journeys.
type Registry struct { adapters map[string]ports.ChannelAdapter }  // key = provider string
func (r *Registry) For(provider string) ports.ChannelAdapter       // fallback to fake for unknown/empty

// httpprovider.go — generic transport; provider-specific shaping via a profile.
type ProviderProfile interface {
    BuildRequest(msg ports.RenderedMessage, cfg Config) (*http.Request, error) // body + auth + url
    ParseResponse(*http.Response, []byte) (providerID string, err error)       // classify → DeliveryError{Retryable}
    IsInvalidToken(*http.Response, []byte) bool                                // push only; false for sms
}
```
- Copy `webhook.go` for the transport (timeout, do-request, `mapError`, `IsSafeURL` for the
  generic `http` profile). The `twilio`/`fcm`/`apns` profiles are pure request/response functions.
- **Replace** the `switch provider` in `campaigns/deliver.go:380` and `journey/deliver.go:106`
  with `registry.For(identity.Provider).Send(...)`. Keep `Adapter/SESAdapter/WebhookAdapter/
  FakeAdapter` fields working (register them into the map) so email/webhook is unchanged.

### 3.2 Channel-aware endpoint resolution
- **Campaign side (the gap):** `dispatch.go:66-81` + `GetProfileEmails` are hardcoded to
  `email`. Add a `resolveEndpoints(channel)` that branches: `email→attrs.email`,
  `sms→attrs.phone`, `push→active device_tokens (one recipient per token)`. Profiles missing the
  channel endpoint are skipped (as email does today).
- **Journey side (mostly done):** `nodes.go:510-516` already has `email`/`sms` arms; add a
  `push` arm that enqueues one intent per active token.

### 3.3 Opt-out + invalid-token (compliance)
- **STOP → suppression:** the callback emits `consent.changed(unsubscribed, channel=sms)` +
  inserts a `suppressions` row (`reason='unsubscribe'`, `ON CONFLICT DO NOTHING`). Reuse the
  `message.bounced → suppressions` projection block (`store.go:449-484`) — add an opt-out case.
  `START` → remove the suppression + `consent.changed(subscribed)`.
- **Invalid token → retire:** on a send/receipt the profile flags `IsInvalidToken`, set
  `device_tokens.status='retired'` **idempotently** (`WHERE status='active'`), and exclude
  retired tokens from the next fan-out. Retire on **provider truth only**, never on 5xx/timeout.

---

## 4. Exit-criteria traceability (this slice of `plan.md` Phase 4 + §5.9)

| Phase 4 element | How this plan meets it | Milestone |
|---|---|---|
| SMS + mobile/web push channels | `twilio`/`fcm`/`apns` profiles on the generic adapter; campaign + journey senders | 10.1, 10.4 |
| Stable delivery intent key + provider idempotency token | per-endpoint idempotency key (reuse the M2 pattern); provider token where supported | 10.1, 10.4 |
| Provider callbacks normalized to canonical immutable events | `POST /v1/callbacks/{sms,push}/{provider}` → `message.*` | 10.2, 10.5 |
| Bounce/opt-out/unsubscribe processing | STOP → consent.changed + suppression; DLR invalid-number → bounce | 10.2 |
| Push token lifecycle + invalid-token cleanup | `device_tokens` + registration/SDK sync + retirement | 10.3, 10.5 |
| Rich message content per channel | sms text; push title+body+data (actions/image/deep-link) | 10.4 |
| Rate limits, retry w/ backoff, error classification | reuse per-identity limiter + `DeliveryError{Retryable}` | 10.1, 10.4 |
| Provider contract suite | table-driven per-profile request/response/classification tests | 10.7 |
| Reports across channel/variant, total & unique | reuse M4 `engagement_facts`/reports, `channel` dimension | 10.6, 10.7 |

---

## 5. Implementation recipes (new; 6.1–6.20 from prior plans still apply)

### 6.21 New channel via HTTP-provider profile
- Add a `ProviderProfile` in `internal/channels/profiles_*.go`: `BuildRequest` (shape body +
  auth from `identity.Config`), `ParseResponse` (provider-id + retryable), `IsInvalidToken`
  (push). Register it in `registry.go` under its provider string. **Done when:** a table-driven
  test asserts the exact request body + the response→`DeliveryError{Retryable}` mapping.

### 6.22 Channel-aware endpoint resolution
- Campaign: extend recipient resolution to branch on `template.Channel` (mirror
  `GetProfileEmails`; for push, join `device_tokens WHERE status='active'`, one recipient per
  token). Journey: add the `push` arm to `nodes.go:510-516`. **Done when:** an sms campaign
  targets phones and a push campaign fans out to each active token; profiles missing the endpoint
  are skipped.

### 6.23 Provider inbound/callback handler
- Copy `handleSESCallback` (`callbacks.go:72`): verify provider signature/auth, parse the
  payload, build canonical `domain.Event`s, `store.AcceptEvents`. Route it unauthenticated but
  signature-verified (like `/v1/callbacks/ses`). **Done when:** a signed DLR creates a
  `message.delivered` fact and an unsigned/forged payload is rejected 4xx.

### 6.24 Device-token lifecycle
- Store methods on `*postgres.Store` in `internal/postgres/device_tokens.go`:
  `RegisterDeviceToken` (upsert on `(tenant, app_id, token)`, refresh `last_seen_at`, reactivate
  if retired), `RetireDeviceToken` (idempotent, `WHERE status='active'`),
  `ListActiveDeviceTokens(profileID)`. Add to `ports.Store`. Tenant+workspace+app scoped.
  **Done when:** registering the same token twice yields one active row; retire flips it once.

### 6.25 Rich template field (title/data) + render pass
- Add `Title` + `Data map[string]string`/`json.RawMessage` to `ports.RenderedMessage`
  (`adapter.go`) and `TitleTemplate`/`PushData` to `domain.Template`. Render title + each
  templated data value via `render.Render`. **Done when:** a push template with a
  `{{first_name}}` title and a deep-link renders both, and `templates` CHECK requires title+body
  for `channel='push'`.

---

## 6. Task list

Testing bar: unit + golden per milestone; one consolidated integration/contract/compliance pass
in 10.7. Each task ends with a **Done when**. Do them in order; compile + `go vet` between
milestones. **Every new `channel`/`provider`/`platform` string the code writes must appear in
the matching CHECK constraint (10.0), the RBAC allowlist + `api_keys` default (10.0/10.3), and
be registered in the adapter registry (10.0).**

### Milestone 10.0 — Foundations: enums + adapter registry (DO FIRST)
1. [x] **Migration** `021_sms_push_channels.sql` per §2.1: widen the four CHECKs
   (`sending_identities.channel`, `.provider`, `templates.channel`, templates body-presence),
   add `title_template` + `push_data`. *Done when:* an `sms` and a `push` template + identity
   insert successfully; an unknown channel still fails the CHECK. — done: `022_sms_push_channels.sql` applied; sms+push identity+template inserts succeed; `fax` channel fails `sending_identities_channel_check`; `go build ./...` BUILD OK
2. [x] **Adapter registry** `internal/channels/registry.go` (§3.1): a `provider→ChannelAdapter` map
   built once, `For(provider)` with fake fallback. Register `ses`/`webhook`/`fake`. *Done when:*
   a unit test resolves each provider and unknown→fake. — done: `registry.go` + `registry_test.go`; TestRegistry_For / TestRegistry_DefaultRegistry_KnownProviders / TestRegistry_Register all PASS
3. [x] **Generic HTTP-provider adapter** `internal/channels/httpprovider.go` + `ProviderProfile`
   seam (Recipe 6.21), copying `webhook.go` transport (`mapError`, timeout, `IsSafeURL` for the
   `http`/`fake` profiles). *Done when:* a `fake`/`http` profile sends and classifies a 5xx as
   retryable, a 4xx as permanent. — done: `httpprovider.go` + `httpprovider_test.go`; 10 profile tests PASS (5xx retryable, 4xx permanent, IsInvalidToken=false, BuildRequest validated)
4. **Replace both provider switches** (`campaigns/deliver.go:380`, `journey/deliver.go:106`) with
   `registry.For(...)`; construct the registry in `cmd/campaigns-delivery/main.go:84` and
   `cmd/journeys-worker/main.go:84`. *Done when:* the existing email/webhook tests
   (`deliver_test.go` in both packages) still pass unchanged (behavior-preserving refactor).

### Milestone 10.1 — SMS outbound (campaign + journey, variant-aware)
1. **Twilio SMS profile** `internal/channels/profiles_sms.go` (Recipe 6.21): Basic-auth POST,
   body `{To, From, Body}` (+ `StatusCallback` URL), parse `sid`→provider-id, classify Twilio
   error codes. Also a `http` generic-SMS profile. *Done when:* the profile table-test passes.
2. **Channel-aware campaign recipients** (Recipe 6.22): a `GetProfilePhones` mirror of
   `GetProfileEmails` (`campaigns.go:340`); `dispatch.go` branches on `template.Channel`; skip
   profiles with no phone. *Done when:* an sms campaign resolves E.164 phones only.
3. **SMS template + render**: `channel='sms'`, `text_template`/`body_template`; render body; note
   GSM-7/UCS-2 length (warn, don't block). *Done when:* an sms template renders a body with
   profile vars.
4. **Send SMS end-to-end** from campaign + journey: `registry.For('twilio')`, stamp
   `channel='sms'` on `delivery_attempts`/`journey_message_intents` + the `message.sent` payload;
   reuse M4 variant stamping; `telemetry.MessagesSent{channel=sms}`. *Done when:* an sms campaign
   and an sms journey node send via the fake profile with correct disposition + `message.sent`
   (incl. variant when bound to an experiment).

### Milestone 10.2 — SMS inbound: opt-out (STOP) + delivery receipts (DLR)
1. **Inbound endpoint** `POST /v1/callbacks/sms/{provider}` (Recipe 6.23) mirroring
   `handleSESCallback`: verify Twilio signature (`X-Twilio-Signature` HMAC) before trusting the
   body. Route in `server.go` near `:128`. *Done when:* a forged (unsigned) request is 4xx.
2. **STOP/START/HELP keyword handling** → `consent.changed(unsubscribed|subscribed, channel=sms)`
   + suppression insert/delete (reason `unsubscribe`); add the opt-out case to the suppression
   projection (`store.go:449`). *Done when:* a STOP webhook suppresses future sms to that phone
   (policy gate blocks it); a START removes the suppression.
3. **DLR status callback** → normalize to `message.delivered` / `message.failed` /
   `message.bounced` (invalid number → bounce → suppression), via `AcceptEvents`. *Done when:* a
   delivered DLR creates one `engagement_facts` row `channel='sms'`; a failed/invalid DLR is
   recorded (and invalid → suppression). Telemetry `Bounces{channel=sms}`.

### Milestone 10.3 — Device tokens: registration API + SDK sync contract
1. **Migration** `022_device_tokens.sql` per §2.2 + scopes `device_tokens:read/write` in
   `rbac.go` allowlist and the `api_keys` default array. *Done when:* the table exists; a fresh
   key carries the scopes.
2. **Domain + store CRUD** `internal/postgres/device_tokens.go` (Recipe 6.24) + `ports.Store`:
   `RegisterDeviceToken` (upsert), `RetireDeviceToken`, `ListActiveDeviceTokens`,
   `ListDeviceTokensByProfile`. Tenant+workspace+app scoped. *Done when:* re-register upserts to
   one active row; a live-Postgres test proves two-workspace isolation.
3. **HTTP + SDK sync contract**: `POST /v1/device-tokens` (register/refresh),
   `DELETE /v1/device-tokens/{id}` (deactivate), and an idempotent `POST /v1/device-tokens/sync`
   (client sends its full token set for a profile+app; server reconciles active/retired and
   returns the canonical state). Scope `device_tokens:write`/`read`. *Done when:* register→list
   round-trips; sync with a shrunk set retires the dropped tokens.
4. **OpenAPI** entries + a short `docs/sdk/device-tokens.md` describing the client contract
   (register on launch, refresh on rotation, sync on reconnect; idempotent; response schema).
   *Done when:* redocly lints clean.

### Milestone 10.4 — Push outbound (campaign + journey, variant-aware, rich payload)
1. **FCM + APNs profiles** `internal/channels/profiles_push.go` (Recipe 6.21): FCM v1
   (`Authorization: Bearer <token>`, body `{message:{token,notification:{title,body},data}}`);
   APNs (HTTP/2, `:path /3/device/{token}`, ES256 JWT `authorization` header, `apns-topic`).
   `IsInvalidToken` detects FCM `UNREGISTERED`/`NOT_FOUND` and APNs `BadDeviceToken`/`Unregistered`.
   *Done when:* per-profile table tests assert request shape + invalid-token detection. (If APNs
   JWT minting is too heavy for one iteration, land FCM here and split direct-APNs-JWT to 10.5.4
   — a `BLOCKERS.md` note, not a fake.)
2. **Rich push template**: `Title` + `Data` on `ports.RenderedMessage`; `TitleTemplate`/`PushData`
   on `domain.Template`; render passes (Recipe 6.25). *Done when:* a push template renders title
   + body + a templated deep-link in `data`.
3. **Push endpoint fan-out** (Recipe 6.22): campaign + journey resolve a profile's active
   `device_tokens` and enqueue **one send + one disposition row per token** (provider chosen by
   the token's `provider`/`platform`); stamp `channel='push'` + variant. *Done when:* a push
   campaign to a profile with 2 active tokens makes 2 provider calls with 2 disposition rows.
4. **Send push end-to-end** from campaign + journey; `telemetry.MessagesSent{channel=push}`;
   reuse M4 variant stamping. *Done when:* a push campaign and a push journey node send via the
   fake profile with correct per-token dispositions + `message.sent`.

### Milestone 10.5 — Push receipts + invalid-token retirement
1. **Invalid-token retirement at send time** (§3.3): when a profile's `IsInvalidToken` is true,
   `RetireDeviceToken` idempotently and record the disposition (`decision='failed'`, not a
   retryable). Telemetry `openjourney_push_tokens_retired_total`. *Done when:* a send returning
   `UNREGISTERED` retires that token and the next fan-out excludes it.
2. **Push receipt endpoint** `POST /v1/callbacks/push/{provider}` (Recipe 6.23): delivery
   receipts → `message.delivered`; provider invalid-token feedback → retire token. Signature/auth
   verified. *Done when:* a delivery receipt creates one `engagement_facts` row `channel='push'`;
   a feedback-invalid token is retired.
3. **(If split) Direct APNs JWT** — implement ES256 JWT minting + HTTP/2 transport for the `apns`
   profile if deferred from 10.4.1. *Done when:* the APNs profile builds a valid signed request
   in its table test.

### Milestone 10.6 — UI: identities, templates, device tokens, per-channel reports
1. **Sending identities UI**: channel selector (email/webhook/sms/push) + a provider config
   editor per profile (Twilio auth token ref; FCM/APNs credential ref). *Done when:* an sms and a
   push identity round-trip.
2. **Template editor**: channel selector; sms body field; push title + body + a `data` editor
   (actions/image/deep-link). Follow the existing template-editor patterns. *Done when:*
   `npm run build` passes; an sms and a push template round-trip.
3. **Device tokens inspector**: per-profile active tokens with a retire button. *Done when:* the
   list renders and retire calls `DELETE /v1/device-tokens/{id}`.
4. **Reports view**: add a `channel` breakdown/filter to the M4 Reports view (funnel +
   deliverability already flow per channel). Follow the `dataviz` skill; theme-aware. *Done
   when:* reports show separate sms/push/email numbers matching the API.

### Milestone 10.7 — Integration, provider contract suite, compliance & audit (closeout)
1. **Provider contract suite** (`plan.md` Phase 4 exit): table-driven tests per profile
   (`twilio`/`fcm`/`apns`/`http`/`fake`) asserting request shape, auth, response→provider-id,
   retryable classification, and invalid-token detection. *Done when:* every profile has a
   contract test.
2. **End-to-end integration** (DB-gated, copy `TestCampaignsEndToEnd`/`TestReportAccuracy…`):
   seed an sms campaign + a push campaign + a journey using both, variant-bound; drive the
   callbacks (DLR, STOP, push receipt, invalid-token); assert dispositions, `engagement_facts`,
   suppressions, token retirement, and the per-channel report numbers exactly. *Done when:* the
   numbers match seeded data.
3. **Compliance tests**: sending to a STOP-suppressed phone (or opted-out profile) is blocked by
   the policy gate; quiet-hours honored for sms/push; fatigue counts sms+push **together with**
   email (cross-channel cap, reuse `TestSentCountSince_FatigueAcrossChannels`). *Done when:*
   each is proven by a test.
4. **Determinism**: an experiment-bound subject gets the **same variant regardless of channel**
   (email vs sms vs push) — the assignment is channel-independent. *Done when:* a test asserts
   identical variant across channels.
5. **Telemetry**: `MessagesSent`/`Bounces`/`Complaints{channel=sms|push}`,
   `openjourney_push_tokens_retired_total`, `openjourney_sms_opt_outs_total`. *Done when:*
   counters increment with the right labels; retirement/opt-out counted exactly once.
6. **Run the suite**: `go build/vet/test ./...`, `go mod tidy`, `cd web && npm run typecheck &&
   npm run build && npm test`, `npm audit`. *Done when:* all green.
7. **Audit doc** `docs/milestones/v1-milestone-5-audit.md` in the M2/M3/M4 table format, one row
   per requirement (10.0–10.7) with direct current-state evidence. *Done when:* every row cites a
   named file/test.

---

## 7. Carry-over hazards & invariants

1. **Do 10.0 first.** Widen the CHECKs and build the registry before any channel work; a new
   provider must be registered **once**, not in two drifting switches.
2. **Enumerate every enum value.** Any `channel`/`provider`/`platform`/`decision`/`reason` string
   the code writes must be in the matching CHECK constraint — the recurring M3/M4 failure mode.
3. **Opt-out is legally mandatory, idempotent, and immediate.** A STOP must create a suppression
   before the next send; the policy gate (`policy.go:37`) then blocks it. Never send to a
   suppressed `(channel, endpoint)`. Verify inbound signatures — a spoofed STOP must not suppress
   an arbitrary number, and a spoofed receipt must not forge delivery.
4. **Invalid-token retirement on provider truth only.** Retire on `UNREGISTERED`/`BadDeviceToken`
   etc., never on 5xx/timeout (those are retryable). Retirement is idempotent (`WHERE
   status='active'`) and must not drop the `message.sent`/disposition event.
5. **One delivery attempt per endpoint with a stable idempotency key.** SMS/push cost money;
   retries must not double-send. Reuse the M2 idempotency-key pattern and the provider idempotency
   token where supported. Push fans out **per token** — the key includes the token.
6. **Device tokens are tenant+workspace+app scoped.** `RegisterDeviceToken`/`ListActive…` must
   filter all three — no cross-tenant token leakage (echoes the M3 workspace-isolation hazard).
7. **Reuse M4 assignment/reports unchanged.** No `math/rand`; a subject's variant is
   channel-independent; reports stay scan-free (SMS/push flow into the same fact/disposition
   tables — no new `accepted_events` scans).
8. **Do not special-case fatigue/quiet-hours per channel.** They are deliberately cross-channel /
   channel-generic; SMS and push count toward the same per-profile caps and honor the same quiet
   hours as email.
9. **Refactors are behavior-preserving.** The registry swap (10.0.4) and endpoint-resolution
   changes must leave the existing email/webhook tests passing unchanged.

## 8. Open items to confirm before coding

- **Phone attribute + normalization.** This plan resolves SMS endpoints from
  `attributes->>'phone'` in E.164. Confirm that's the canonical phone key and whether inbound
  normalization (libphonenumber-style) is in scope for v1 or deferred (recommend: require E.164
  at ingestion, defer normalization).
- **Provider credentials/secrets.** SES relies on ambient AWS creds; Twilio/FCM/APNs need secrets
  (auth token, service-account/OAuth token, APNs signing key). This plan stores config in
  `sending_identities.config` jsonb with a secret **reference**, matching `plan.md §5.9`
  ("KMS/Vault-backed credential references"). Confirm the secret-reference resolver to use, or
  whether v1 may store secrets inline in dev only.
- **Push disposition granularity.** This plan writes **one disposition row per token** (for
  retirement + per-device analytics). Confirm that's acceptable vs. one row per profile.
- **APNs direct vs gateway.** Direct APNs (HTTP/2 + ES256 JWT) is included but flagged as the
  heaviest single piece; confirm whether to implement it in 10.4/10.5 or ship FCM first and
  fast-follow APNs.
- **SDK client libraries.** This milestone delivers the device-token **sync contract** (HTTP
  endpoints + doc), not native Swift/Kotlin/JS SDKs — those remain a later phase. Confirm.
- **SMS segmentation/cost accounting.** Length/segment warnings only; full per-segment cost
  accounting and provider cost reconciliation are deferred to the analytics/cost milestone.
