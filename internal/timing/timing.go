package timing

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Entry struct {
	Function string
	Target   string
	Duration time.Duration
	Start    time.Time
}

type Collector struct {
	mu      sync.Mutex
	entries []Entry
}

var Timings = &Collector{}

func (tc *Collector) Track(function, target string) func() {
	start := time.Now()
	return func() {
		tc.mu.Lock()
		tc.entries = append(tc.entries, Entry{
			Function: function,
			Target:   target,
			Duration: time.Since(start),
			Start:    start,
		})
		tc.mu.Unlock()
	}
}

func (tc *Collector) WriteReport(filename string) error {
	tc.mu.Lock()
	entries := make([]Entry, len(tc.entries))
	copy(entries, tc.entries)
	tc.mu.Unlock()

	if len(entries) == 0 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Start.Before(entries[j].Start)
	})

	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("could not create directory for timing report: %w", err)
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("could not create timing report: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "%-35s %-30s %12s\n", "FUNCTION", "TARGET", "DURATION")
	fmt.Fprintf(f, "%s\n", "---------------------------------------------------------------------------------")
	for _, e := range entries {
		fmt.Fprintf(f, "%-35s %-30s %12s\n", e.Function, e.Target, e.Duration.Round(time.Millisecond))
	}

	type summary struct {
		count int
		total time.Duration
		min   time.Duration
		max   time.Duration
	}
	agg := make(map[string]*summary)
	for _, e := range entries {
		s, ok := agg[e.Function]
		if !ok {
			s = &summary{min: e.Duration, max: e.Duration}
			agg[e.Function] = s
		}
		s.count++
		s.total += e.Duration
		if e.Duration < s.min {
			s.min = e.Duration
		}
		if e.Duration > s.max {
			s.max = e.Duration
		}
	}

	funcs := make([]string, 0, len(agg))
	for fn := range agg {
		funcs = append(funcs, fn)
	}
	sort.Strings(funcs)

	fmt.Fprintf(f, "\n%-35s %6s %12s %12s %12s %12s\n", "FUNCTION", "CALLS", "TOTAL", "AVG", "MIN", "MAX")
	fmt.Fprintf(f, "%s\n", "------------------------------------------------------------------------------------------------------")
	for _, fn := range funcs {
		s := agg[fn]
		avg := s.total / time.Duration(s.count)
		fmt.Fprintf(f, "%-35s %6d %12s %12s %12s %12s\n",
			fn, s.count,
			s.total.Round(time.Millisecond),
			avg.Round(time.Millisecond),
			s.min.Round(time.Millisecond),
			s.max.Round(time.Millisecond),
		)
	}

	return nil
}
