#!/usr/bin/env bash
# Registers <webhook-url> as the bot's Telegram webhook, with a secret token
# that the bot checks on every update (see bot/common/webhook).
#
# Usage: BOT_TOKEN=<token> scripts/set-webhook.sh <webhook-url>
#
# The bot token is read from the environment and never passed as a
# command-line argument, so it doesn't show up in process listings.
set -euo pipefail

webhook_url=${1:?usage: BOT_TOKEN=<token> $0 <webhook-url>}
: "${BOT_TOKEN:?BOT_TOKEN must be set}"
if ! [[ "$BOT_TOKEN" =~ ^[0-9]+:[A-Za-z0-9_-]+$ ]]; then
  echo "BOT_TOKEN doesn't look like a Telegram bot token (<digits>:<letters, digits, _ or ->)" >&2
  exit 1
fi
export BOT_TOKEN

# Must match webhook.DeriveSecret in bot/common/webhook/webhook.go.
secret_token=$(python3 -I -c '
import hashlib, hmac, os
print(hmac.new(os.environ["BOT_TOKEN"].encode(), b"telegram-webhook-secret", hashlib.sha256).hexdigest())
')

telegram() { # <method> [curl args...] -- calls the Bot API, URL passed via stdin
  local method=$1
  shift
  printf 'url = "https://api.telegram.org/bot%s/%s"\n' "$BOT_TOKEN" "$method" |
    curl --config - --silent --show-error --fail-with-body --max-time 30 "$@"
}

response=$(telegram setWebhook \
  --data-urlencode "url=${webhook_url}" \
  --data-urlencode "secret_token=${secret_token}" \
  --data-urlencode 'allowed_updates=["message","callback_query"]' \
  --data-urlencode "max_connections=10")

python3 -I -c '
import json, sys
r = json.loads(sys.argv[1])
if not r.get("ok"):
    sys.exit("setWebhook failed: %s" % r.get("description"))
print("setWebhook: %s" % r.get("description"))
' "$response"

# Verify Telegram stored the expected URL (without printing it).
info=$(telegram getWebhookInfo)
python3 -I -c '
import json, sys
r = json.loads(sys.argv[1])["result"]
if r.get("url") != sys.argv[2]:
    sys.exit("webhook URL mismatch after setWebhook")
print("Webhook verified (pending updates: %s)" % r.get("pending_update_count", 0))
' "$info" "$webhook_url"
