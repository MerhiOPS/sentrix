#!/bin/sh
set -e
if ! getent passwd sentrix >/dev/null; then
    useradd --system --home-dir /var/lib/sentrix --no-create-home \
        --shell /usr/sbin/nologin --user-group sentrix
fi
# The data directory must exist before the first start: `tenant create`
# and `user create` run against it first.
install -d -o sentrix -g sentrix -m 0700 /var/lib/sentrix
