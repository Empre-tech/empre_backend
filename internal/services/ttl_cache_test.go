package services

import (
	"sync"
	"testing"
	"time"
)

func TestTTLCacheExpiresAndClears(t *testing.T) {
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	c := NewTTLCache[string, int](5 * time.Minute)
	c.now = func() time.Time { return now }

	if _, ok := c.Get("a"); ok {
		t.Fatal("empty cache returned a value")
	}
	c.Set("a", 1)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("want 1, got %v %v", v, ok)
	}
	now = now.Add(4 * time.Minute)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expired too early")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("should have expired after the TTL")
	}
	c.Set("b", 2)
	c.Clear()
	if _, ok := c.Get("b"); ok {
		t.Fatal("Clear should forget everything")
	}
}

func TestTTLCacheConcurrent(t *testing.T) {
	c := NewTTLCache[int, int](time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Set(i%5, i)
			c.Get(i % 5)
			if i%10 == 0 {
				c.Clear()
			}
		}(i)
	}
	wg.Wait()
}
