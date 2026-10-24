#!/bin/sh
# Stop the server on removal. /var/lib/sentrix (the database) is kept.
set -e
if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now sentrix-server 2>/dev/null || true
fi
