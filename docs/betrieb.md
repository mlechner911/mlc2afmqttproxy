# Betrieb: Bedienung, Anmeldung, Sicherung, Update

[🇬🇧 English](betrieb_en.md) · Konfiguration im Detail: [configuration.md](configuration.md) ·
Anbindung an den MLC Sensor Monitor (Token, Mandanten): [mlc-sensor-monitor.md](mlc-sensor-monitor.md)

Diese Seite beschreibt den **laufenden Betrieb** des Proxys — für jeden Upstream (eigener
MQTT-Broker, HTTP-Endpunkt, MLC Sensor Monitor). Was nur für den MLC Sensor Monitor gilt, steht
auf der eigenen Seite.

## So fließen die Daten

```
Zigbee-Geräte → zigbee2mqtt → lokaler Broker des Proxys (:1884) → Puffer (BadgerDB, ./data)
                                                                      → Worker → Upstream
                                                                      → je Route: eigener Puffer → eigener Upstream
```

- zigbee2mqtt (oder ein anderer MQTT-Client) **publiziert an den Proxy**, nicht direkt an den
  Upstream. In z2m: `mqtt.server: mqtt://<host-des-proxys>:1884`.
- Jede Nachricht landet **zuerst im Puffer** auf der Platte, mit ihrer **Empfangszeit**. Erst
  dann versucht der Worker, sie weiterzuleiten. Ist der Upstream nicht erreichbar, bleibt sie im
  Puffer — auch über einen Neustart oder Stromausfall hinweg — und geht später mit der
  **ursprünglichen Zeit** hinaus (`mqtt.timestamp_mode`).
- Gelöscht wird eine Nachricht erst, wenn der Upstream sie angenommen hat. Lehnt er sie
  **endgültig** ab — nur bei Formfehlern: MQTT 0x90 ungültiges Topic, 0x95 zu groß, 0x99
  Nutzlastformat; HTTP 400, 413, 415, 422; ungültiges JSON —, wird sie in
  **`<storage.path>/dead-letter.jsonl`** abgelegt (je Route im Routen-Puffer; mit Empfangszeit,
  Topic, Grund und Nutzlast) und erst dann aus dem Puffer genommen; der Rest läuft weiter. Eine
  abgelegte Messung fehlt im Monitor — sie ist ein Befund, den jemand ansehen muss
  (`messages_dead_lettered_total` in `stats`). Lässt sich die Datei nicht schreiben, bleibt die
  Nachricht im Puffer.
- Jede andere Ablehnung — auch „nicht autorisiert" (gesperrtes Token, übernommene Verbindung) —
  gilt als vorübergehend: Die Nachricht bleibt, der Proxy versucht es mit wachsendem Abstand
  erneut. Lieber steht die Weiterleitung, als dass Messwerte verschwinden.
- Größte angenommene Nachricht 1 MiB. Ist der Puffer voll (`storage.max_size_mb`), nimmt der
  Proxy nichts mehr an (Tail-Drop); diese Werte fehlen — gezählt in
  `messages_dropped_buffer_full_total`.

## Anmeldung am Upstream

Der Proxy meldet sich am Upstream so an, wie der Upstream es verlangt. Allgemein:

| Upstream | Wo in `config.yaml` | Bedeutung |
| :-- | :-- | :-- |
| MQTT-Broker | `mqtt.upstream`, `mqtt.username`, `mqtt.password` | Benutzer und Passwort, wie der Broker sie prüft |
| HTTP-Endpunkt | `http.endpoint`, `http.token` | Token als Header (beim MLC Sensor Monitor `X-Ingest-Token`) |
| Weitere Ziele (Routen) | `routes[].upstream`, `.username`, `.password` | eigener Zugang je Route |

- **Was Benutzer und Passwort bedeuten, bestimmt der Upstream.** Ein üblicher Mosquitto-Broker
  prüft beides gegen seine Passwortdatei; der MLC Sensor Monitor wertet nur das Passwort aus
  (Token = Mandant), siehe [mlc-sensor-monitor.md](mlc-sensor-monitor.md).
- **Zugang falsch oder gesperrt:** Der Upstream lehnt die Verbindung ab. Der Proxy **puffert
  weiter** (nichts geht verloren), versucht es mit wachsendem Abstand erneut
  (`worker.retry_min_s` … `retry_max_s`) und schreibt ins Protokoll
  (`journalctl -u mlc-edge-proxy`). Im Dashboard steigt `messages_forward_failed_total`, der
  Puffer wächst. Mit dem richtigen Zugang und einem Neustart leert er sich.
- **Zugang wechseln:** neuen Zugang beim Upstream anlegen, in `config.yaml` eintragen, Proxy
  neu starten, prüfen (Puffer leert sich, keine Fehler), **dann** den alten Zugang sperren. So
  gibt es keinen Moment ohne gültigen Zugang.
- **Zugangsdaten gehören nur in `config.yaml`** — nie in `config.example.yaml`, nie ins Repository
  (`config.yaml` steht in `.gitignore`). Das Repository ist öffentlich.

## Bedienung

