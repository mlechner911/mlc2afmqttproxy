# Anbindung an den MLC Sensor Monitor

[🇬🇧 English](mlc-sensor-monitor_en.md) · Allgemeiner Betrieb: [betrieb.md](betrieb.md)

Diese Seite gilt **nur**, wenn der Upstream der **MLC Sensor Monitor** ist
([mlcgo.eu](https://mlcgo.eu)). Andere Upstreams melden sich anders an (siehe
[betrieb.md › Anmeldung am Upstream](betrieb.md#anmeldung-am-upstream)).

## Das Ingest-Token

Der MLC Sensor Monitor nimmt Daten mit einem **Ingest-Token** an:

- **MQTT** (eingebetteter Broker des Monitors, Standard-Port 51883 bzw. `wss://…/mqtt`): das
  Token ist das **Passwort**. Der **Benutzername** wird nur protokolliert — sinnvoll ist die
  Kundennummer oder der Name der Box, damit man die Verbindung im Protokoll erkennt.
- **HTTP** (`POST /api/v1/ingest`): das Token als Header `X-Ingest-Token`.

**Das Token ist die Mandantengrenze.** Aus ihm ergibt sich, in welchen Mandanten (Kunden) die
Daten gehen — nicht aus dem Benutzernamen und nicht aus dem Topic. Ein Token gehört genau zu
einem Mandanten.

### Woher das Token kommt

- In der **Anwendung** des Mandanten: **MQTT-Quellen › Ingest-Tokens › „Token erzeugen"**
  (Recht „Firmen-Stammdaten verwalten"). Der Klartext wird **genau einmal** angezeigt — sofort in
  `config.yaml` eintragen. Gespeichert wird beim Monitor nur eine Prüfsumme.
- Für Kunden richtet es der Betreiber ein (Betreiber-Konsole › Mandant).
- **„Sperren"** macht ein Token sofort ungültig.

### Wohin es in `config.yaml` gehört

```yaml
mqtt:
  upstream: "tcp://127.0.0.1:51883"      # Monitor auf demselben Host; sonst ssl://<host>:… bzw. wss://
  username: "box-lager-1"                # nur zur Erkennung im Protokoll
  password: "<Ingest-Token des Mandanten>"
  timestamp_mode: "v5_property"          # Empfangszeit mitschicken (Nachlieferung)

http:
  endpoint: "http://127.0.0.1:58080/api/v1/ingest"
  token: "<dasselbe Ingest-Token>"       # für den Heartbeat — derselbe Mandant wie mqtt.password
```

`http.token` **muss** gleich `mqtt.password` sein: Der Heartbeat (siehe unten) geht über HTTP; mit
einem anderen Token landete der Dienst in einem anderen Mandanten als die Daten.

### Falsches oder gesperrtes Token

- Der Monitor lehnt die MQTT-Verbindung ab („bad username or password"); im Protokoll des
  Monitors steht die Ablehnung mit Client-ID und Adresse.
- Der Proxy **puffert weiter**, nichts geht verloren (siehe [betrieb.md](betrieb.md)).
- Der Heartbeat bleibt aus → der Monitor meldet für den Dienst `svc-edgeproxy` nach kurzer Zeit
  **„Keine Daten"** — so fällt ein abgelaufenes oder gesperrtes Token auf.
- Mit gültigem Token und Neustart leert sich der Puffer; die Werte kommen mit ihrer echten Zeit
  an.

### Token wechseln

1. Neues Token erzeugen (MQTT-Quellen).
2. In `config.yaml` bei `mqtt.password` **und** `http.token` eintragen (bei einer Route deren
   `password`). Konfiguration sichern ([betrieb.md › Konfiguration sichern](betrieb.md#konfiguration-sichern)).
3. `sudo systemctl restart mlc-edge-proxy`; prüfen: Protokoll ohne Ablehnung, `stats` ohne
   Fehler, im Monitor kommen Werte und der Heartbeat.
4. **Erst dann** das alte Token sperren.

## Mehrere Mandanten von einer Box (Routen)

Hängen an einer Box Geräte verschiedener Kunden, schickt eine **Route** ausgewählte Geräte mit
einem **eigenen Token** in einen anderen Mandanten:

```yaml
routes:
  - name: demo                            # Puffer unter <storage.path>/routes/demo
    upstream: "tcp://127.0.0.1:51883"
    username: "box-lager-1-demo"
    password: "<Ingest-Token des anderen Mandanten>"
    topic_alias: true
    devices:                              # IEEE-Adressen (bleiben gleich, auch wenn das Gerät in z2m umbenannt wird)
      - "0xa4c1388e4c14c889"
```

- Jede Route hat ihren **eigenen Puffer und Worker** — ist ein Ziel nicht erreichbar, hält das die
  anderen nicht auf — und eine eigene MQTT-Client-ID.
- Mit `devices` gehen **nur** diese Geräte über die Route hinaus, dazu eine auf sie gekürzte
  Geräteliste (`bridge/devices`); andere `bridge/*`-Meldungen nie.
- Beim Start steht im Protokoll: `[Route demo] aktiv: Modus mqtt, 6 Gerät(e)`.
- Über die Route kann der Mandant **ihre** Geräte schalten ([Rückweg](#geräte-schalten-rückweg)).

## Heartbeat und Fernsteuerung

Standardmäßig aktiv (`monitor.enabled`), abschaltbar:

- **Heartbeat:** alle 30 s (`monitor.interval_s`) an `http.endpoint` mit `http.token`. Der Monitor
  legt daraus das Gerät **`svc-edgeproxy`** an und zeigt es auf der Seite **Dienste**. Bleibt der
  Heartbeat aus (Proxy aus, Netz weg, Token gesperrt), meldet der Monitor „Keine Daten".
  Mitgeschickt werden `service_up`, `service_state` (1 läuft · 2 pausiert · 3 leert),
  `svc_uptime_s`, `svc_buffer_pending`, `svc_buffer_mb`, `svc_upstream_connected`,
  `svc_zigbee_connected`, `svc_last_forward_age_s`, `svc_last_receive_age_s`,
  `svc_forward_failed_total`, `svc_dropped_full_total`, `svc_dead_lettered_total` sowie
  `meta.svc_version` / `meta.svc_product`. Ein Proxy kann „läuft" melden und trotzdem nichts
  weiterleiten — diese Werte zeigen es.
- **Fernsteuerung** (Seite **Dienste**, Recht „Dienste steuern", mit Begründung, protokolliert):
  **Pausieren** (Versand ruht, die Daten bleiben im Puffer), **Fortsetzen** (Puffer leert sich mit
  den ursprünglichen Zeiten), **Neu starten**, **Stoppen**. Der Proxy empfängt die Befehle über den
  ohnehin genutzten Upstream-Broker (`cmd/svc/edgeproxy`) und bestätigt sie. Ist der Proxy aus,
  nimmt er keine Befehle an — dann startet ihn der System-Administrator auf der Box.
  - Befehle sendet nur der Monitor selbst; ein Gerät oder ein anderer Zugang kann dort nicht
    schreiben (Monitor seit 07.11.2026).
  - Eine Pause endet spätestens nach `monitor.max_pause_minutes` (Standard 60) von selbst — eine
    vergessene Pause füllte sonst den Puffer, bis neue Werte verloren gehen.
  - Ein gespeicherter (retained) Befehl wird nicht ausgeführt: Er käme sonst bei jedem Neustart
    wieder.
  - **Neu starten** endet mit Exit-Code 75, damit auch eine Unit mit `Restart=on-failure` neu
    startet; **Stoppen** endet mit 0 — die Box bleibt aus.

## Geräte schalten (Rückweg)

Seit 1.5 schaltet der Monitor Lampen, Steckdosen und Rollläden über den Proxy. Standardmäßig an,
auf der Box ist nichts einzurichten (abschalten: `control.enabled: false`).

```
Monitor ──cmd/zb/<…>/<ieee>/set──▶ Proxy ──zigbee2mqtt/<gerät>/set──▶ zigbee2mqtt ──▶ Gerät
   ▲                                 │                                                  │
   └──────cmd/zb/<…>/<ieee>/ack──────┘◀──────── zigbee2mqtt/<gerät> {"state":"ON"} ──────┘
```

1. Der Monitor schickt z. B. `{"state":"ON","_id":"a1b2"}` an `cmd/zb/<kundennr>/<ieee>/set`.
   `_id` ist Pflicht; sie geht nicht an zigbee2mqtt.
2. Der Proxy abonniert `cmd/zb/#` über die Verbindung des Haupt-Upstreams und jeder Route. Der
   Monitor stellt jeder Verbindung nur die Befehle ihres eigenen Mandanten zu und lässt nur
   seinen eigenen Server Befehle schreiben — eine Kundennummer braucht der Proxy nicht.
3. Der Proxy prüft: Gehört das Gerät zum Mandanten (Route: ihre `devices`), ist das Feld erlaubt
   (`control.allow`, Vorgabe: Bedienfelder), kann das Gerät es laut zigbee2mqtt setzen, ist es
   online, höchstens 10 Befehle je Minute.
4. Er übersetzt die IEEE-Adresse in den Namen bei zigbee2mqtt und schickt `{"state":"ON"}` an
   `zigbee2mqtt/<gerät>/set`.
5. Die Quittung geht auf dasselbe Topic mit `/ack`:
   - `{"id":"a1b2","result":"ok","state":{"state":"ON"}}` — erst, wenn das Gerät den Zustand meldet,
   - `timeout` — keine passende Meldung binnen `control.timeout_s` (10 s), mit dem letzten Zustand,
   - `rejected` mit `reason`: `device not allowed`, `field not allowed: …`,
     `field not writable on this device: …`, `device unknown to zigbee2mqtt`, `device offline`,
     `rate limit`, `zigbee2mqtt not reachable`, `missing _id`, `invalid payload: JSON object expected`,
     `no fields to set`.

**Der Proxy schaltet nie selbst** — keine Regeln, keine Zeitpläne. Befehle werden **nie
gepuffert**: Ein gespeicherter (retained) Befehl wird verworfen, und was während eines Ausfalls
geschickt wird, kommt nie verspätet an (Verbindung ohne gespeicherte Sitzung). Befehle, die
lokal an zigbee2mqtt gehen (`…/set`, `…/get`, `bridge/request/…`, etwa von Home Assistant),
gehen nicht an den Upstream.

Im Protokoll: `[control demo] abonniert: cmd/zb/#`, je Befehl
`[control demo] 0x… map[state:ON] → ok`. Lehnt der Monitor das Abo ab, steht dort
`Abo cmd/zb/# vom Broker ABGELEHNT (0x87)`. In der Oberfläche zeigt der Tab **Filters** die Karte
„Remote control" mit Umfang, Zählern und den letzten Befehlen (`/api/v1/control`).

Einstellungen: [configuration.md › Rückweg](configuration.md#rückweg-geräte-aus-dem-monitor-schalten).

## Prüfen, dass alles ankommt

- Proxy: `curl http://127.0.0.1:8097/api/v1/stats` — Puffer 0, keine Fehler.
- Monitor: Seite **Dienste** — `svc-edgeproxy` läuft; Seite **Messgeräte** — die Zigbee-Geräte
  haben frische Werte.
- Nach einer Unterbrechung: Die Werte aus der Pause stehen im Verlauf an ihrer **Messzeit**, nicht
  gesammelt zum Zeitpunkt der Rückkehr.
