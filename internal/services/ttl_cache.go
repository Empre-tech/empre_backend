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
}

type ttlEntry[V any] struct {
	value V
	until time.Time
}

func NewTTLCache[K comparable, V any](ttl time.Duration) *TTLCache[K, V] {
	return &TTLCache[K, V]{ttl: ttl, now: time.Now, data: map[K]ttlEntry[V]{}}
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
	c.data[key] = ttlEntry[V]{value: value, until: c.now().Add(c.ttl)}
	c.mu.Unlock()
}

// Clear olvida todo (al crear, editar o borrar lo que se guarda).
func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	c.data = map[K]ttlEntry[V]{}
	c.mu.Unlock()
}
