package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
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

type setProRequest struct {
	// Days: cuántos días de Pro desde hoy (1 a 400). 0 lo devuelve a Base.
	Days *int `json:"days" binding:"required,min=0,max=400"`
}

// SetPro (solo admin, para pruebas): POST /api/admin/entities/:id/pro {"days": 30} pone el negocio en Empre Pro por
// esos días; {"days": 0} lo deja en Base.
func (h *LedgerHandler) SetPro(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "id inválido"})
		return
	}
	var req setProRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "days debe estar entre 0 y 400"})
		return
	}
	var until *time.Time
	if *req.Days > 0 {
		t := time.Now().AddDate(0, 0, *req.Days)
		until = &t
	}
	sub, err := h.svc.SetPro(entityID, until)
	if err != nil {
		h.fail(c, err)
		return
	}
	plan := "base"
	if until != nil {
		plan = "pro"
	}
	c.JSON(http.StatusOK, gin.H{"plan": plan, "status": sub.Status, "current_period_end": sub.CurrentPeriodEnd})
}

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
	case errors.Is(err, services.ErrProRequired):
		c.JSON(http.StatusPaymentRequired, gin.H{"error": "pro_required", "message": "Esto es de Empre Pro."})
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

// Analytics: el análisis del negocio para un rango libre (from y to en AAAA-MM-DD, ambos incluidos) comparado con
// compare = prev (los días justo antes, por defecto), month (mismas fechas del mes anterior) o year (del año anterior).
func (h *LedgerHandler) Analytics(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.Analytics(user, entity, c.Query("from"), c.Query("to"), c.DefaultQuery("compare", "prev"))
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

// VoiceUsage: cuántos dictados lleva el negocio este mes y su tope (para decirlo antes de usar Empre AI).
func (h *LedgerHandler) VoiceUsage(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.VoiceUsageFor(user, entity)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) VoiceParse(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
		// TabID: se dicta desde dentro de una cuenta abierta; lo pedido va a esa cuenta.
		TabID *uuid.UUID `json:"tab_id"`
		// Module: dónde se abrió la hoja (finance, inventory, debts). Solo desempata lo que tiene dos lecturas.
		Module string `json:"module"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.ParseVoiceAt(user, entity, body.Text, body.TabID, body.Module)
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

// ---- inventario ----

func (h *LedgerHandler) Inventory(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.Inventory(user, entity)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// InventoryPage: una página de una pestaña del inventario (tablero «Inventario»): ?tab=product|service|supply&q=&section=
// &state=low|out|nophoto&sort=az|stock|sales&cursor=&limit=. Las variantes de los productos de la página van aparte.
func (h *LedgerHandler) InventoryPage(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	q, err := services.ParseInventoryPageQuery(c.Query("tab"), c.Query("q"), c.Query("section"), c.Query("state"), c.Query("sort"), c.Query("cursor"), c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "consulta inválida"})
		return
	}
	out, err := h.svc.InventoryPage(user, entity, q)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// InventoryCounts: los números de las tres pestañas y del filtro, sin las filas.
func (h *LedgerHandler) InventoryCounts(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.InventoryCountsFor(user, entity)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// InventorySummary: lo de arriba de Inventario (avisos, empezar a llevar inventario, revisar qué es cada cosa y la
// nota de precios) ya contado, sin las filas.
func (h *LedgerHandler) InventorySummary(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.InventorySummaryFor(user, entity, time.Now())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// InventoryItems: solo las filas pedidas, ?ids=a,b,c (de la fila o de su ítem del catálogo; hasta 100). Para el aviso
// de lo que se vendió hoy.
func (h *LedgerHandler) InventoryItems(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var ids []uuid.UUID
	for _, raw := range strings.Split(c.Query("ids"), ",") {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil || len(ids) >= 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "ids inválidos"})
			return
		}
		ids = append(ids, id)
	}
	out, err := h.svc.InventoryItemsByIDs(user, entity, ids)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// InventoryNames: el nombre y el tipo de cada fila (para que Empre AI entienda lo que se dicta). Se pide al abrirlo.
func (h *LedgerHandler) InventoryNames(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.InventoryNamesFor(user, entity)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"names": out})
}

// InventoryCount: guarda el conteo de una sección de una vez (tablero «Inventario», teléfonos 9–12). Reenviar el mismo
// client_id devuelve lo guardado sin aplicar nada otra vez.
func (h *LedgerHandler) InventoryCount(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.CountInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "conteo inválido"})
		return
	}
	out, err := h.svc.CountSection(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// InventorySetup: un toque para arrancar. choice = recommended | simple | none.
func (h *LedgerHandler) InventorySetup(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var body struct {
		Choice string `json:"choice"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.SetupInventory(user, entity, body.Choice)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) InventorySettings(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	ref, ok := uuidParam(c, "ref")
	if !ok {
		return
	}
	var in services.ItemSettings
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.UpdateStockSettings(user, entity, ref, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// InventoryArrived: «ya llegó» (suma lo que llegó y, si se dice cuánto se pagó, registra la compra).
func (h *LedgerHandler) InventoryArrived(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	ref, ok := uuidParam(c, "ref")
	if !ok {
		return
	}
	var in services.ArrivedInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.Arrived(user, entity, ref, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) InventoryLevel(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	ref, ok := uuidParam(c, "ref")
	if !ok {
		return
	}
	var body struct {
		ClientID uuid.UUID `json:"client_id"`
		Level    string    `json:"level"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	if err := h.svc.SetLevel(user, entity, ref, body.Level, body.ClientID); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *LedgerHandler) InventoryAdjust(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.AdjustInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.Adjust(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) CreateSupply(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.SupplyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.CreateSupply(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) ArchiveSupply(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	ref, ok := uuidParam(c, "ref")
	if !ok {
		return
	}
	if err := h.svc.ArchiveSupply(user, entity, ref); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *LedgerHandler) Insights(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.Insights(user, entity)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"insights": out})
}

// InventoryIngredients guarda qué insumos usa un producto o plato.
func (h *LedgerHandler) InventoryIngredients(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	ref, ok := uuidParam(c, "ref")
	if !ok {
		return
	}
	// «items» lleva cantidades (recetas); «supplies» es la forma simple, solo los insumos.
	var in struct {
		Supplies []uuid.UUID                `json:"supplies"`
		Items    []services.IngredientInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	items := in.Items
	for _, r := range in.Supplies {
		items = append(items, services.IngredientInput{Ref: r})
	}
	out, err := h.svc.SetRecipe(user, entity, ref, items)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ---------- cuentas abiertas ----------

func (h *LedgerHandler) ListTabs(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	out, err := h.svc.ListTabs(user, entity)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *LedgerHandler) OpenTab(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	var in services.OpenTabInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.OpenTab(user, entity, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) AddToTab(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	tab, ok := uuidParam(c, "tabId")
	if !ok {
		return
	}
	var in struct {
		Lines []services.TabLineInput `json:"lines"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.AddToTab(user, entity, tab, in.Lines)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) RemoveFromTab(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	tab, ok := uuidParam(c, "tabId")
	if !ok {
		return
	}
	line, ok := uuidParam(c, "lineId")
	if !ok {
		return
	}
	out, err := h.svc.RemoveFromTab(user, entity, tab, line)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) PayTab(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	tab, ok := uuidParam(c, "tabId")
	if !ok {
		return
	}
	var in services.PayTabInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "datos inválidos"})
		return
	}
	out, err := h.svc.PayTab(user, entity, tab, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *LedgerHandler) CancelTab(c *gin.Context) {
	user, entity, ok := h.ids(c)
	if !ok {
		return
	}
	tab, ok := uuidParam(c, "tabId")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&in)
	out, err := h.svc.CancelTab(user, entity, tab, in.Reason)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
