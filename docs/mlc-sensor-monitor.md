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

## Heartbeat und Fernsteuerung

Standardmäßig aktiv (`monitor.enabled`), abschaltbar:

- **Heartbeat:** alle 30 s (`monitor.interval_s`) an `http.endpoint` mit `http.token`. Der Monitor
  legt daraus das Gerät **`svc-edgeproxy`** an und zeigt es auf der Seite **Dienste**. Bleibt der
  Heartbeat aus (Proxy aus, Netz weg, Token gesperrt), meldet der Monitor „Keine Daten".
- **Fernsteuerung** (Seite **Dienste**, Recht „Dienste steuern", mit Begründung, protokolliert):
  **Pausieren** (Versand ruht, die Daten bleiben im Puffer), **Fortsetzen** (Puffer leert sich mit
  den ursprünglichen Zeiten), **Neu starten**, **Stoppen**. Der Proxy empfängt die Befehle über den
  ohnehin genutzten Upstream-Broker (`cmd/svc/edgeproxy`) und bestätigt sie. Ist der Proxy aus,
  nimmt er keine Befehle an — dann startet ihn der System-Administrator auf der Box.

## Prüfen, dass alles ankommt

- Proxy: `curl http://127.0.0.1:8097/api/v1/stats` — Puffer 0, keine Fehler.
- Monitor: Seite **Dienste** — `svc-edgeproxy` läuft; Seite **Messgeräte** — die Zigbee-Geräte
  haben frische Werte.
- Nach einer Unterbrechung: Die Werte aus der Pause stehen im Verlauf an ihrer **Messzeit**, nicht
  gesammelt zum Zeitpunkt der Rückkehr.
