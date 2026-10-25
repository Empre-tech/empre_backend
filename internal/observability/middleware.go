package observability

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// HeaderRequestID es la cabecera que identifica una petición de punta a punta (app → logs del backend).
const HeaderRequestID = "X-Request-ID"

// SlowRequest: más que esto y la petición se registra como advertencia.
const SlowRequest = time.Second

var validID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// Logger escribe una línea JSON por evento en la salida estándar.
var Logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

// RequestID devuelve el id de la petición en curso (o "" fuera de una petición).
func RequestID(c *gin.Context) string {
	if v, ok := c.Get("request_id"); ok {
		return v.(string)
	}
	return ""
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// userHash identifica a una persona en los logs sin guardar su id.
func userHash(c *gin.Context) string {
	v, ok := c.Get("userID")
	if !ok {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprint(v)))
	return hex.EncodeToString(sum[:4])
}

// Middleware mide y registra cada petición: un id (el que mande la app o uno nuevo, devuelto en X-Request-ID), la ruta
// como plantilla (/api/entities/:id, nunca el id real), el código, la duración y el tamaño. Las de 5xx van como error
// y las lentas como advertencia. /health y /metrics se miden pero no se registran (son muchas y no dicen nada).
func Middleware(reg *Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		id := c.GetHeader(HeaderRequestID)
		if !validID.MatchString(id) {
			id = newID()
		}
		c.Set("request_id", id)
		c.Header(HeaderRequestID, id)

		c.Next()

		d := time.Since(start)
		route := c.FullPath()
		if route == "" {
			route = "sin_ruta" // 404: no se usa la URL real para no crear una serie por cada dirección inventada
		}
		status := c.Writer.Status()
		reg.Observe(c.Request.Method, route, status, d)
		if route == "/health" || route == "/ready" || route == "/metrics" {
			return
		}
		attrs := []any{
			"request_id", id, "method", c.Request.Method, "route", route, "status", status,
			"ms", d.Milliseconds(), "bytes", c.Writer.Size(),
		}
		if u := userHash(c); u != "" {
			attrs = append(attrs, "user", u)
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "error", c.Errors.String())
		}
		switch {
		case status >= 500:
			Logger.Error("petición", attrs...)
		case d >= SlowRequest:
			Logger.Warn("petición lenta", attrs...)
		default:
			Logger.Info("petición", attrs...)
		}
	}
}

// Recovery convierte un pánico en un 500 con su request_id y la pila en el log JSON (sin el volcado de texto de gin).
// Va después de Middleware: así el 500 también queda medido.
func Recovery(reg *Registry) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, err any) {
		reg.Inc("panic")
		Logger.Error("pánico", "request_id", RequestID(c), "route", c.FullPath(), "error", fmt.Sprint(err), "stack", string(debug.Stack()))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "Algo falló de nuestro lado. Inténtalo de nuevo.", "request_id": RequestID(c)})
	})
}