| Was | Wie |
| :-- | :-- |
| Starten, Stoppen, Neustart | `sudo systemctl restart mlc-edge-proxy` (Unit: `deployment/proxy.service` bzw. `deploy/systemd/`) |
| Läuft er, welche Version? | `curl http://127.0.0.1:8097/api/v1/health` → `{"status":"ok","version":"…"}` |
| Puffer und Zähler | `curl http://127.0.0.1:8097/api/v1/stats` → `buffer_count`, `messages_forwarded_total`, `messages_forward_failed_total` … |
| Dashboard | `http://<host>:8097/` — Pufferstand, Live-Baum der MQTT-Topics |
| Protokoll | `journalctl -u mlc-edge-proxy -f` |
| Versand anhalten/fortsetzen | über den Upstream (beim MLC Sensor Monitor: Seite **Dienste**), siehe dort |

**Puffer lesen:** `buffer_count` 0 = alles weitergeleitet. Wächst er und bleibt
`messages_forward_failed_total` stehen, ist der Versand angehalten (Pause) — sonst erreicht der
Proxy den Upstream nicht (Netz, Zugang). Ist der Platz für den Puffer erschöpft
(`storage.max_size_mb`), nimmt der Proxy **keine neuen** Nachrichten mehr an, statt die SD-Karte
vollzuschreiben („Tail Drop").

## Konfiguration sichern

`config.yaml` enthält die Zugangsdaten und ist **nicht** im Repository. Geht sie verloren, startet
der Proxy beim nächsten Neustart nicht — die laufende Instanz merkt davon nichts, erst der
Neustart (oder ein Stromausfall).

- Nach jeder Änderung eine Kopie **außerhalb** des Repository-Verzeichnisses ablegen, nur für den
  Dienst-Benutzer lesbar, z. B.:
  ```bash
  install -d -m 700 ~/.config/mlc-edge-proxy
  install -m 600 config.yaml ~/.config/mlc-edge-proxy/config.yaml.$(date +%F)
  ```
- **Keine Sicherungskopien im Repository-Verzeichnis** (`config.yaml.bak` …): `.gitignore` deckt
  nur `config.yaml` ab, ein `git add -A` nähme die Kopie samt Zugangsdaten mit ins öffentliche
  Repository.

## Update

- **Nie** im Verzeichnis, aus dem der Dienst läuft, `git checkout`, `git switch` oder `git pull`
  ausführen. `git` überschreibt **ignorierte** Dateien beim Wechsel ohne Warnung — dazu gehört
  `config.yaml`. (So ist die Konfiguration am 07.11.2026 auf dem Entwicklungsserver verloren
  gegangen.)
- Stattdessen: das Paket installieren (`task package` → `build/dist/*.deb|.rpm`), oder in einem
  **getrennten** Verzeichnis bauen und nur das Programm austauschen:
  ```bash
  git worktree add /tmp/proxy-build main && cd /tmp/proxy-build && task build-server
  install -m 755 bin/mlc-edge-proxy /pfad/zum/dienst/bin/mlc-edge-proxy
  sudo systemctl restart mlc-edge-proxy
  ```
- Der Puffer übersteht das Update. Einträge, die noch im **alten Format** gepuffert sind (vor
  07.11.2026: Uhrzeit als Schlüssel), werden nach dem Update zuerst und mit ihrer Zeit
  weitergeleitet.
- Nach dem Neustart: `health` (Version), `stats` (keine Fehler, Puffer leert sich), Protokoll.

## Sicherheit — Auflagen für den Betrieb

- **Der lokale Broker (`:1884`, WebSocket `:1885`) lauscht nur auf `127.0.0.1`** (`mqtt.bind`).
  Was dort ankommt, leitet der Proxy mit gültigem Zugang in den Nachweis weiter — bis 07.11.2026
  nahm er jede Verbindung von überall ohne Anmeldung an. Läuft zigbee2mqtt auf einem anderen
  Rechner: `mqtt.bind` auf die Adresse der Box und ein Konto in `mqtt.local_users` (in z2m
  `mqtt.user`/`mqtt.password`); ohne Konto startet der Proxy dann nicht. Von Loopback geht es
  weiter ohne Anmeldung.
- **Dashboard** (`:8097`): ohne `server.username`/`server.password` nur auf `127.0.0.1`; mit
  Anmeldung auf `server.host`. Der Live-Datenstrom (`/mqtt`) geht nur über das Dashboard und nur
  von derselben Herkunft (Schutz gegen fremde Webseiten im Browser). Anmeldung ist HTTP Basic
  Auth — über das LAN unverschlüsselt; außerhalb eines vertrauten Netzes nur über einen
  verschlüsselten Zugang (VPN, SSH-Tunnel).
- **Ein vorgeschalteter Broker** (z. B. mosquitto mit `mqttbridge --bidi`) braucht dieselbe
  Sorgfalt: Was ihn erreicht, erreicht über die Bridge auch den Proxy.
- **Zum Upstream nur verschlüsselt**, sobald die Verbindung das Haus verlässt (`ssl://`/`wss://`
  bzw. `https://`).
- Geprüft wird der Proxy bei jeder Version des MLC Sensor Monitor mit (`task edge:check`: vet,
  Tests, bekannte Schwachstellen, semgrep, Suche nach Zugangsdaten im Repository).
