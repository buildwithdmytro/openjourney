#!/usr/bin/env sh
set -eu

API_URL="${OPENJOURNEY_API_URL:-http://localhost:8080}"
ISSUER="${OPENJOURNEY_OIDC_ISSUER:-}"
CLIENT_ID="${OPENJOURNEY_OIDC_CLIENT_ID:-}"
ID_TOKEN="${OPENJOURNEY_OIDC_ID_TOKEN:-}"
PROFILE_ID="${OPENJOURNEY_OIDC_VALIDATION_PROFILE_ID:-__oidc_validation_probe__}"

if [ -z "$ISSUER" ] || [ -z "$CLIENT_ID" ] || [ -z "$ID_TOKEN" ]; then
  cat >&2 <<'EOF'
Required environment:
  OPENJOURNEY_OIDC_ISSUER
  OPENJOURNEY_OIDC_CLIENT_ID
  OPENJOURNEY_OIDC_ID_TOKEN

Optional:
  OPENJOURNEY_API_URL=http://localhost:8080
  OPENJOURNEY_OIDC_VALIDATION_PROFILE_ID=__oidc_validation_probe__

The token must be a fresh ID token for a provisioned OpenJourney user with profiles:read.
Do not commit or paste the token into issue trackers, logs, or evidence files.
EOF
  exit 2
fi

DISCOVERY_URL="${ISSUER%/}/.well-known/openid-configuration"
DISCOVERY_JSON="$(curl -fsS "$DISCOVERY_URL")"

DISCOVERY_JSON="$DISCOVERY_JSON" python3 - "$ISSUER" "$CLIENT_ID" <<'PY'
import json
import os
import sys

issuer = sys.argv[1].rstrip("/")
client_id = sys.argv[2]
document = json.loads(os.environ["DISCOVERY_JSON"])
if document.get("issuer", "").rstrip("/") != issuer:
    raise SystemExit(f"issuer mismatch: {document.get('issuer')!r}")
if not document.get("jwks_uri"):
    raise SystemExit("jwks_uri missing from discovery document")
if "authorization_endpoint" not in document:
    raise SystemExit("authorization_endpoint missing from discovery document")
if "token_endpoint" not in document:
    raise SystemExit("token_endpoint missing from discovery document")
if client_id == "":
    raise SystemExit("client ID must not be empty")
PY

curl -fsS "$API_URL/health/ready" >/dev/null

HTTP_STATUS="$(curl -sS -o /tmp/openjourney-oidc-validation-body.json -w "%{http_code}" \
  -H "Authorization: Bearer $ID_TOKEN" \
  "$API_URL/v1/profiles/$PROFILE_ID")"

case "$HTTP_STATUS" in
  200|404)
    echo "OIDC provider validation passed: discovery is valid and the API accepted the ID token for profiles:read."
    ;;
  401)
    echo "OIDC provider validation failed: the API rejected the ID token. Check issuer, audience/client ID, required tenant/workspace/app claims, token expiry, and user provisioning." >&2
    exit 1
    ;;
  403)
    echo "OIDC provider validation failed: token is valid but the provisioned user lacks profiles:read." >&2
    exit 1
    ;;
  *)
    echo "OIDC provider validation failed: unexpected API status $HTTP_STATUS." >&2
    sed -n '1,20p' /tmp/openjourney-oidc-validation-body.json >&2
    exit 1
    ;;
esac
