package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// LedgerHandler: administración del negocio (/entities/:id/ledger/...). Solo el dueño.
type LedgerHandler struct {
	svc *services.LedgerService
}

func NewLedgerHandler(svc *services.LedgerService) *LedgerHandler { return &LedgerHandler{svc: svc} }

func (h *LedgerHandler) fail(c *gin.Context, err error) {
	var invalid *services.InvalidError
	switch {
	case errors.As(err, &invalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": invalid.Msg})
	case errors.Is(err, services.ErrLedgerNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, services.ErrLedgerForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, services.ErrVoiceDisabled):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ai_disabled"})
	case errors.Is(err, services.ErrVoiceLimit):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "voice_limit", "message": "Llegaste al límite de dictados de este mes. Puedes seguir registrando a mano."})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
	}
}

// ids resuelve usuario y negocio.
func (h *LedgerHandler) ids(c *gin.Context) (user, entity uuid.UUID, ok bool) {
	user, ok = currentUser(c)
	if !ok {
		return
	}
	entity, ok = uuidParam(c, "id")
	return
}

func created(c *gin.Context, isNew bool, body any) {
	if isNew {
		c.JSON(http.StatusCreated, body)
		return
	}
	c.JSON(http.StatusOK, body) // reenvío: ya estaba guardado
}

// ---- ventas ----

func (h *LedgerHandler) CreateSale(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.SaleInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	sale, isNew, err := h.svc.CreateSale(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	created(c, isNew, sale)
}

func (h *LedgerHandler) VoidSale(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	id, ok := uuidParam(c, "saleId")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	sale, err := h.svc.VoidSale(user, entity, id, body.Reason)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sale)
}

func parseListFilter(c *gin.Context) services.ListFilter {
	f := services.ListFilter{}
	if v, err := strconv.Atoi(c.Query("limit")); err == nil {
		f.Limit = v
	}
	if t, err := time.Parse(time.RFC3339, c.Query("before")); err == nil {
		f.Before = &t
	}
	if t, err := time.Parse(time.RFC3339, c.Query("from")); err == nil {
		f.From = &t
	}
	if t, err := time.Parse(time.RFC3339, c.Query("to")); err == nil {
		f.To = &t
	}
	return f
}

func (h *LedgerHandler) ListSales(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.ListSales(user, entity, parseListFilter(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sales": out})
}

// ---- gastos ----

func (h *LedgerHandler) CreateExpense(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.ExpenseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	e, isNew, err := h.svc.CreateExpense(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	created(c, isNew, e)
}

func (h *LedgerHandler) VoidExpense(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	id, ok := uuidParam(c, "expenseId")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	e, err := h.svc.VoidExpense(user, entity, id, body.Reason)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, e)
}

func (h *LedgerHandler) ListExpenses(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.ListExpenses(user, entity, parseListFilter(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"expenses": out, "categories": services.ExpenseCategories()})
}

// ---- terceros y abonos ----

func (h *LedgerHandler) CreateParty(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.PartyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	p, isNew, err := h.svc.CreateParty(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	created(c, isNew, p)
}

func (h *LedgerHandler) ListParties(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.ListParties(user, entity, c.Query("kind"), c.Query("owing") == "1")
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"parties": out})
}

func (h *LedgerHandler) CreatePayment(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.PaymentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	p, isNew, err := h.svc.CreatePayment(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	created(c, isNew, p)
}

func (h *LedgerHandler) ListPayments(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	party, ok := uuidParam(c, "partyId")
	if !ok {
		return
	}
	out, err := h.svc.ListPayments(user, entity, party)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"payments": out})
}

// ---- catálogo con costo ----

func (h *LedgerHandler) ListItems(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.ListItems(user, entity)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// UsualExpenses: los gastos que se repiten en los últimos 30 días (Empre Finance, 3.51).
func (h *LedgerHandler) UsualExpenses(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.ListUsualExpenses(user, entity, time.Now(), 4)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *LedgerHandler) SetItemCost(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	item, ok := uuidParam(c, "itemId")
	if !ok {
		return
	}
	var body struct {
		Cost *int `json:"cost"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	if err := h.svc.SetItemCost(user, entity, item, body.Cost); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- resumen y exportar ----

func (h *LedgerHandler) Summary(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	ref := time.Now()
	if d := c.Query("date"); d != "" {
		t, err := time.ParseInLocation("2006-01-02", d, time.FixedZone("COT", -5*3600))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "date debe ser AAAA-MM-DD"})
			return
		}
		ref = t
	}
	out, err := h.svc.Summary(user, entity, c.DefaultQuery("period", "day"), ref)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) Export(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	ref := time.Now()
	if d := c.Query("month"); d != "" {
		t, err := time.ParseInLocation("2006-01", d, time.FixedZone("COT", -5*3600))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "month debe ser AAAA-MM"})
			return
		}
		ref = t
	}
	from, to, _ := services.PeriodRange("month", ref)
	data, err := h.svc.ExportCSV(user, entity, from, to)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="empre-`+from.Format("2006-01")+`.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

// ---- voz ----

func (h *LedgerHandler) VoiceParse(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.ParseVoice(user, entity, body.Text)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) VoiceConfirm(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	draft, ok := uuidParam(c, "draftId")
	if !ok {
		return
	}
	var body struct {
		Actions []services.VoiceAction `json:"actions"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.ConfirmVoice(user, entity, draft, body.Actions)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) VoiceDiscard(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	draft, ok := uuidParam(c, "draftId")
	if !ok {
		return
	}
	if err := h.svc.DiscardVoice(user, entity, draft); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
