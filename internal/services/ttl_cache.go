package services

import (
	"sync"
	"time"
)

// TTLCache guarda resultados que casi no cambian (3.53) durante un tiempo fijo. Con la base remota, cada consulta
// cuesta ~100 ms de ida y vuelta: las categorías, por ejemplo, se pedían en cada pantalla con 3 consultas. Es segura
// para usar desde varias peticiones a la vez. `Clear` la vacía cuando el dato cambia.
type TTLCache[K comparable, V any] struct {
	ttl  time.Duration
	now  func() time.Time
	mu   sync.RWMutex
	data map[K]ttlEntry[V]
	// max: cuántas entradas guarda como mucho (0 = sin tope). Para claves que vienen del usuario (preguntas), así
	// nadie llena la memoria mandando textos distintos.
	max int
}

type ttlEntry[V any] struct {
	value V
	until time.Time
}

func NewTTLCache[K comparable, V any](ttl time.Duration) *TTLCache[K, V] {
	return &TTLCache[K, V]{ttl: ttl, now: time.Now, data: map[K]ttlEntry[V]{}}
}

// NewTTLCacheMax es NewTTLCache con un tope de entradas: al llenarse borra las vencidas y, si sigue lleno, empieza
// de cero (es una caché: perderla solo cuesta volver a calcular).
func NewTTLCacheMax[K comparable, V any](ttl time.Duration, max int) *TTLCache[K, V] {
	c := NewTTLCache[K, V](ttl)
	c.max = max
	return c
}

// Get devuelve el valor guardado si no venció.
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	e, ok := c.data[key]
	c.mu.RUnlock()
	if !ok || c.now().After(e.until) {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	now := c.now()
	if c.max > 0 && len(c.data) >= c.max {
		for k, e := range c.data {
			if now.After(e.until) {
				delete(c.data, k)
			}
		}
		if len(c.data) >= c.max {
			c.data = map[K]ttlEntry[V]{}
		}
	}
	c.data[key] = ttlEntry[V]{value: value, until: now.Add(c.ttl)}
	c.mu.Unlock()
}

// Len cuántas entradas hay guardadas (vencidas incluidas hasta la próxima limpieza).
func (c *TTLCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Clear olvida todo (al crear, editar o borrar lo que se guarda).
func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	c.data = map[K]ttlEntry[V]{}
	c.mu.Unlock()
}
