package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"empre_backend/internal/services"
)

// TestRevenueCatWebhookAuth: sin secreto configurado responde 503; con otro secreto, 401; con el correcto, 200 (el
// aviso de prueba de RevenueCat no toca la base).
func TestRevenueCatWebhookAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	call := func(secret, auth string) int {
		h := NewProPlanHandler(services.NewProPlanService(nil, secret))
		r := gin.New()
		r.POST("/wh", h.RevenueCatWebhook)
		req := httptest.NewRequest(http.MethodPost, "/wh", strings.NewReader(`{"event":{"id":"t1","type":"TEST"}}`))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if c := call("", "x"); c != http.StatusServiceUnavailable {
		t.Fatalf("sin secreto: %d", c)
	}
	if c := call("s3cr3t", "otro"); c != http.StatusUnauthorized {
		t.Fatalf("secreto equivocado: %d", c)
	}
	if c := call("s3cr3t", ""); c != http.StatusUnauthorized {
		t.Fatalf("sin cabecera: %d", c)
	}
	if c := call("s3cr3t", "Bearer s3cr3t"); c != http.StatusOK {
		t.Fatalf("secreto correcto: %d", c)
	}
}
