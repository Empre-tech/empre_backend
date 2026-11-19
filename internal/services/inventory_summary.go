package services

import (
	"math"
	"time"

	"github.com/google/uuid"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
)

// Resumen del inventario (2026-11-16, «que no traiga todo»): lo que la pantalla de Inventario muestra arriba — los
// avisos, empezar a llevar inventario, revisar qué es cada cosa y la nota de precios — ya contado en el servidor. Antes
// la app bajaba la vista completa y el catálogo entero solo para contar; con miles de productos eso pesaba más que la
// lista. Los nombres (para Empre AI) y las filas de lo vendido hoy (para el aviso de Finance) se piden aparte y solo
// cuando hacen falta. Contar es puro: se prueba sin base de datos.

// AlertCounts: cuántas cosas hay por revisar (las mismas reglas que inventoryAlerts en la app, src/lib/alerts.ts).
type AlertCounts struct {
	Out   int `json:"out"`
	Soon  int `json:"soon"`
	Risk  int `json:"risk"`
	Total int `json:"total"`
}

// UncheckedItem: un ítem que la app clasificó sola (servicio o producto) y el dueño no ha confirmado.
type UncheckedItem struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Kind string    `json:"kind"`
}

// PriceSummary: para la nota «Precios habituales · actualizados hace N días». OldestAt es la fecha más vieja entre
// los precios publicados (cuándo se fijó o se confirmó); sin fecha cuenta como ahora, como en la app.
type PriceSummary struct {
	Priced   int        `json:"priced"`
	Stale    int        `json:"stale"`
	OldestAt *time.Time `json:"oldest_at,omitempty"`
}

// InventorySummary es lo de arriba de la pantalla, sin filas.
type InventorySummary struct {
	Kind     string `json:"kind"`
	Category string `json:"category"`
	// Unset: productos sin decidir si se cuentan. Tracked: cuántos ya se cuentan (hay o exacto).
	Unset   int         `json:"unset"`
	Tracked int         `json:"tracked"`
	Alerts  AlertCounts `json:"alerts"`
	// Unchecked: los primeros por revisar (la tarjeta muestra hasta 8) y cuántos son en total.
	Unchecked      []UncheckedItem `json:"unchecked"`
	UncheckedTotal int             `json:"unchecked_total"`
	Prices         PriceSummary    `json:"prices"`
}

// InventoryName es el nombre de una fila, para que Empre AI entienda lo que se dicta y para no repetir un insumo.
type InventoryName struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

const uncheckedShown = 8

// perDayOf: cuántos se venden al día (mínimo 1), como perDay en la app.
func perDayOf(sold28 float64) float64 {
	return math.Max(1, math.Round(sold28/28))
}

// CountAlerts cuenta lo agotado, lo que se acaba y los platos en riesgo (faltan insumos o no alcanzan para el día).
func CountAlerts(rows []InventoryItem) AlertCounts {
	var c AlertCounts
	for _, i := range rows {
		tracked := (i.Mode == "light" || i.Mode == "exact") && !i.IsService && i.Variants == 0
		if tracked && i.Status == ledger.StatusOut {
			c.Out++
			continue
		}
		if tracked && i.Status == ledger.StatusLow {
			c.Soon++
		}
		if i.Kind == "product" && !i.IsService && i.Status != ledger.StatusOut {
			missing := false
			for _, g := range i.Ingredients {
				if g.Status == ledger.StatusOut {
					missing = true
					break
				}
			}
			if missing || (i.Servings != nil && *i.Servings < perDayOf(i.Sold28)) {
				c.Risk++
			}
		}
	}
	c.Total = c.Out + c.Soon + c.Risk
	return c
}

// SummarizePrices: cuántos precios hay, cuántos no se han confirmado hace tiempo y la fecha más vieja.
func SummarizePrices(items []models.ServiceItem, now time.Time) PriceSummary {
	var p PriceSummary
	for _, it := range items {
		if it.Price == nil {
			continue
		}
		p.Priced++
		if !PriceFreshness(it, now) {
			p.Stale++
		}
		last := now
		if it.PriceUpdatedAt != nil {
			last = *it.PriceUpdatedAt
		}
		if it.ConfirmedAt != nil && (it.PriceUpdatedAt == nil || it.ConfirmedAt.After(last)) {
			last = *it.ConfirmedAt
		}
		if p.OldestAt == nil || last.Before(*p.OldestAt) {
			t := last
			p.OldestAt = &t
		}
	}
	return p
}

// SummarizeInventory arma el resumen con la vista de inventario y los ítems activos del catálogo.
func SummarizeInventory(ov InventoryOverview, items []models.ServiceItem, now time.Time) InventorySummary {
	s := InventorySummary{Kind: ov.Kind, Category: ov.Category, Unset: ov.Unset, Alerts: CountAlerts(ov.Items), Unchecked: []UncheckedItem{}, Prices: SummarizePrices(items, now)}
	for _, i := range ov.Items {
		if !i.IsService && (i.Mode == "light" || i.Mode == "exact") {
			s.Tracked++
		}
	}
	for _, it := range items {
		if it.KindChecked {
			continue
		}
		s.UncheckedTotal++
		if len(s.Unchecked) < uncheckedShown {
			kind := models.ItemProduct
			if it.IsService() {
				kind = models.ItemService
			}
			s.Unchecked = append(s.Unchecked, UncheckedItem{ID: it.ID, Name: it.Name, Kind: kind})
		}
	}
	return s
}

// PickInventory devuelve las filas de esos ids (de la fila o de su ítem del catálogo): lo que se vendió hoy.
func PickInventory(rows []InventoryItem, ids []uuid.UUID) []InventoryItem {
	want := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := []InventoryItem{}
	for _, it := range rows {
		if want[it.Ref] || (it.ItemID != nil && want[*it.ItemID]) {
			out = append(out, it)
		}
	}
	return out
}

// InventoryNames: el nombre y el tipo de cada fila.
func InventoryNames(rows []InventoryItem) []InventoryName {
	out := make([]InventoryName, 0, len(rows))
	for _, it := range rows {
		out = append(out, InventoryName{Name: it.Name, Kind: it.Kind})
	}
	return out
}

// InventorySummaryFor: el resumen de arriba (solo el dueño).
func (s *LedgerService) InventorySummaryFor(ownerID, entityID uuid.UUID, now time.Time) (InventorySummary, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventorySummary{}, err
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return InventorySummary{}, err
	}
	var items []models.ServiceItem
	if err := s.db.Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).Order("position, name").Find(&items).Error; err != nil {
		return InventorySummary{}, err
	}
	return SummarizeInventory(ov, items, now), nil
}

// InventoryItemsByIDs: solo las filas pedidas (solo el dueño).
func (s *LedgerService) InventoryItemsByIDs(ownerID, entityID uuid.UUID, ids []uuid.UUID) ([]InventoryItem, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return nil, err
	}
	return PickInventory(ov.Items, ids), nil
}

// InventoryNamesFor: los nombres de todas las filas (solo el dueño).
func (s *LedgerService) InventoryNamesFor(ownerID, entityID uuid.UUID) ([]InventoryName, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return nil, err
	}
	return InventoryNames(ov.Items), nil
}
