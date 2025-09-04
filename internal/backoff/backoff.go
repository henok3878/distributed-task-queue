package backoff

import (
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

// computes the next delay for a given attempt (1 based)
type Strategy interface {
	NextDelay(attempt int) time.Duration
}

type fixed struct{ d time.Duration }

func (f fixed) NextDelay(int) time.Duration { return f.d }

type list struct{ ds []time.Duration }

func (l list) NextDelay(attempt int) time.Duration {
	// clamp attempt to at least 1 so the first failure uses slot 0
	if attempt < 1 {
		attempt = 1
	}
	i := attempt - 1
	if i >= len(l.ds) {
		// if attempts exceed the list, stick to the last delay.
		return l.ds[len(l.ds)-1]
	}
	return l.ds[i]
}

// Exponential (+ optional jitter)

type exp struct {
	base   time.Duration
	factor float64
	max    time.Duration
	jitter float64 // 0..1 => +- % jitter
}

func (e exp) NextDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := float64(e.base) * math.Pow(e.factor, float64(attempt-1))
	if e.max > 0 && time.Duration(delay) > e.max {
		delay = float64(e.max)
	}
	d := time.Duration(delay)
	if e.jitter > 0 {
		amp := e.jitter * float64(d)
		delta := (rand.Float64()*2 - 1) * amp // [-amp, +amp]
		d = time.Duration(float64(d) + delta)
		if d < 0 {
			d = 0
		}
	}
	return d
}

// FromEnv builds a Strategy from env configuration.
// BACKOFF_STRATEGY: list (default) | fixed | exponential
//
// list:        BACKOFFS="5s,30s,2m,10m,1h"
// fixed:       BACKOFF_FIXED="30s"
// exponential: BACKOFF_BASE="5s", BACKOFF_FACTOR="6", BACKOFF_MAX="1h", BACKOFF_JITTER="0.2"
func FromEnv() Strategy {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("BACKOFF_STRATEGY")))
	switch mode {
	case "fixed":
		if d := parseDur(os.Getenv("BACKOFF_FIXED")); d > 0 {
			return fixed{d: d}
		}
		return fixed{d: 30 * time.Second}
	case "exponential":
		base := parseDur(os.Getenv("BACKOFF_BASE"))
		if base <= 0 {
			base = 5 * time.Second
		}
		factor := parseFloat(os.Getenv("BACKOFF_FACTOR"))
		if factor <= 1 {
			factor = 6
		}
		max := parseDur(os.Getenv("BACKOFF_MAX"))
		if max <= 0 {
			max = time.Hour
		}
		j := parseFloat(os.Getenv("BACKOFF_JITTER"))
		if j < 0 || j > 1 {
			j = 0
		}
		return exp{base: base, factor: factor, max: max, jitter: j}
	default: // list
		if raw := strings.TrimSpace(os.Getenv("BACKOFFS")); raw != "" {
			parts := strings.Split(raw, ",")
			var ds []time.Duration
			for _, p := range parts {
				if d := parseDur(p); d > 0 {
					ds = append(ds, d)
				}
			}
			if len(ds) > 0 {
				return list{ds: ds}
			}
		}
		// sensible default ladder
		return list{ds: []time.Duration{
			5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour,
		}}
	}
}

func parseDur(s string) time.Duration {
	d, _ := time.ParseDuration(strings.TrimSpace(s))
	return d
}
func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
