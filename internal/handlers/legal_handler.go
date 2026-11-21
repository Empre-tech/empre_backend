package handlers

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

//go:embed legal/*.html
var legalFiles embed.FS

var legalPages = template.Must(template.ParseFS(legalFiles, "legal/*.html"))

// LegalData es lo que cambia en las páginas legales. El correo y el responsable salen de LEGAL_CONTACT_EMAIL y
// LEGAL_OWNER (en el .env del servidor).
type LegalData struct {
	Owner   string
	Email   string
	Updated string
}

// LegalUpdated es la fecha de la última versión de la política: cámbiala cuando cambie el texto.
const LegalUpdated = "20 de noviembre de 2026"

func legalData() LegalData {
	d := LegalData{Owner: os.Getenv("LEGAL_OWNER"), Email: os.Getenv("LEGAL_CONTACT_EMAIL"), Updated: LegalUpdated}
	if d.Owner == "" {
		d.Owner = "Empre"
	}
	if d.Email == "" {
		d.Email = "soporte@empre.com.co"
	}
	return d
}

// LegalPage sirve una página legal pública (las URL que piden Google Play y la App Store): /privacidad y
// /eliminar-cuenta.
func LegalPage(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var buf bytes.Buffer
		if err := legalPages.ExecuteTemplate(&buf, name, legalData()); err != nil {
			internalError(c, err)
			return
		}
		c.Header("Cache-Control", "public, max-age=3600")
		c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
	}
}
