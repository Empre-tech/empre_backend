package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AskHandler struct {
	Service *services.AskService
}

func NewAskHandler(service *services.AskService) *AskHandler {
	return &AskHandler{Service: service}
}

// Ask: «Pregúntale a Empre». Público.
// GET /api/ask?q=almuerzo barato cerca&lat=..&long=..&open=1&offer=1&delivery=1&cheap=1
func (h *AskHandler) Ask(c *gin.Context) {
	lat, _ := strconv.ParseFloat(c.Query("lat"), 64)
	lng, _ := strconv.ParseFloat(c.Query("long"), 64)
	on := func(k string) bool { return c.Query(k) == "1" || c.Query(k) == "true" }
	// ai=0: búsqueda por palabras de Vitrina (pública, sin IA, sin cupo). Con IA: hace falta cuenta y gasta cupo.
	noAI := c.Query("ai") == "0"
	var quota *services.AskQuota
	if !noAI {
		v, ok := c.Get("userID")
		userID, isID := v.(uuid.UUID)
		if !ok || !isID {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "login_required", "message": "Inicia sesión para preguntarle a Empre."})
			return
		}
		q, err := h.Service.UseQuestion(userID, c.Query("q"), time.Now())
		if errors.Is(err, services.ErrAskQuota) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "ask_quota", "message": askQuotaMessage(q), "quota": q})
			return
		}
		if err != nil {
			internalError(c, err)
			return
		}
		quota = &q
	}
	res, err := h.Service.Ask(services.AskInput{
		Question: c.Query("q"),
		Lat:      lat,
		Lng:      lng,
		Filters:  services.AskFilters{OpenNow: on("open"), WithOffer: on("offer"), Delivery: on("delivery"), Cheapest: on("cheap")},
		NoAI:     noAI,
	})
	if errors.Is(err, services.ErrCatalogInvalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Escribe qué necesitas"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos buscar ahora"})
		return
	}
	res.Quota = quota
	c.JSON(http.StatusOK, res)
}

var monthsES = [...]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}

func askQuotaMessage(q services.AskQuota) string {
	when := fmt.Sprintf("el 1 de %s", monthsES[q.ResetsAt.Month()-1])
	return fmt.Sprintf("Usaste tus %d preguntas de este mes. Se renuevan %s.", q.Limit, when)
}

// Quota: GET /api/ask/quota. Cuántas preguntas lleva el usuario este mes y de cuántas.
func (h *AskHandler) Quota(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	q, err := h.Service.Quota(userID, time.Now())
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, q)
}

type grantPlanRequest struct {
	Days int `json:"days" binding:"required,min=1,max=400"`
}

// GrantPlan: POST /api/admin/users/:id/plan {"days": 30}. Activa la suscripción de usuario (pruebas y cortesías,
// mientras no estén las compras de las tiendas).
func (h *AskHandler) GrantPlan(c *gin.Context) {
	userID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	var req grantPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days debe estar entre 1 y 400"})
		return
	}
	until := time.Now().AddDate(0, 0, req.Days)
	if err := h.Service.GrantPlus(userID, until, "manual"); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"plan": "plus", "current_period_end": until})
}
