package history

import (
	"errors"
	"sort"
	"time"
)

// Nachrichten je Gerät: je Stunde (DefaultSlotDays) und je Tag (DefaultRetentionDays).
// Ablage: devhours/<Tag>.json, devdays/<Monat>.json.
const (
	devHoursDir = "devhours"
	devDaysDir  = "devdays"
)

// DeviceHour sind die Nachrichten je Gerät in einer Stunde.
type DeviceHour struct {
	Start  time.Time        `json:"start"`
	Counts map[string]int64 `json:"counts"`
}

// DeviceDay sind die Nachrichten je Gerät an einem Tag.
type DeviceDay struct {
	Date   string           `json:"date"`
	Counts map[string]int64 `json:"counts"`
}

// Point ist ein Wert zu einem Zeitpunkt (Beginn der Stunde bzw. des Tages).
type Point struct {
	Start time.Time `json:"start"`
	Count int64     `json:"count"`
}

// DeviceTotals sind die Nachrichten eines Geräts heute, in 7 und in 30 Tagen.
type DeviceTotals struct {
	Today int64 `json:"today"`
	Week  int64 `json:"week"`
	Month int64 `json:"month"`
}

type deviceStore struct {
	devHours      map[int64]*DeviceHour
	devDays       map[string]*DeviceDay
	dirtyDevHours map[string]bool
	dirtyDevDays  map[string]bool
}

func newDeviceStore() deviceStore {
	return deviceStore{devHours: map[int64]*DeviceHour{}, devDays: map[string]*DeviceDay{},
		dirtyDevHours: map[string]bool{}, dirtyDevDays: map[string]bool{}}
}

// addDevices verbucht die Nachrichten je Gerät seit der letzten Messung.
func (s *Store) addDevices(counts map[string]int64, at time.Time) {
	if len(counts) == 0 {
		return
	}
	hour := at.Truncate(time.Hour)
	h := s.devHours[hour.Unix()]
	if h == nil {
		h = &DeviceHour{Start: hour, Counts: map[string]int64{}}
		s.devHours[hour.Unix()] = h
	}
	date := at.Format(dateLayout)
	d := s.devDays[date]
	if d == nil {
		d = &DeviceDay{Date: date, Counts: map[string]int64{}}
		s.devDays[date] = d
	}
	for name, n := range counts {
		h.Counts[name] += n
		d.Counts[name] += n
	}
	s.dirtyDevHours[hour.In(at.Location()).Format(dateLayout)] = true
	s.dirtyDevDays[date[:len(monthLayout)]] = true
}

// DeviceTotals liefert je Gerät die Nachrichten heute, in 7 und in 30 Tagen bis now.
func (s *Store) DeviceTotals(now time.Time) map[string]DeviceTotals {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := now.Format(dateLayout)
	week := now.AddDate(0, 0, -6).Format(dateLayout)
	month := now.AddDate(0, 0, -29).Format(dateLayout)
	out := map[string]DeviceTotals{}
	for date, d := range s.devDays {
		if date < month {
			continue
		}
		for name, n := range d.Counts {
			t := out[name]
			t.Month += n
			if date >= week {
				t.Week += n
			}
			if date == today {
				t.Today += n
			}
			out[name] = t
		}
	}
	return out
}

// DeviceDays liefert die Tageswerte eines Geräts der letzten n Tage, älteste zuerst; Tage mit
// Messung, aber ohne Nachricht des Geräts, stehen mit 0 darin.
func (s *Store) DeviceDays(name string, n int, now time.Time) []Point {
	s.mu.Lock()
	defer s.mu.Unlock()
	from := now.AddDate(0, 0, -(n - 1)).Format(dateLayout)
	var out []Point
	for date, d := range s.devDays {
		if date < from {
			continue
		}
		start, _ := time.ParseInLocation(dateLayout, date, now.Location())
		out = append(out, Point{Start: start, Count: d.Counts[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// DeviceHours liefert die Stundenwerte eines Geräts in [from, to), älteste zuerst.
func (s *Store) DeviceHours(name string, from, to time.Time) []Point {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Point
	for _, h := range s.devHours {
		if !h.Start.Before(from) && h.Start.Before(to) {
			out = append(out, Point{Start: h.Start, Count: h.Counts[name]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func (s *Store) loadDevices() error {
	return errors.Join(
		readFiles(s.dir, devHoursDir, func(items []DeviceHour) {
			for i := range items {
				h := items[i]
				s.devHours[h.Start.Unix()] = &h
			}
		}),
		readFiles(s.dir, devDaysDir, func(items []DeviceDay) {
			for i := range items {
				d := items[i]
				s.devDays[d.Date] = &d
			}
		}))
}

// flushDevices räumt Abgelaufenes weg und schreibt geänderte Dateien.
func (s *Store) flushDevices(now time.Time) error {
	hourLimit := now.AddDate(0, 0, -DefaultSlotDays)
	for k, h := range s.devHours {
		if h.Start.Before(hourLimit) {
			delete(s.devHours, k)
			s.dirtyDevHours[h.Start.In(now.Location()).Format(dateLayout)] = true
		}
	}
	dayLimit := now.AddDate(0, 0, -DefaultRetentionDays).Format(dateLayout)
	for date := range s.devDays {
		if date < dayLimit {
			delete(s.devDays, date)
			s.dirtyDevDays[date[:len(monthLayout)]] = true
		}
	}
	var errs []error
	for date := range s.dirtyDevHours {
		var list []DeviceHour
		for _, h := range s.devHours {
			if h.Start.In(now.Location()).Format(dateLayout) == date {
				list = append(list, *h)
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Start.Before(list[j].Start) })
		errs = append(errs, writeFile(s.dir, devHoursDir, date, list, len(list) == 0))
	}
	for month := range s.dirtyDevDays {
		var list []DeviceDay
		for date, d := range s.devDays {
			if date[:len(monthLayout)] == month {
				list = append(list, *d)
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Date < list[j].Date })
		errs = append(errs, writeFile(s.dir, devDaysDir, month, list, len(list) == 0))
	}
	clear(s.dirtyDevHours)
	clear(s.dirtyDevDays)
	return errors.Join(errs...)
}
