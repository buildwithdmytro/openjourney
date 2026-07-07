# Optional OIDC provider validation

Use this checklist when a deployment chooses external SSO. Milestone 1 does not require OIDC because the control plane includes built-in self-hosted operator login.

## Required provider configuration

- Authorization Code + PKCE enabled for the OpenJourney web client.
- Redirect URI: `http://localhost:3000` for local validation, plus the deployed control-plane URL for environment validation.
- ID token includes stable claims:
  - `sub`
  - `email`
  - `name`
  - `tenant_id`
  - `workspace_id`
  - `app_id`
- The issuer URL is reachable by the API at `/.well-known/openid-configuration`.

## OpenJourney configuration

Configure API:

```bash
OPENJOURNEY_OIDC_ISSUER='https://issuer.example.com'
OPENJOURNEY_OIDC_CLIENT_ID='openjourney-control-plane'
OPENJOURNEY_CORS_ALLOWED_ORIGIN='http://localhost:3000'
```

Configure web:

```bash
VITE_OIDC_AUTHORITY='https://issuer.example.com'
VITE_OIDC_CLIENT_ID='openjourney-control-plane'
VITE_OIDC_REDIRECT_URI='http://localhost:3000'
```

## Provision the user

Create or reuse a role with the required permissions, then provision the provider user from the Access view or API:

```bash
curl -H "Authorization: Bearer ${OPENJOURNEY_DEV_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "oidc_issuer":"https://issuer.example.com",
    "oidc_subject":"provider-subject",
    "email":"operator@example.com",
    "display_name":"Operator",
    "role_ids":["role-id"]
  }' \
  http://localhost:8080/v1/users
```

## Acceptance checks

1. Start API and web with the same issuer and client ID.
2. Open the control plane and select “Sign in with OIDC”.
3. Complete provider login and return to the control plane.
4. Confirm the API-key/OIDC token field is populated by the returned ID token.
5. Open Profiles, Schemas, Access, Operations, and Audit views using the OIDC token.
6. Confirm a role-scoped user cannot call an endpoint outside its role permissions.
7. Confirm the API rejects an ID token with a wrong audience, wrong issuer, missing tenant/workspace/app claims, or an unprovisioned subject.

The repeatable command-line gate is:

```bash
export OPENJOURNEY_API_URL='http://localhost:8080'
export OPENJOURNEY_OIDC_ISSUER='https://issuer.example.com'
export OPENJOURNEY_OIDC_CLIENT_ID='openjourney-control-plane'
export OPENJOURNEY_OIDC_ID_TOKEN='fresh-id-token-from-provider'
make validate-oidc-provider
```

The token must belong to a provisioned user with `profiles:read`. The script fetches the provider discovery document, validates required metadata, checks API readiness, and calls the profile lookup API with the ID token. A `200` or `404` response proves the API accepted the OIDC token and authorized `profiles:read`; `401` means token verification/provisioning failed, and `403` means the token was valid but the role lacked the scope.

Milestone evidence should record provider name, issuer URL, client ID, validation date, tested user subject, tested role, and pass/fail notes. Do not store ID tokens, client secrets, or screenshots containing tokens in the repository.
