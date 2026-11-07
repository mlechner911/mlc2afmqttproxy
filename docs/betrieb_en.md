# Operation: usage, authentication, backup, update

[🇩🇪 Deutsch](betrieb.md) · Configuration in detail: [configuration_en.md](configuration_en.md) ·
Connecting to the MLC Sensor Monitor (tokens, tenants): [mlc-sensor-monitor_en.md](mlc-sensor-monitor_en.md)

This page covers **running** the proxy — for any upstream (your own MQTT broker, an HTTP
endpoint, the MLC Sensor Monitor). What only applies to the MLC Sensor Monitor is on its own page.

## How data flows

```
Zigbee devices → zigbee2mqtt → proxy's local broker (:1884) → buffer (BadgerDB, ./data)
                                                                → worker → upstream
                                                                → per route: own buffer → own upstream
```

- zigbee2mqtt (or any MQTT client) **publishes to the proxy**, not directly to the upstream. In z2m:
  `mqtt.server: mqtt://<proxy-host>:1884`.
- Every message is **written to the on-disk buffer first**, with its **receive time**. Only then
  does the worker try to forward it. If the upstream is unreachable, it stays in the buffer — even
  across a restart or power loss — and is sent later with its **original time**
  (`mqtt.timestamp_mode`).
- A message is deleted only once the upstream has accepted it. If the upstream rejects it
  **permanently** (MQTT 5 reason code, e.g. invalid topic), only that one is dropped; the rest
  continues.

## Authenticating to the upstream

The proxy authenticates the way the upstream requires. In general:

| Upstream | In `config.yaml` | Meaning |
| :-- | :-- | :-- |
| MQTT broker | `mqtt.upstream`, `mqtt.username`, `mqtt.password` | user and password as checked by the broker |
| HTTP endpoint | `http.endpoint`, `http.token` | token as a header (MLC Sensor Monitor: `X-Ingest-Token`) |
| Additional targets (routes) | `routes[].upstream`, `.username`, `.password` | own credentials per route |

- **What user and password mean is up to the upstream.** A typical Mosquitto broker checks both
  against its password file; the MLC Sensor Monitor only evaluates the password (token = tenant),
  see [mlc-sensor-monitor_en.md](mlc-sensor-monitor_en.md).
- **Wrong or revoked credentials:** the upstream refuses the connection. The proxy **keeps
  buffering** (nothing is lost), retries with growing intervals (`worker.retry_min_s` …
  `retry_max_s`) and logs it (`journalctl -u mlc-edge-proxy`). On the dashboard
  `messages_forward_failed_total` rises and the buffer grows. With the right credentials and a
  restart it drains.
- **Changing credentials:** create the new credential at the upstream, put it into `config.yaml`,
  restart, verify (buffer drains, no errors), **then** revoke the old one — so there is never a
  moment without a valid credential.
- **Credentials belong only in `config.yaml`** — never in `config.example.yaml`, never in the
  repository (`config.yaml` is in `.gitignore`). The repository is public.

## Usage

| What | How |
| :-- | :-- |
| Start, stop, restart | `sudo systemctl restart mlc-edge-proxy` (unit: `deployment/proxy.service` or `deploy/systemd/`) |
| Running, which version? | `curl http://127.0.0.1:8097/api/v1/health` → `{"status":"ok","version":"…"}` |
| Buffer and counters | `curl http://127.0.0.1:8097/api/v1/stats` → `buffer_count`, `messages_forwarded_total`, `messages_forward_failed_total` … |
| Dashboard | `http://<host>:8097/` — buffer, live tree of MQTT topics |
| Log | `journalctl -u mlc-edge-proxy -f` |
| Pause/resume forwarding | via the upstream (MLC Sensor Monitor: **Services** page), see there |

**Reading the buffer:** `buffer_count` 0 = everything forwarded. If it grows while
`messages_forward_failed_total` stays flat, forwarding is paused — otherwise the proxy cannot reach
the upstream (network, credentials). If the buffer space is exhausted (`storage.max_size_mb`), the
proxy accepts **no new** messages rather than filling the SD card ("tail drop").

## Backing up the configuration

`config.yaml` holds the credentials and is **not** in the repository. If it is lost, the proxy will
not start on the next restart — the running instance does not notice; only the restart (or a power
cut) does.

- After each change, keep a copy **outside** the repository directory, readable only by the
  service user, e.g.:
  ```bash
  install -d -m 700 ~/.config/mlc-edge-proxy
  install -m 600 config.yaml ~/.config/mlc-edge-proxy/config.yaml.$(date +%F)
  ```
- **No backup copies inside the repository directory** (`config.yaml.bak` …): `.gitignore` only
  covers `config.yaml`; a `git add -A` would push the copy with its credentials to the public
  repository.

## Updating

- **Never** run `git checkout`, `git switch` or `git pull` in the directory the service runs from.
  Git overwrites **ignored** files on a switch without warning — including `config.yaml`. (That is
  how the configuration was lost on the development server on 7 Nov 2026.)
- Instead install the package (`task package` → `build/dist/*.deb|.rpm`), or build in a **separate**
  directory and only replace the binary:
  ```bash
  git worktree add /tmp/proxy-build main && cd /tmp/proxy-build && task build-server
  install -m 755 bin/mlc-edge-proxy /path/to/service/bin/mlc-edge-proxy
  sudo systemctl restart mlc-edge-proxy
  ```
- The buffer survives the update. Entries still buffered in the **old format** (before 7 Nov 2026:
  time as key) are forwarded first after the update, with their time.
- After the restart: `health` (version), `stats` (no errors, buffer drains), log.

## Security — operating conditions

- **The local broker (`:1884`, WebSocket `:1885`) listens on `127.0.0.1` only** (`mqtt.bind`).
  Whatever arrives there is forwarded with valid credentials into the records — until 7 Nov 2026
  it accepted any connection from anywhere without authentication. If zigbee2mqtt runs on another
  machine: set `mqtt.bind` to the box's address and an account in `mqtt.local_users` (in z2m
  `mqtt.user`/`mqtt.password`); without an account the proxy then refuses to start. Loopback
  connections still need no login.
- **Dashboard** (`:8097`): without `server.username`/`server.password` only on `127.0.0.1`; with
  a login on `server.host`. The live stream (`/mqtt`) is only available through the dashboard and
  only from the same origin (protection against foreign web pages in the browser). The login is
  HTTP basic auth — unencrypted over the LAN; outside a trusted network use an encrypted access
  (VPN, SSH tunnel).
- **An upstream broker in front** (e.g. mosquitto with `mqttbridge --bidi`) needs the same care:
  whatever reaches it reaches the proxy through the bridge.
- **Encrypt the connection to the upstream** as soon as it leaves the premises (`ssl://`/`wss://`
  or `https://`).
- The proxy is checked with every MLC Sensor Monitor release (`task edge:check`: vet, tests, known
  vulnerabilities, semgrep, scan for credentials in the repository).
