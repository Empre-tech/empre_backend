package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLegalPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LEGAL_CONTACT_EMAIL", "privacidad@empre.test")
	r := gin.New()
	r.GET("/", LegalPage("inicio.html"))
	r.GET("/privacidad", LegalPage("privacidad.html"))
	r.GET("/terminos", LegalPage("terminos.html"))
	r.GET("/eliminar-cuenta", LegalPage("eliminar-cuenta.html"))
	for path, want := range map[string]string{"/": "todo tu barrio", "/privacidad": "Política de tratamiento de datos personales", "/terminos": "Términos y condiciones", "/eliminar-cuenta": "Eliminar mi cuenta"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, want) || !strings.Contains(body, "privacidad@empre.test") {
			t.Fatalf("%s: %d, falta %q o el correo", path, w.Code, want)
		}
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: content-type %q", path, w.Header().Get("Content-Type"))
		}
	}
}
