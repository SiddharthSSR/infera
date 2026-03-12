#!/bin/sh

set -eu

TEMPLATE_PATH="/etc/alertmanager/alertmanager.yml.tmpl"
RENDERED_PATH="/tmp/alertmanager.yml"

require_env() {
  var_name="$1"
  eval "var_value=\${$var_name:-}"
  if [ -z "$var_value" ]; then
    echo "missing required environment variable: $var_name" >&2
    exit 1
  fi
}

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\\/&|]/\\&/g'
}

require_env ALERT_EMAIL_TO
require_env ALERT_SMTP_FROM
require_env ALERT_SMTP_SMARTHOST
require_env ALERT_SMTP_USERNAME
require_env ALERT_SMTP_PASSWORD

sed \
  -e "s|\${ALERT_EMAIL_TO}|$(escape_sed_replacement "$ALERT_EMAIL_TO")|g" \
  -e "s|\${ALERT_SMTP_FROM}|$(escape_sed_replacement "$ALERT_SMTP_FROM")|g" \
  -e "s|\${ALERT_SMTP_SMARTHOST}|$(escape_sed_replacement "$ALERT_SMTP_SMARTHOST")|g" \
  -e "s|\${ALERT_SMTP_USERNAME}|$(escape_sed_replacement "$ALERT_SMTP_USERNAME")|g" \
  -e "s|\${ALERT_SMTP_PASSWORD}|$(escape_sed_replacement "$ALERT_SMTP_PASSWORD")|g" \
  "$TEMPLATE_PATH" > "$RENDERED_PATH"

exec /bin/alertmanager \
  --config.file="$RENDERED_PATH" \
  --storage.path=/alertmanager
