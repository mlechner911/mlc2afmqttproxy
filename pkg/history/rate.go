package history

import "time"

// Rate ist die kleinste und größte Nachrichtenrate (Nachrichten je Sekunde), gemessen über je
// eine Messung (SampleInterval, 15 s). Samples = Zahl der Messungen; 0 = keine Messung (ältere
// Werte von vor dem Einbau, 28.11.2026). Der Durchschnitt ergibt sich aus Zähler / Laufzeit und
// steht deshalb nicht hier.
type Rate struct {
	Min     float64 `json:"min" example:"0"`
	Max     float64 `json:"max" example:"4.2"`
	Samples int64   `json:"samples" example:"5760"`
}

// Rates: empfangen (Last durch zigbee2mqtt) und weitergeleitet (Last am Upstream).
type Rates struct {
	Received  Rate `json:"received"`
	Forwarded Rate `json:"forwarded"`
}

func (r *Rate) observe(v float64) {
	if r.Samples == 0 || v < r.Min {
		r.Min = v
	}
	if r.Samples == 0 || v > r.Max {
		r.Max = v
	}
	r.Samples++
}

func (r *Rate) merge(o Rate) {
	if o.Samples == 0 {
		return
	}
	if r.Samples == 0 || o.Min < r.Min {
		r.Min = o.Min
	}
	if r.Samples == 0 || o.Max > r.Max {
		r.Max = o.Max
	}
	r.Samples += o.Samples
}

func (r *Rates) merge(o Rates) {
	r.Received.merge(o.Received)
	r.Forwarded.merge(o.Forwarded)
}

// rateWindow: nur Abstände nahe SampleInterval zählen. Die erste Messung nach dem Start trägt
// den ganzen Stand, eine Lücke (Proxy hing, Rechner schlief) würde eine Spitze glätten.
func rateWindow(dt time.Duration) bool {
	return dt >= SampleInterval/2 && dt <= 2*SampleInterval
}

// sampleRates: die Raten dieser Messung; ok ist false, wenn das Fenster nicht passt.
func sampleRates(delta Counters, dt time.Duration) (recv, fwd float64, ok bool) {
	if !rateWindow(dt) {
		return 0, 0, false
	}
	s := dt.Seconds()
	return float64(delta.Received) / s, float64(delta.Forwarded) / s, true
}
