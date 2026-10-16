package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Solo los caminos que se rechazan antes de tocar la base de datos (con servicio nil): cuerpo inválido
// y tamaño del lote.
func TestEventIngest_RejectsBadRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewEventHandler(nil)
	r := gin.New()
	r.POST("/events", h.Ingest)

	manyEvents := `{"name":"search_submitted"}`
	over := strings.Repeat(manyEvents+",", 20) + manyEvents // 21 eventos

	cases := []struct{ name, body string }{
		{"no es JSON", `no es json`},
		{"sin eventos", `{"anon_id":"inst-1234567890abcdef","events":[]}`},
		{"sin lista", `{"anon_id":"inst-1234567890abcdef"}`},
		{"más de 20 eventos", `{"anon_id":"inst-1234567890abcdef","events":[` + over + `]}`},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: código %d; quería 400 (%s)", c.name, w.Code, w.Body.String())
		}
	}
}
