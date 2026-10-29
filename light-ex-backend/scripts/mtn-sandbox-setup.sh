#!/usr/bin/env bash
# Creates the MTN MoMo sandbox API User and API Key for one product and
# saves them to .env.mtn (git-ignored). Run once per product.
#
# Usage: ./scripts/mtn-sandbox-setup.sh collection|disbursement
set -euo pipefail

product="${1:-}"
case "$product" in
  collection|disbursement) ;;
  *) echo "usage: $0 collection|disbursement" >&2; exit 1 ;;
esac

base="https://sandbox.momodeveloper.mtn.com"
out=".env.mtn"
prefix="MTN_$(echo "$product" | tr '[:lower:]' '[:upper:]')"

# Read the subscription key without echoing it or saving it in shell history.
read -r -s -p "Paste the ${product} subscription key (input hidden): " sub_key
echo

if [ -z "${sub_key:-}" ]; then
  echo "Error: Subscription key was empty. Please copy your key and paste it (in Linux terminals, use Ctrl+Shift+V or right-click to paste)." >&2
  exit 1
fi

user_id="$(uuidgen | tr '[:upper:]' '[:lower:]')"
callback_host="example.com"

echo "Creating API User (${user_id})..."

resp="$(curl -sS -w "\n%{http_code}" -X POST \
  "$base/v1_0/apiuser" \
  -H "X-Reference-Id: $user_id" \
  -H "Ocp-Apim-Subscription-Key: $sub_key" \
  -H "Content-Type: application/json" \
  -d "{\"providerCallbackHost\": \"$callback_host\"}" || true)"
status="$(printf '%s\n' "$resp" | tail -n1)"
body="$(printf '%s\n' "$resp" | sed '$d')"

if [ "$status" != "201" ]; then
  echo "Creating API User failed with HTTP status: $status" >&2
  if [ -n "$body" ]; then
    echo "Response: $body" >&2
  fi
  exit 1
fi

echo "API User created. Generating API Key..."

key_resp="$(curl -sS -w "\n%{http_code}" -X POST \
  "$base/v1_0/apiuser/$user_id/apikey" \
  -H "Ocp-Apim-Subscription-Key: $sub_key" \
  -H "Accept: application/json" \
  -H "Content-Length: 0" \
  -d "" || true)"

key_status="$(printf '%s\n' "$key_resp" | tail -n1)"
key_body="$(printf '%s\n' "$key_resp" | sed '$d')"

if [ "$key_status" != "201" ] && [ "$key_status" != "200" ]; then
  echo "Generating API Key failed with HTTP status: $key_status" >&2
  if [ -n "$key_body" ]; then
    echo "Response: $key_body" >&2
  fi
  exit 1
fi

api_key="$(printf '%s' "$key_body" | sed -n 's/.*"apiKey"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
if [ -z "$api_key" ]; then
  echo "Creating API Key failed; response did not contain apiKey: $key_body" >&2
  exit 1
fi

{
  echo "${prefix}_SUBSCRIPTION_KEY=$sub_key"
  echo "${prefix}_API_USER=$user_id"
  echo "${prefix}_API_KEY=$api_key"
} >> "$out"
chmod 600 "$out"

echo "Saved ${prefix}_API_USER, ${prefix}_API_KEY and ${prefix}_SUBSCRIPTION_KEY to $out"
echo "The values are not printed here. Keep $out out of Git."
