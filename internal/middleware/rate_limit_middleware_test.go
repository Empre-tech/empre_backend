package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestRateLimitBlocksAfterMax: pasado el máximo en la ventana, la misma IP recibe 429; otra IP sigue pasando.
func TestRateLimitBlocksAfterMax(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/login", RateLimit(3, time.Minute), func(c *gin.Context) { c.Status(http.StatusOK) })

	do := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	for i := 1; i <= 3; i++ {
		if code := do("10.0.0.1"); code != http.StatusOK {
			t.Fatalf("intento %d: %d", i, code)
		}
	}
	if code := do("10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("el cuarto intento debería dar 429, dio %d", code)
	}
	if code := do("10.0.0.2"); code != http.StatusOK {
		t.Fatalf("otra IP no debería quedar bloqueada: %d", code)
	}
}
