package handlers

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"empre_backend/internal/observability"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

// HealthHandler: salud y métricas del backend (3.53).
//   - /health: el proceso responde (para el balanceador; no toca la base).
//   - /ready: la base responde en menos de 2 s; si no, 503 (para no mandar tráfico a un servidor sin base).
//   - /metrics: métricas en formato Prometheus. Con METRICS_TOKEN pide «Bearer <token>»; sin él, solo desde la misma
//     máquina.
//   - /api/admin/health: el resumen para una persona (rutas más lentas, errores, pool, lo que reporta la app).
type HealthHandler struct {
	db     *sql.DB
	reg    *observability.Registry
	events *services.EventService
	token  string
}

func NewHealthHandler(db *sql.DB, reg *observability.Registry, events *services.EventService) *HealthHandler {
	return &HealthHandler{db: db, reg: reg, events: events, token: os.Getenv("METRICS_TOKEN")}
}

func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	start := time.Now()
	err := h.db.PingContext(ctx)
	body := gin.H{"db_ms": time.Since(start).Milliseconds(), "pool": h.reg.Pool()}
	if err != nil {
		body["status"] = "sin base"
		body["error"] = err.Error()
		c.JSON(http.StatusServiceUnavailable, body)
		return
	}
	body["status"] = "ok"
	c.JSON(http.StatusOK, body)
}

// allowed: con token, el token exacto; sin token, solo desde la misma máquina (desarrollo).
func (h *HealthHandler) allowed(c *gin.Context) bool {
	if h.token != "" {
		got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		return subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) == 1
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *HealthHandler) Metrics(c *gin.Context) {
	if !h.allowed(c) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	h.reg.WritePrometheus(c.Writer)
}

// AdminHealth: lo que una persona necesita para saber cómo va el sistema, desde que arrancó el proceso (backend) y
// en las últimas 24 h (lo que reporta la app).
func (h *HealthHandler) AdminHealth(c *gin.Context) {
	routes := h.reg.Snapshot()
	if len(routes) > 15 {
		routes = routes[:15]
	}
	app, err := h.events.Health(time.Now().Add(-24 * time.Hour))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo leer la salud de la app"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"backend": gin.H{"slowest_routes": routes, "pool": h.reg.Pool()},
		"app_24h": app,
	})
}

// Pprof expone /debug/pprof (perfiles de CPU, memoria y goroutines) con las mismas reglas que /metrics. Solo se
// monta con ENABLE_PPROF=1.
func (h *HealthHandler) Pprof(c *gin.Context) {
	if !h.allowed(c) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	http.DefaultServeMux.ServeHTTP(c.Writer, c.Request)
}
