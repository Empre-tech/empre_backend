package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// El navegador solo manda la reserva si el preflight permite Idempotency-Key, y solo los PATCH si permite PATCH.
func TestCORSAllowsIdempotencyKeyAndPatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORSMiddleware())
	r.POST("/api/appointments", func(c *gin.Context) { c.Status(http.StatusCreated) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/appointments", nil)
	req.Header.Set("Access-Control-Request-Headers", "idempotency-key, authorization, content-type")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d", w.Code)
	}
	if h := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(h), "idempotency-key") {
		t.Fatalf("falta Idempotency-Key en %q", h)
	}
	if m := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, "PATCH") {
		t.Fatalf("falta PATCH en %q", m)
	}
}
