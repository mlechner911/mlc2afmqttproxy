# Best Setup & Integration: Edge Ingestion

Dieses Dokument beschreibt die Best-Practice Architektur für ein maximal ausfallsicheres lokales Setup von Zigbee-Sensoren in Kombination mit dem **MLC Edge Proxy**.

## Architektur & Hardware-Empfehlung

Um Telemetriedaten selbst bei massiven Störungen (Stromausfall, Internetausfall) nicht zu verlieren, empfehlen wir folgendes Hardware-Setup:

```mermaid
graph TD
    subgraph "Sensoren (Batteriebetrieben)"
        Z1((Zigbee Sensor))
        Z2((Zigbee Sensor))
        Z3((Zigbee Sensor))
    end

    subgraph "Edge Node (z.B. Raspberry Pi / Orange Pi)"
        subgraph "Software"
            Z2M[Zigbee2MQTT]
            MLC[MLC Edge Proxy]
            Storage[(BadgerDB Buffer)]
        end
        
        EthDongle[Zigbee Ethernet-Coordinator]
    end

    subgraph "Netzwerk / Cloud"
        Cloud[Cloud MQTT Broker]
        Backend[Backend Service]
    end

    USV((USV / Mini-USV))
    Switch[5-Port Switch]

    Z1 -.->|Zigbee| EthDongle
    Z2 -.->|Zigbee| EthDongle
    Z3 -.->|Zigbee| EthDongle

    EthDongle -->|Ethernet TCP/IP| Switch
    Switch -->|Ethernet| Z2M
    Z2M -->|Lokales MQTT 127.0.0.1| MLC
    MLC -->|Schreibt auf SSD/SD| Storage
    MLC ==>|Store & Forward via Internet| Cloud
    Cloud --> Backend

    USV -.->|Strom| Edge Node
    USV -.->|Strom| Switch
    USV -.->|Strom| EthDongle
```

## Die kritischen Hardware-Komponenten

### 1. Die richtige Zigbee-Hardware (Ethernet statt USB)
Viele Nutzer beginnen mit einfachen USB-Zigbee-Sticks. Dies kann jedoch auf Dauer zu **Interferenzen** führen, da USB 3.0 und 2.4 GHz WLAN die Zigbee-Frequenzen stark stören.

**Unsere Best-Practice:** Nutze einen **Ethernet Zigbee Coordinator** (z.B. SMLight, TubesZB oder ähnliche PoE-Gateways). 
- **Vorteil 1 (Abstand):** Der Coordinator kann räumlich weit entfernt vom Raspberry Pi und störenden WLAN-Antennen platziert werden (Stichwort: Funkhygiene).
- **Vorteil 2 (Netzwerk):** Falls dein Raspberry Pi im Hauptnetzwerk primär über WLAN verbunden ist, kannst du den Ethernet-Dongle direkt mit einem Kabel an den lokalen Ethernet-Port des Pi anschließen.
- **Tipp:** Wenn du den Ethernet-Port deines Pi bereits für die Internetverbindung nutzt, schließe einfach einen **minimalen 5-Port Switch** vor den Pi, an den du sowohl den Zigbee-Coordinator als auch den Pi hängst.

### 2. Stromausfall-Absicherung (USV)
Fällt in einer Fabrik oder im Büro der Strom aus, gehen in der Regel alle Sensordaten verloren, und Router stürzen ab. 
- Da Zigbee-Sensoren meist batteriebetrieben sind, messen und funken sie bei einem Stromausfall unbeeindruckt weiter.
- Hängt dein Raspberry Pi, der Ethernet-Switch und der Zigbee-Coordinator an einer kleinen, kostengünstigen **USV (Unterbrechungsfreie Stromversorgung)** oder einer passenden DC-Mini-USV, bleiben diese kritischen Kernkomponenten am Leben.
- Der MLC Edge Proxy nimmt die Daten der Sensoren lokal über Zigbee2MQTT an und speichert sie direkt in die lokale `BadgerDB` auf der SD-Karte oder SSD.

### 3. Der Verbindungsabbruch zur Cloud
Selbst wenn die USV den Pi am Leben hält: Dein Internet-Router hat bei einem kompletten Stromausfall im Haus wahrscheinlich keinen Saft mehr, oder dein Provider hat eine Störung (LTE-Ausfall).
- Das ist exakt der Anwendungsfall des MLC Edge Proxys.
- Er fängt alle gesammelten Sensordaten ab. Sobald das Internet wieder da ist, wird der **Forwarder** aktiv und sendet alle aufgestauten Daten **verlustfrei und in der korrekten historischen Reihenfolge** an die Cloud.

## Fazit
Mit einem Raspberry Pi, einer Mini-USV, einem Ethernet Zigbee-Dongle und dem MLC Edge Proxy baust du eine extrem robuste, industrietaugliche Ingestion-Pipeline für wenige hundert Euro, bei der garantiert keine Sensor-Pakete mehr verloren gehen.
