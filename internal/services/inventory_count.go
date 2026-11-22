package services

import (
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
)

// Contar una sección (2026-11-16, tablero «Inventario», teléfonos 9–12): el dueño cuenta los productos o insumos de una
// sección y guarda todo junto. Cada fila trae lo que había cuando empezó a contar (Expected): se aplica la diferencia
// contra eso, así una venta que entró mientras contaba no se pierde. Un faltante puede llevar su motivo (se dañó, venció,
// se perdió) y entonces cuenta como pérdida de plata; si no, es un conteo. Cada conteo queda guardado con su fecha, y
// con eso cada sección dice cuándo se contó por última vez.

const maxCountLines = 500

// CountLine es una fila contada: Counted (cantidad exacta) o Level (hay, queda poco, se acabó), una de las dos.
type CountLine struct {
	Ref      uuid.UUID `json:"ref"`
	Expected *float64  `json:"expected"`
	Counted  *float64  `json:"counted"`
	Level    string    `json:"level"`
	Reason   string    `json:"reason"`
}

// CountInput es el conteo de una sección. ClientID lo hace idempotente.
type CountInput struct {
	ClientID uuid.UUID   `json:"client_id"`
	Tab      string      `json:"tab"`
	Section  string      `json:"section"`
	Lines    []CountLine `json:"lines"`
}

// CountResult: cuántas filas se contaron, cuántas cambiaron y cuándo.
type CountResult struct {
	Counted   int       `json:"counted"`
	Changed   int       `json:"changed"`
	CountedAt time.Time `json:"counted_at"`
}

// countDelta dice cuánto cambia una fila contada. Si ya se había contado y la app mandó lo que había al empezar, es
// contado − esperado (lo vendido mientras se contaba sigue descontado); nunca deja la existencia bajo cero. Si nunca se
// había contado, lo contado es lo que hay.
func countDelta(qtyNow, counted float64, expected *float64, countedBefore bool) float64 {
	if expected != nil && countedBefore {
		d := counted - *expected
		if qtyNow+d < 0 {
			d = -qtyNow
		}
		return ledger.RoundQty(d)
	}
	return ledger.RoundQty(counted - qtyNow)
}

// validCountInput revisa lo que llega antes de tocar nada.
func validCountInput(in CountInput) error {
	if in.ClientID == uuid.Nil {
		return &InvalidError{"falta client_id"}
	}
	if in.Tab != TabProduct && in.Tab != TabSupply {
		return &InvalidError{"se cuentan productos o insumos"}
	}
	if len(in.Lines) == 0 || len(in.Lines) > maxCountLines {
		return &InvalidError{"el conteo no tiene filas o tiene demasiadas"}
	}
	for _, l := range in.Lines {
		if l.Ref == uuid.Nil {
			return &InvalidError{"una fila no dice qué producto es"}
		}
		if (l.Counted == nil) == (l.Level == "") {
			return &InvalidError{"cada fila lleva una cantidad o un nivel"}
		}
		if l.Counted != nil && (*l.Counted < 0 || *l.Counted > maxQty || math.IsNaN(*l.Counted)) {
			return &InvalidError{"cantidad inválida"}
		}
		if l.Expected != nil && (*l.Expected < 0 || *l.Expected > maxQty || math.IsNaN(*l.Expected)) {
			return &InvalidError{"cantidad inválida"}
		}
		if l.Level != "" && !ledger.ValidLevel(l.Level) {
			return &InvalidError{"nivel inválido: usa ok, low u out"}
		}
		if l.Reason != "" && l.Reason != models.ReasonCount && !models.IsLossReason(l.Reason) {
			return &InvalidError{"motivo inválido"}
		}
	}
	return nil
}

