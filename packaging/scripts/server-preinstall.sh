#!/bin/sh
set -e
if ! getent passwd sentrix >/dev/null; then
    useradd --system --home-dir /var/lib/sentrix --no-create-home \
        --shell /usr/sbin/nologin --user-group sentrix
fi
