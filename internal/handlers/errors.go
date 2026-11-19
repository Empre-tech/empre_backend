package handlers

import (
	"net/http"

	"empre_backend/internal/observability"

	"github.com/gin-gonic/gin"
)

// internalError responde un 500 sin contarle al cliente qué falló por dentro (errores de la base, de S3, de la IA
// dan pistas a un atacante). El error completo queda en el log de la petición (observability lo toma de c.Errors)
// con su request_id, que también va en la respuesta para poder buscarlo.
func internalError(c *gin.Context, err error) {
	internalErrorMsg(c, err, "Algo salió mal de nuestro lado. Intenta de nuevo en un momento.")
}

// internalErrorMsg es internalError con un mensaje propio para el usuario.
func internalErrorMsg(c *gin.Context, err error, msg string) {
	if err != nil {
		_ = c.Error(err)
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": msg, "request_id": observability.RequestID(c)})
}
