#!/bin/sh
set -e

if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
    # Stop and disable the service only on complete removal, not upgrade
    systemctl stop mlc-edge-proxy.service || true
    systemctl disable mlc-edge-proxy.service || true
fi
