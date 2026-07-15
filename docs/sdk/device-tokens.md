# SDK Device Token Sync Contract

OpenJourney supports push notification channel delivery via FCM, APNs, and custom HTTP transports. The SDK interacts with the device token API endpoints to keep the client device tokens synchronized with profiles.

## API Endpoints

### 1. Register or Refresh a Token
* **Route**: `POST /v1/device-tokens`
* **Scope Required**: `device_tokens:write`
* **Request Body**:
  ```json
  {
    "profile_id": "31d68ba1-87ab-4b2a-bf33-4df45e99aa33",
    "platform": "ios",
    "provider": "apns",
    "token": "token-device-value-here"
  }
  ```
* **Response**: Returns the registered `DeviceToken` JSON object (status: `active`).

### 2. Deactivate/Retire a Token
* **Route**: `DELETE /v1/device-tokens/{id}`
* **Scope Required**: `device_tokens:write`
* **Response**: `204 No Content` (flips the token status to `retired`).

### 3. Sync Device Tokens Set
* **Route**: `POST /v1/device-tokens/sync`
* **Scope Required**: `device_tokens:write`
* **Description**: Idempotent endpoint to synchronize all active tokens for a profile. Any active tokens currently registered in the database for the profile that are not present in the request list are automatically deactivated/retired.
* **Request Body**:
  ```json
  {
    "profile_id": "31d68ba1-87ab-4b2a-bf33-4df45e99aa33",
    "tokens": [
      {
        "token": "active-token-value-1",
        "platform": "ios",
        "provider": "apns"
      }
    ]
  }
  ```
* **Response**: `200 OK` with the canonical list of active `DeviceToken` objects.

---

## Client Integration Pattern

To avoid stale pushes and duplicate token entries, client SDKs should adhere to the following contract:

1. **Register on Launch**: Always call `POST /v1/device-tokens` when the application finishes launching and receives a token from the OS.
2. **Refresh on Rotation**: Trigger registration when the OS reports a rotated or newly generated token.
3. **Sync on Reconnect**: Whenever connection is restored or the user logs in/out, call the idempotent `/v1/device-tokens/sync` to reconcile client tokens with the backend state.
