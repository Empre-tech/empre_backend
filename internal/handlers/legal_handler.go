package handlers

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"os"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

//go:embed legal/*.html
var legalFiles embed.FS

var legalPages = template.Must(template.ParseFS(legalFiles, "legal/*.html"))

// LegalData es lo que cambia en las páginas legales, desde el .env del servidor: LEGAL_OWNER (responsable: razón
// social o nombre), LEGAL_ID (NIT o cédula), LEGAL_ADDRESS (domicilio), LEGAL_PHONE y LEGAL_CONTACT_EMAIL. La Ley 1581
// pide identificar al responsable con esos datos; los vacíos no se muestran.
type LegalData struct {
	Owner   string
	ID      string
	Address string
	Phone   string
	Email   string
	Updated string
	Version string
}

// LegalUpdated es la fecha de la última versión de la política: cámbiala cuando cambie el texto.
const LegalUpdated = "25 de noviembre de 2026"

func legalData() LegalData {
	d := LegalData{
		Owner: os.Getenv("LEGAL_OWNER"), ID: os.Getenv("LEGAL_ID"), Address: os.Getenv("LEGAL_ADDRESS"), Phone: os.Getenv("LEGAL_PHONE"),
		Email: os.Getenv("LEGAL_CONTACT_EMAIL"), Updated: LegalUpdated, Version: services.LegalVersion,
	}
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
