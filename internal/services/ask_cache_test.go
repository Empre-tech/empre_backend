package services

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestAskIntentCache: la misma pregunta (con otras mayúsculas o signos) no vuelve a llamar a la IA.
func TestAskIntentCache(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"tool_use","name":"ask_intent","input":{"terms":"almuerzo","open_now":true}}]}`))
	}))
	defer srv.Close()

	s := NewAskService(nil, nil, NewAIService("clave-de-prueba", srv.URL, "modelo"))
	for _, q := range []string{"¿Dónde almuerzo abierto?", "dónde almuerzo abierto", "  DÓNDE   almuerzo abierto!! "} {
		terms, f, usedAI := s.intent(q)
		if terms != "almuerzo" || !f.OpenNow || !usedAI {
			t.Fatalf("%q: terms=%q filtros=%+v ia=%v", q, terms, f, usedAI)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("la IA se llamó %d veces; con la caché debería ser 1", got)
	}
	if _, _, _ = s.intent("otra pregunta distinta"); atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("una pregunta nueva sí llama a la IA")
	}
}

// TestTTLCacheMax: con tope, la caché no crece sin límite.
func TestTTLCacheMax(t *testing.T) {
	c := NewTTLCacheMax[int, int](askIntentTTL, 3)
	for i := 0; i < 10; i++ {
		c.Set(i, i)
	}
	if c.Len() > 3 {
		t.Fatalf("la caché tiene %d entradas con tope 3", c.Len())
	}
	if v, ok := c.Get(9); !ok || v != 9 {
		t.Fatal("la última entrada debe quedar")
	}
}
