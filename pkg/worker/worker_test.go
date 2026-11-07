package worker

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/storage"
)

// ziel: nachgebautes Upstream-Ziel — erreichbar oder nicht, lehnt bestimmte Topics dauerhaft ab.
type ziel struct {
	erreichbar bool
	ablehnen   map[string]bool
	gesendet   []gesendet
}

type gesendet struct {
	topic string
	zeit  time.Time
}

func (z *ziel) Send(topic string, _ []byte, ts time.Time) error {
	if !z.erreichbar {
		return errors.New("Ziel nicht erreichbar")
	}
	if z.ablehnen[topic] {
		return &forwarder.PermanentError{Err: errors.New("abgelehnt")}
	}
	z.gesendet = append(z.gesendet, gesendet{topic, ts})
	return nil
}
func (z *ziel) IsConnected() bool { return z.erreichbar }
func (z *ziel) Connect() error {
	if !z.erreichbar {
		return errors.New("Ziel nicht erreichbar")
	}
	return nil
}
func (z *ziel) Close()                                                {}
func (z *ziel) Subscribe([]string, forwarder.DownstreamHandler) error { return nil }
func (z *ziel) SetDownstreamHandler(forwarder.DownstreamHandler)      {}

func puffer(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.InitBadger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func ablegen(t *testing.T, s *storage.Store, topic string, zeit time.Time) {
	t.Helper()
	v, _ := json.Marshal(broker.PayloadWrapper{Topic: topic, Payload: []byte(`{"temperature":5}`), Time: zeit.UTC().Format(time.RFC3339Nano)})
	if _, err := s.Append(v); err != nil {
		t.Fatal(err)
	}
}

func neuerWorker(s *storage.Store, z *ziel) *Worker {
	return New(s, z, config.WorkerConf{IntervalMs: 10, MaxBatchSize: 100, RetryMinS: 1, RetryMaxS: 60})
}

// Ziel weg (Internet aus): Nichts wird gelöscht, alles bleibt im Puffer. Ziel zurück: Alles geht
// in Reihenfolge hinaus, jede Nachricht mit ihrer ECHTEN Empfangszeit (nicht der Sendezeit), danach
// ist der Puffer leer. Das ist das Versprechen des Proxys.
func TestZielWegNichtsVerlorenZielZurueckAllesInReihenfolge(t *testing.T) {
	s := puffer(t)
	z := &ziel{}
	w := neuerWorker(s, z)
	beginn := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for i, topic := range []string{"zigbee2mqtt/a", "zigbee2mqtt/b", "zigbee2mqtt/c"} {
		ablegen(t, s, topic, beginn.Add(time.Duration(i)*time.Minute))
	}

	w.runBatch()
	if n, _ := s.GetSize(); n != 3 || len(z.gesendet) != 0 {
		t.Fatalf("Ziel weg: Puffer %d (erwartet 3), gesendet %d (erwartet 0)", n, len(z.gesendet))
	}

	z.erreichbar = true
	w.nextAttempt = time.Time{} // Backoff abgelaufen
	w.runBatch()
	if n, _ := s.GetSize(); n != 0 {
		t.Errorf("Ziel zurück: %d Einträge im Puffer, erwartet 0", n)
	}
	if len(z.gesendet) != 3 {
		t.Fatalf("%d gesendet, erwartet 3", len(z.gesendet))
	}
	for i, g := range z.gesendet {
		if want := beginn.Add(time.Duration(i) * time.Minute); !g.zeit.Equal(want) {
			t.Errorf("#%d %s mit Zeit %v, erwartet die Empfangszeit %v", i, g.topic, g.zeit, want)
		}
	}
	if z.gesendet[0].topic != "zigbee2mqtt/a" || z.gesendet[2].topic != "zigbee2mqtt/c" {
		t.Errorf("Reihenfolge %v", z.gesendet)
	}
}

// Eine Nachricht, die das Ziel dauerhaft ablehnt (MQTT-5-Reason-Code), wird verworfen — nur sie;
// die übrigen gehen weiter hinaus, statt hinter ihr festzuhängen.
func TestAbgelehnteNachrichtHaeltDieAnderenNichtAuf(t *testing.T) {
	s := puffer(t)
	z := &ziel{erreichbar: true, ablehnen: map[string]bool{"kaputt/topic": true}}
	w := neuerWorker(s, z)
	jetzt := time.Now()
	ablegen(t, s, "zigbee2mqtt/a", jetzt)
	ablegen(t, s, "kaputt/topic", jetzt)
	ablegen(t, s, "zigbee2mqtt/b", jetzt)
	w.runBatch()
	if n, _ := s.GetSize(); n != 0 {
		t.Errorf("%d im Puffer, erwartet 0", n)
	}
	if len(z.gesendet) != 2 || z.gesendet[0].topic != "zigbee2mqtt/a" || z.gesendet[1].topic != "zigbee2mqtt/b" {
		t.Errorf("gesendet %v, erwartet a und b", z.gesendet)
	}
}

// Fällt das Ziel mitten im Abarbeiten aus, bleibt der nicht gesendete Rest im Puffer.
func TestAusfallMittenDrinBehaeltDenRest(t *testing.T) {
	s := puffer(t)
	z := &ziel{erreichbar: true}
	w := neuerWorker(s, z)
	jetzt := time.Now()
	for _, topic := range []string{"t/1", "t/2", "t/3"} {
		ablegen(t, s, topic, jetzt)
	}
	if ok, err := w.processNext(); !ok || err != nil {
		t.Fatalf("erste Nachricht: %v %v", ok, err)
	}
	z.erreichbar = false
	w.runBatch()
	if n, _ := s.GetSize(); n != 2 || len(z.gesendet) != 1 {
		t.Errorf("Puffer %d (erwartet 2), gesendet %d (erwartet 1)", n, len(z.gesendet))
	}
}

// Übergang: Ein Eintrag im alten Format (Zeit im Schlüssel, nicht im Eintrag) geht mit der Zeit
// aus dem Schlüssel hinaus.
func TestAlterEintragMitZeitAusDemSchluessel(t *testing.T) {
	s := puffer(t)
	z := &ziel{erreichbar: true}
	w := neuerWorker(s, z)
	alt := time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC)
	v, _ := json.Marshal(broker.PayloadWrapper{Topic: "zigbee2mqtt/alt", Payload: []byte(`{}`)})
	if err := s.Push([]byte(alt.Format(time.RFC3339Nano)), v); err != nil {
		t.Fatal(err)
	}
	w.runBatch()
	if len(z.gesendet) != 1 || !z.gesendet[0].zeit.Equal(alt) {
		t.Errorf("gesendet %v, erwartet Zeit %v", z.gesendet, alt)
	}
}
