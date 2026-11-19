package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestInternalErrorHidesDetails: un 500 no le cuenta al cliente el error interno, pero lo deja en c.Errors (el log).
func TestInternalErrorHidesDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logged string
	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		internalError(c, errors.New(`pq: relation "users" does not exist`))
		logged = c.Errors.String()
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if strings.Contains(w.Body.String(), "relation") || body["error"] == nil {
		t.Fatalf("el cuerpo filtra el error interno: %s", w.Body.String())
	}
	if !strings.Contains(logged, "relation") {
		t.Fatalf("el error debería quedar para el log: %q", logged)
	}
}
