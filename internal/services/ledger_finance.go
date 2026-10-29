package services

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
)

// Lo que Empre Finance necesita ya sumado (3.51): lo de cada día del periodo y los gastos que se repiten. Antes la app
// bajaba hasta 600 registros para armar las barras de la semana (y se cortaba en negocios con mucho movimiento) y 300
// gastos para sacar 4 sugerencias.

// DayTotals es lo que se vendió y se gastó un día (hora de Colombia).
type DayTotals struct {
	Date     string `json:"date"` // AAAA-MM-DD
	Sales    int    `json:"sales"`
	Expenses int    `json:"expenses"`
}

// DailyTotals reparte ventas y gastos activos por día entre [from, to), con un renglón por cada día aunque esté en cero.
func DailyTotals(sales []models.Sale, exps []models.Expense, from, to time.Time) []DayTotals {
	loc := from.Location()
	var out []DayTotals
	index := map[string]int{}
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		index[key] = len(out)
		out = append(out, DayTotals{Date: key})
	}
	for _, s := range sales {
		if s.Status != models.LedgerActive {
			continue
		}
		if i, ok := index[s.OccurredAt.In(loc).Format("2006-01-02")]; ok {
			out[i].Sales += s.Total
		}
	}
	for _, e := range exps {
		if e.Status != models.LedgerActive {
			continue
		}
		if i, ok := index[e.OccurredAt.In(loc).Format("2006-01-02")]; ok {
			out[i].Expenses += e.Amount
		}
	}
	return out
}

// UsualExpense es un gasto que se repite: el nombre (la nota, o vacío si va por categoría), su categoría, el último
// monto y cuántas veces se registró.
type UsualExpense struct {
	Key      string `json:"key"`
	Note     string `json:"note"`
	Category string `json:"category"`
	Amount   int    `json:"amount"`
	Count    int    `json:"count"`
}

// UsualWindowDays es cuánto hacia atrás se miran los gastos repetidos.
const UsualWindowDays = 30

// UsualExpenses agrupa los gastos activos por su nota (o por categoría si no tiene) y devuelve los que se repiten al
// menos dos veces, primero los más frecuentes y luego los más recientes. El monto es el del último registro.
func UsualExpenses(exps []models.Expense, limit int) []UsualExpense {
	type group struct {
		UsualExpense
		last time.Time
	}
	groups := map[string]*group{}
	for _, e := range exps {
		if e.Status != models.LedgerActive || e.Amount <= 0 {
			continue
		}
		note := strings.TrimSpace(e.Note)
		key := "cat:" + e.Category
		if note != "" {
			key = "note:" + ledger.Normalize(note)
		}
		g := groups[key]
		if g == nil {
			groups[key] = &group{UsualExpense{Key: key, Note: note, Category: e.Category, Amount: e.Amount, Count: 1}, e.OccurredAt}
			continue
		}
		g.Count++
		if e.OccurredAt.After(g.last) {
			g.last = e.OccurredAt
			g.Amount = e.Amount
			g.Category = e.Category
			g.Note = note
		}
	}
	list := make([]*group, 0, len(groups))
	for _, g := range groups {
		if g.Count >= 2 {
			list = append(list, g)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Count != list[j].Count {
			return list[i].Count > list[j].Count
		}
		if !list[i].last.Equal(list[j].last) {
			return list[i].last.After(list[j].last)
		}
		return list[i].Key < list[j].Key
	})
	out := []UsualExpense{}
	for _, g := range list {
		if len(out) == limit {
			break
		}
		out = append(out, g.UsualExpense)
	}
	return out
}

// ListUsualExpenses devuelve los gastos de siempre del negocio en los últimos UsualWindowDays días.
func (s *LedgerService) ListUsualExpenses(ownerID, entityID uuid.UUID, now time.Time, limit int) ([]UsualExpense, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	var exps []models.Expense
	if err := s.db.Select("note", "category", "amount", "occurred_at", "status").
		Where("entity_id = ? AND status = ? AND occurred_at >= ?", entityID, models.LedgerActive, now.AddDate(0, 0, -UsualWindowDays)).
		Find(&exps).Error; err != nil {
		return nil, err
	}
	return UsualExpenses(exps, limit), nil
}
