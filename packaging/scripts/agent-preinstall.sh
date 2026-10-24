#!/bin/sh
# Create the agent's dedicated system user. It reads logs through group
# membership (adm, systemd-journal) where those groups exist, never as root.
set -e
if ! getent passwd sentrix-agent >/dev/null; then
    useradd --system --home-dir /var/lib/sentrix-agent --no-create-home \
        --shell /usr/sbin/nologin --user-group sentrix-agent
fi
for g in adm systemd-journal; do
    if getent group "$g" >/dev/null; then
        usermod -a -G "$g" sentrix-agent
    fi
done
# The state directory must exist before the first start: enrollment writes
# the identity into it (systemd's StateDirectory only appears at start).
install -d -o sentrix-agent -g sentrix-agent -m 0700 /var/lib/sentrix-agent
