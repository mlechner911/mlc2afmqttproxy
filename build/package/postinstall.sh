#!/bin/sh
set -e

# Create mlc-edge-proxy user if it doesn't exist
if ! id "mlc-edge-proxy" >/dev/null 2>&1; then
    useradd --system --no-create-home --user-group mlc-edge-proxy
fi

# Ensure data directory exists and is owned by the user
mkdir -p /var/lib/mlc-edge-proxy
chown mlc-edge-proxy:mlc-edge-proxy /var/lib/mlc-edge-proxy

# Reload systemd and start the service
systemctl daemon-reload
systemctl enable --now mlc-edge-proxy.service
