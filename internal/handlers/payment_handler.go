package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PaymentHandler struct {
	Service       *services.SubscriptionService
	EntityService *services.EntityService
}

func NewPaymentHandler(service *services.SubscriptionService, entityService *services.EntityService) *PaymentHandler {
	return &PaymentHandler{Service: service, EntityService: entityService}
}

// requireOwner loads the entity and checks the caller owns it. Returns nil
// (and has already written the response) if the request should stop here.
func (h *PaymentHandler) requireOwner(c *gin.Context) *uuid.UUID {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return nil
	}
	userIDVal, _ := c.Get("userID")
	userID, _ := userIDVal.(uuid.UUID)

	entity, err := h.EntityService.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Negocio no encontrado"})
		return nil
	}
	if entity.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "No eres el dueño de este negocio"})
		return nil
	}
	return &id
}

// GetSubscription returns the current plan status for a business.
// @Summary Estado de la suscripción de un negocio
// @Tags Payments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Success 200 {object} models.Subscription
// @Router /api/entities/{id}/subscription [get]
func (h *PaymentHandler) GetSubscription(c *gin.Context) {
	id := h.requireOwner(c)
	if id == nil {
		return
	}
	sub, err := h.Service.Status(*id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos consultar la suscripción"})
		return
	}
	c.JSON(http.StatusOK, sub)
}

// CreateCheckout starts a new Wompi Web Checkout payment attempt for a
// business's monthly plan and returns the signed data the app needs to open
// the widget.
// @Summary Iniciar el pago de la suscripción
// @Tags Payments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Success 200 {object} services.CheckoutData
// @Failure 400 {object} map[string]string
// @Router /api/entities/{id}/subscription/checkout [post]
func (h *PaymentHandler) CreateCheckout(c *gin.Context) {
	id := h.requireOwner(c)
	if id == nil {
		return
	}
	if !h.Service.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Los pagos todavía no están configurados"})
		return
	}
	data, err := h.Service.CreateCheckout(*id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

// wompiEvent mirrors the subset of Wompi's webhook payload we need. Wompi
// sends the same shape for every event type; we only act on
// "transaction.updated".
type wompiEvent struct {
	Event string `json:"event"`
	Data  struct {
		Transaction struct {
			ID            string `json:"id"`
			Reference     string `json:"reference"`
			Status        string `json:"status"`
			AmountInCents int64  `json:"amount_in_cents"`
		} `json:"transaction"`
	} `json:"data"`
	Timestamp int64 `json:"timestamp"`
	Signature struct {
		Properties []string `json:"properties"`
		Checksum   string   `json:"checksum"`
	} `json:"signature"`
}

// resolveProperty reads a dot-path like "transaction.status" out of the
// parsed event (only the paths Wompi actually sends here matter).
func (e *wompiEvent) resolveProperty(path string) string {
	switch path {
	case "transaction.id":
		return e.Data.Transaction.ID
	case "transaction.status":
		return e.Data.Transaction.Status
	case "transaction.amount_in_cents":
		return jsonInt(e.Data.Transaction.AmountInCents)
	case "transaction.reference":
		return e.Data.Transaction.Reference
	default:
		return ""
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Webhook receives Wompi's transaction.updated events. Public endpoint (no
// auth — Wompi is the caller), secured instead by the events signature.
// @Summary Webhook de eventos de Wompi
// @Tags Payments
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/payments/wompi/webhook [post]
func (h *PaymentHandler) Webhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	var event wompiEvent
	if err := json.Unmarshal(body, &event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	values := make([]string, 0, len(event.Signature.Properties))
	for _, p := range event.Signature.Properties {
		values = append(values, event.resolveProperty(p))
	}
	if !h.Service.VerifyEventSignature(values, event.Timestamp, event.Signature.Checksum) {
		log.Println("payments: webhook con firma inválida, ignorado")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	if event.Event != "transaction.updated" || event.Data.Transaction.Reference == "" {
		c.JSON(http.StatusOK, gin.H{"message": "ignored"})
		return
	}

	tx := event.Data.Transaction
	var applyErr error
	switch tx.Status {
	case string(models.PaymentApproved):
		applyErr = h.Service.ApplyApprovedPayment(tx.Reference, tx.ID)
	case string(models.PaymentDeclined), string(models.PaymentError), string(models.PaymentVoided):
		applyErr = h.Service.MarkPaymentStatus(tx.Reference, models.PaymentStatus(tx.Status), tx.ID)
	default:
		// PENDING u otro estado transitorio: nada que hacer todavía.
	}
	if applyErr != nil {
		log.Println("payments: error aplicando evento de webhook:", applyErr)
		// 200 igual: si respondemos error, Wompi reintenta con el mismo
		// payload y probablemente vuelva a fallar por lo mismo.
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
