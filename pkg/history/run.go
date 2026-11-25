package history

import (
	"context"
	"log"
	"time"
)

// SampleInterval ist der Abstand der Messungen; FlushInterval der der Schreibvorgänge.
const (
	SampleInterval = 15 * time.Second
	FlushInterval  = time.Minute
)

// Run misst alle SampleInterval mit sample und schreibt jede FlushInterval, bis ctx endet;
// dann misst und schreibt es ein letztes Mal. Fehler beim Schreiben werden geloggt — die
// Weiterleitung hängt nicht an der Statistik.
func (s *Store) Run(ctx context.Context, sample func() Sample) {
	s.Record(sample(), time.Now())
	tick := time.NewTicker(SampleInterval)
	defer tick.Stop()
	flush := time.NewTicker(FlushInterval)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Record(sample(), time.Now())
			s.logFlush(time.Now())
			return
		case now := <-tick.C:
			s.Record(sample(), now)
		case now := <-flush.C:
			s.logFlush(now)
		}
	}
}

func (s *Store) logFlush(now time.Time) {
	if err := s.Flush(now); err != nil {
		log.Printf("[history] Schreiben fehlgeschlagen: %v", err)
	}
}
