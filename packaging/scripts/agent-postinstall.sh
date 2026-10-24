#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi
cat <<'MSG'
sentrix-agent installed. Next:
  sudo -u sentrix-agent sentrix-agent enroll --state-dir /var/lib/sentrix-agent \
      --server https://YOUR-SERVER:8443 --token TOKEN
  sudo systemctl enable --now sentrix-agent
MSG
