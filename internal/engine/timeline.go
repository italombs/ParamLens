package engine

import (
	"math"
	"strings"
)

// noTS marks an execution without a (readable) timestamp.
const noTS = math.MinInt64

// ParseTimestamp reads an event timestamp as SQL Server and Extended Events write it:
// "2026-11-20T13:10:00.123Z" (.xel), "2026-11-20 10:10:00.1234567 -03:00" (datetimeoffset, as in the
// "timestamp" column of an SSMS export), "2026-11-20 13:10:00.1234567" (datetime2, "timestamp (UTC)").
// It returns the instant in Unix milliseconds: an explicit offset is applied, a timestamp without one
// is taken as UTC. Digits after the milliseconds are dropped. JS: parseTimestamp.
func ParseTimestamp(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	p := 0
	num := func(n int) (int64, bool) {
		if p+n > len(s) {
			return 0, false
		}
		var v int64
		for k := 0; k < n; k++ {
			c := s[p+k]
			if c < '0' || c > '9' {
				return 0, false
			}
			v = v*10 + int64(c-'0')
		}
		p += n
		return v, true
	}
	lit := func(set string) bool {
		if p < len(s) && strings.IndexByte(set, s[p]) >= 0 {
			p++
			return true
		}
		return false
	}
	y, ok1 := num(4)
	if !ok1 || !lit("-") {
		return 0, false
	}
	mo, ok2 := num(2)
	if !ok2 || !lit("-") {
		return 0, false
	}
	d, ok3 := num(2)
	if !ok3 || !lit("T ") {
		return 0, false
	}
	h, ok4 := num(2)
	if !ok4 || !lit(":") {
		return 0, false
	}
	mi, ok5 := num(2)
	if !ok5 {
		return 0, false
	}
	var sec, ms int64
	if lit(":") {
		var ok bool
		if sec, ok = num(2); !ok {
			return 0, false
		}
		if lit(".,") {
			k := 0
			for p < len(s) && s[p] >= '0' && s[p] <= '9' {
				if k < 3 {
					ms = ms*10 + int64(s[p]-'0')
				}
				k++
				p++
			}
			if k == 0 {
				return 0, false
			}
			for ; k < 3; k++ {
				ms *= 10
			}
		}
	}
	for p < len(s) && s[p] == ' ' {
		p++
	}
	var off int64 // minutes east of UTC
	if lit("Zz") {
	} else if p < len(s) && (s[p] == '+' || s[p] == '-') {
		sign := int64(1)
		if s[p] == '-' {
			sign = -1
		}
		p++
		oh, ok := num(2)
		if !ok {
			return 0, false
		}
		lit(":")
		om, ok := num(2)
		if !ok || oh > 14 || om > 59 {
			return 0, false
		}
		off = sign * (oh*60 + om)
	}
	if p != len(s) || y < 1 || mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || sec > 59 {
		return 0, false
	}
	// days from 1970-01-01 (H. Hinnant's days_from_civil; every term is non-negative for y >= 1)
	if mo <= 2 {
		y--
	}
	era := y / 400
	yoe := y - era*400
	mp := (mo + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	days := era*146097 + doe - 719468
	return (((days*24+h)*60+mi-off)*60+sec)*1000 + ms, true
}

// Timeline lists the executions of a query in capture order, with their timestamp and metrics;
// the page groups them by minute, hour, day… JS: timeline.
type Timeline struct {
	Family int            `json:"family"`
	Has    [NMetrics]bool `json:"has"`
	WithTS int            `json:"withTs"` // executions with a timestamp
	Rows   []TimelineRow  `json:"rows"`
}

type TimelineRow struct {
	I  int32   `json:"i"`  // execution index (for the "sample" of that execution)
	TS *int64  `json:"ts"` // Unix ms (UTC), null when unknown
	M  Metrics `json:"m"`
}

func (e *Engine) Timeline(id int) (*Timeline, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if id < 0 || id >= len(e.fams) {
		return nil, errNoFamily
	}
	f := e.fams[id]
	tl := &Timeline{Family: f.ID, Has: e.has, Rows: make([]TimelineRow, 0, len(f.Execs))}
	for _, ix := range f.Execs {
		ex := &e.execs[ix]
		r := TimelineRow{I: ix, M: ex.M}
		if ex.TS != noTS {
			ts := ex.TS
			r.TS = &ts
			tl.WithTS++
		}
		tl.Rows = append(tl.Rows, r)
	}
	return tl, nil
}
