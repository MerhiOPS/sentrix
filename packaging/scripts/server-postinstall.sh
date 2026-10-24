#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi
cat <<'MSG'
sentrix-server installed. Next:
  sudo -u sentrix sentrix-server tenant create --data /var/lib/sentrix --name "My company"
  sudo -u sentrix sentrix-server user create   --data /var/lib/sentrix --tenant TENANT_ID --email you@example.com
  sudo systemctl enable --now sentrix-server
MSG
