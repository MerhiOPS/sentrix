#!/bin/sh
# Installs the built .deb packages on this (clean, systemd) machine, starts
# both services, enrolls the agent, and checks the host reports. Run by CI;
# needs sudo. Usage: scripts/smoke-deb.sh dist/
set -eu
DIST="${1:-dist}"
sudo dpkg -i "$DIST"/sentrix-server_*_amd64.deb "$DIST"/sentrix-agent_*_amd64.deb
id sentrix-agent
systemctl cat sentrix-agent.service >/dev/null
TID=$(sudo -u sentrix sentrix-server tenant create --data /var/lib/sentrix --name ci | grep -oE '[0-9a-f-]{36}' | head -1)
echo "ci-password-123456" | sudo -u sentrix sentrix-server user create --data /var/lib/sentrix --tenant "$TID" --email ci@example.com
TOKEN=$(sudo -u sentrix sentrix-server token create --data /var/lib/sentrix --tenant "$TID" | grep -oE 'stx1\.[^ ]+' | head -1)
sudo systemctl enable --now sentrix-server
for _ in $(seq 1 40); do curl -sf http://127.0.0.1:8080/readyz >/dev/null && break; sleep 0.5; done
sudo -u sentrix-agent sentrix-agent enroll --state-dir /var/lib/sentrix-agent --server https://127.0.0.1:8443 --token "$TOKEN"
sudo systemctl enable --now sentrix-agent
sleep 25
systemctl is-active sentrix-server sentrix-agent
curl -sf -c /tmp/ci-jar -H 'Origin: http://127.0.0.1:8080' -H 'Content-Type: application/json' \
    -d '{"email":"ci@example.com","password":"ci-password-123456"}' http://127.0.0.1:8080/api/v1/auth/login >/dev/null
curl -sf -b /tmp/ci-jar http://127.0.0.1:8080/api/v1/hosts | python3 -c '
import json, sys
hosts = json.load(sys.stdin)["hosts"]
assert len(hosts) == 1 and hosts[0]["latest"] and hosts[0]["status"] == "ok", hosts
print("PASS: packaged agent reporting:", hosts[0]["name"], hosts[0]["status"])'
sudo systemctl restart sentrix-server sentrix-agent   # a service restart, as after a reboot
sleep 15
systemctl is-active sentrix-server sentrix-agent
sudo journalctl -u sentrix-agent --no-pager | tail -n 5
