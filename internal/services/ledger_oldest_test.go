package services

import (
	"testing"
	"time"
)

func TestOldestUnpaid(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
	entries := []debtEntry{{d(1), 10000}, {d(10), 20000}, {d(20), 5000}}
	if got := oldestUnpaid(entries, 5000); got == nil || !got.Equal(d(20)) {
		t.Fatalf("solo lo último: %v", got)
	}
	if got := oldestUnpaid(entries, 25000); got == nil || !got.Equal(d(10)) {
		t.Fatalf("abonó lo más viejo: %v", got)
	}
	if got := oldestUnpaid(entries, 35000); got == nil || !got.Equal(d(1)) {
		t.Fatalf("no ha abonado: %v", got)
	}
	if oldestUnpaid(entries, 0) != nil || oldestUnpaid(nil, 100) != nil {
		t.Fatal("sin deuda o sin registros no hay fecha")
	}
}