// CountSection guarda el conteo de una sección de una vez (solo el dueño). Reenviar el mismo client_id devuelve el
// resultado guardado sin aplicar nada otra vez.
func (s *LedgerService) CountSection(ownerID, entityID uuid.UUID, in CountInput) (CountResult, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return CountResult{}, err
	}
	// Contar una sección es de Empre Pro.
	if err := s.requirePro(entityID); err != nil {
		return CountResult{}, err
	}
	in.Section = strings.TrimSpace(in.Section)
	if r := []rune(in.Section); len(r) > 60 {
		in.Section = string(r[:60])
	}
	if err := validCountInput(in); err != nil {
		return CountResult{}, err
	}
	var res CountResult
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var prev models.InventoryCount
		if found := tx.Where("id = ? AND entity_id = ?", in.ClientID, entityID).Limit(1).Find(&prev); found.Error != nil {
			return found.Error
		} else if found.RowsAffected > 0 {
			res = CountResult{Counted: prev.Lines, Changed: prev.Changed, CountedAt: prev.CountedAt}
			return nil
		}
		now := nowCO()
		changed := 0
		for _, l := range in.Lines {
			cid := uuid.NewSHA1(in.ClientID, []byte("count:"+l.Ref.String()))
			if l.Level != "" {
				it, err := s.stockItem(tx, entityID, l.Ref, false, "", true)
				if err != nil {
					return err
				}
				if it.Mode == ledger.ModeLight && it.Level != l.Level {
					changed++
				}
				if err := s.setLevel(tx, entityID, l.Ref, l.Level, cid); err != nil {
					return err
				}
				continue
			}
			it, err := s.stockItem(tx, entityID, l.Ref, true, ledger.ModeExact, true)
			if err != nil {
				return err
			}
			if it.Mode != ledger.ModeExact {
				it.Mode = ledger.ModeExact // contar es pasar a cantidad exacta
				if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("mode", it.Mode).Error; err != nil {
					return err
				}
			}
			d := countDelta(it.Qty, *l.Counted, l.Expected, it.CountedAt != nil)
			if d != 0 {
				kind, reason := models.MoveCount, models.ReasonCount
				if d < 0 && models.IsLossReason(l.Reason) {
					kind, reason = models.MoveAdjust, l.Reason
				}
				if _, err := s.move(tx, it, kind, d, reason, nil, cid, now); err != nil {
					return err
				}
				changed++
			}
			it.CountedAt = &now
			if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("counted_at", now).Error; err != nil {
				return err
			}
			if err := syncSoldOut(tx, it); err != nil {
				return err
			}
		}
		rec := models.InventoryCount{ID: in.ClientID, EntityID: entityID, Tab: in.Tab, Section: in.Section, Lines: len(in.Lines), Changed: changed, CountedAt: now}
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		res = CountResult{Counted: rec.Lines, Changed: changed, CountedAt: now}
		return nil
	})
	return res, err
}

// countKey: la llave de una sección de una pestaña (sin tildes ni mayúsculas, como el filtro).
func countKey(tab, section string) string { return tab + "|" + fold(section) }

// lastCounts: la última vez que se contó cada sección de cada pestaña.
func (s *LedgerService) lastCounts(entityID uuid.UUID) (map[string]time.Time, error) {
	type row struct {
		Tab     string
		Section string
		At      time.Time
	}
	var rows []row
	if err := s.db.Model(&models.InventoryCount{}).Select("tab, section, MAX(counted_at) AS at").Where("entity_id = ?", entityID).Group("tab, section").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		out[countKey(r.Tab, r.Section)] = r.At
	}
	return out, nil
}

// AttachCountDates pone en cada sección cuándo se contó por última vez (nada si nunca se ha contado).
func AttachCountDates(c *InventoryCounts, last map[string]time.Time) {
	set := func(tab string, tc *TabCounts) {
		for i := range tc.Sections {
			if t, ok := last[countKey(tab, tc.Sections[i].Name)]; ok {
				at := t
				tc.Sections[i].CountedAt = &at
			}
		}
	}
	set(TabProduct, &c.Products)
	set(TabService, &c.Services)
	set(TabSupply, &c.Supplies)
}
