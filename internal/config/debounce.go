package config

import (
	"sync"
	"time"
)

// SaveFunc persists a configuration value. It should be idempotent and safe
// to call from a background goroutine.
type SaveFunc func(v any) error

// DebouncedSaver coalesces rapid Schedule() calls into at most one SaveFunc
// invocation per interval, always flushing the most-recent value.
type DebouncedSaver struct {
	interval time.Duration
	save     SaveFunc

	mu      sync.Mutex
	pending any
	timer   *time.Timer
	stopped bool
}

// NewDebouncedSaver builds a saver that fires at most once per interval.
func NewDebouncedSaver(interval time.Duration, save SaveFunc) *DebouncedSaver {
	return &DebouncedSaver{
		interval: interval,
		save:     save,
	}
}

// Schedule records v as the latest value and arms the debounce timer.
// Subsequent calls within the interval overwrite v without scheduling a new save.
func (d *DebouncedSaver) Schedule(v any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.pending = v
	if d.timer == nil {
		d.timer = time.AfterFunc(d.interval, d.flush)
	}
}

func (d *DebouncedSaver) flush() {
	d.mu.Lock()
	v := d.pending
	d.pending = nil
	d.timer = nil
	stopped := d.stopped
	d.mu.Unlock()
	if stopped || v == nil {
		return
	}
	_ = d.save(v) // intentionally swallow error; caller can log via SaveFunc
}

// Stop cancels any pending save and prevents future Schedules from firing.
func (d *DebouncedSaver) Stop() {
	d.mu.Lock()
	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.mu.Unlock()
}

// Flush blocks until any pending save has fired. Safe to call from main() exit.
func (d *DebouncedSaver) Flush() {
	d.mu.Lock()
	t := d.timer
	d.timer = nil
	v := d.pending
	d.pending = nil
	d.mu.Unlock()
	if t != nil {
		t.Stop()
	}
	if v != nil {
		_ = d.save(v)
	}
}
