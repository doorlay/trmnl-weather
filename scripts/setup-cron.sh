#!/usr/bin/env bash
set -euo pipefail

INTERVAL="${1:-15}"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="$SCRIPT_DIR/webserver"
ENV_FILE="$SCRIPT_DIR/.env"
LOG_FILE="/tmp/trmnl-weather.log"
CRON_MARKER="trmnl-weather"

if [ ! -f "$ENV_FILE" ]; then
  echo "Error: .env file not found at $ENV_FILE"
  echo "Copy .env.example to .env and fill in your values."
  exit 1
fi

if [ ! -f "$BINARY" ]; then
  echo "Binary not found. Building..."
  (cd "$SCRIPT_DIR" && go build -o webserver .)
fi

# Load env vars inline in the cron command
ENV_VARS=$(grep -v '^\s*#' "$ENV_FILE" | grep -v '^\s*$' | tr '\n' ' ')

# Remove old cron entry
crontab -l 2>/dev/null | grep -v "$CRON_MARKER" | crontab - 2>/dev/null || true

# Add new cron entry
(crontab -l 2>/dev/null; echo "*/$INTERVAL * * * * cd $SCRIPT_DIR && env $ENV_VARS $BINARY >> $LOG_FILE 2>&1 # $CRON_MARKER") | crontab -

echo "Cron job installed: runs every $INTERVAL minutes"
echo "Logs: $LOG_FILE"
