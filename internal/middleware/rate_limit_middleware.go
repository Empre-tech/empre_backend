package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimit is a fixed-window, in-memory rate limiter. Keyed by the
// authenticated user id when present, otherwise by the client IP. Good enough
// for the MVP (single instance); a distributed deployment would use Redis.
func RateLimit(max int, window time.Duration) gin.HandlerFunc {
	var (
		mu     sync.Mutex
		counts = make(map[string][]time.Time)
	)

	return func(c *gin.Context) {
		key := c.ClientIP()
		if uid, exists := c.Get("userID"); exists {
			if id, ok := uid.(interface{ String() string }); ok {
				key = id.String()
			}
		}

		now := time.Now()
		cutoff := now.Add(-window)

		mu.Lock()
		// Podar timestamps viejos y conservar solo los dentro de la ventana.
		kept := counts[key][:0]
		for _, t := range counts[key] {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		counts[key] = kept

		if len(kept) >= max {
			mu.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Demasiadas solicitudes. Intenta de nuevo en un momento."})
			c.Abort()
			return
		}
		counts[key] = append(counts[key], now)
		mu.Unlock()

		c.Next()
	}
}
