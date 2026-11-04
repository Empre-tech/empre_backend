package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"empre_backend/internal/services"
)

// SetSaleMethod cambia cómo se pagó una venta.
func (h *LedgerHandler) SetSaleMethod(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	id, ok := uuidParam(c, "saleId")
	if !ok {
		return
	}
	var body struct {
		Method string `json:"method"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	sale, err := h.svc.SetSaleMethod(user, entity, id, body.Method)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sale)
}

// ---- caja ----

func (h *LedgerHandler) CashDay(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.CashDay(user, entity, c.Query("day"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) CreateCashMove(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.CashMoveInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	m, isNew, err := h.svc.CreateCashMove(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	created(c, isNew, m)
}

func (h *LedgerHandler) VoidCashMove(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	id, ok := uuidParam(c, "moveId")
	if !ok {
		return
	}
	m, err := h.svc.VoidCashMove(user, entity, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *LedgerHandler) CloseCash(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.CashCloseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.CloseCash(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
