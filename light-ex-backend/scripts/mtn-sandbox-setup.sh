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

user_id="$(uuidgen | tr '[:upper:]' '[:lower:]')"
callback_host="example.com"

status="$(curl -s -o /dev/null -w "%{http_code}" -X POST \
  "$base/$product/v1_0/apiuser" \
  -H "X-Reference-Id: $user_id" \
  -H "Ocp-Apim-Subscription-Key: $sub_key" \
  -H "Content-Type: application/json" \
  -d "{\"providerCallbackHost\": \"$callback_host\"}")"
if [ "$status" != "201" ]; then
  echo "Creating API User failed with HTTP $status" >&2
  exit 1
fi

key_json="$(curl -s -X POST \
  "$base/$product/v1_0/apiuser/$user_id/apikey" \
  -H "Ocp-Apim-Subscription-Key: $sub_key")"
api_key="$(printf '%s' "$key_json" | sed -n 's/.*"apiKey"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
if [ -z "$api_key" ]; then
  echo "Creating API Key failed; response did not contain apiKey" >&2
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
