#!/bin/sh
# Stop the agent on removal. /var/lib/sentrix-agent (credential, unsent
# batches) is kept; remove it by hand to forget the enrollment.
set -e
if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now sentrix-agent 2>/dev/null || true
fi
