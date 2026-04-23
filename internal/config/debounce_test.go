package config

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncedSaver_CoalescesBurst(t *testing.T) {
	var calls int32
	saver := NewDebouncedSaver(100*time.Millisecond, func(v any) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	defer saver.Stop()

	for i := 0; i < 10; i++ {
		saver.Schedule("payload")
		time.Sleep(5 * time.Millisecond)
	}
	// Wait past the debounce window + slack.
	time.Sleep(250 * time.Millisecond)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 coalesced save, got %d", got)
	}
}

func TestDebouncedSaver_SavesLatestValue(t *testing.T) {
	var lastSeen atomic.Value
	saver := NewDebouncedSaver(60*time.Millisecond, func(v any) error {
		lastSeen.Store(v)
		return nil
	})
	defer saver.Stop()

	saver.Schedule("a")
	saver.Schedule("b")
	saver.Schedule("c")

	time.Sleep(200 * time.Millisecond)

	if got := lastSeen.Load(); got != "c" {
		t.Fatalf("expected latest value 'c', got %v", got)
	}
}
