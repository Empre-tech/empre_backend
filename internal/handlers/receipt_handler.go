package handlers

import (
	"bytes"
	"errors"
	"net/http"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ReceiptPage: GET /t/:saleId, el ticket digital de una venta (público para quien tenga el enlace, sin sesión).
func (h *LedgerHandler) ReceiptPage(c *gin.Context) {
	c.Header("X-Robots-Tag", "noindex")
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("saleId"))
	if err != nil {
		receiptMissing(c)
		return
	}
	r, err := h.svc.PublicReceipt(id)
	if errors.Is(err, services.ErrLedgerNotFound) {
		receiptMissing(c)
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	var buf bytes.Buffer
	if err := legalPages.ExecuteTemplate(&buf, "ticket.html", r); err != nil {
		internalError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
}

func receiptMissing(c *gin.Context) {
	c.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Ticket</title><p style="font:16px system-ui;padding:32px;text-align:center">Este ticket no existe o ya no está disponible.</p>`))
}
