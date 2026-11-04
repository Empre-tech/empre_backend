package services

import (
	"time"

	"github.com/google/uuid"

	"empre_backend/internal/models"
)

// normalWeeks: cuántas semanas hacia atrás cuentan para decir qué es normal en cada día de la semana.
const normalWeeks = 6

// normalMinWeeks: con menos semanas con ventas no hay historia suficiente y no se dice nada.
const normalMinWeeks = 4

// salesByBucket suma las ventas en las barras de un rango; lo que cae fuera no cuenta.
func salesByBucket(bucket string, from, to time.Time, sales []models.Sale) []int {
	starts := bucketStarts(bucket, from, to)
	idx := make(map[string]int, len(starts))
	for i, st := range starts {
		idx[st.Format(dayFmt)] = i
	}
	out := make([]int, len(starts))
	loc := from.Location()
	for _, sl := range sales {
		if i, ok := idx[bucketOf(bucket, sl.OccurredAt.In(loc)).Format(dayFmt)]; ok {
			out[i] += sl.Total
		}
	}
	return out
}

// normalForDays: para cada día de [from, to), lo que se suele vender ese día de la semana según las ventas dadas
// (las semanas anteriores a from). Devuelve nil si no hay al menos normalMinWeeks semanas con ventas.
func normalForDays(from, to time.Time, history []models.Sale) []int {
	loc := from.Location()
	weeks := map[string]bool{}
	var total [7]int
	for _, sl := range history {
		t := sl.OccurredAt.In(loc)
		weeks[bucketOf("week", t).Format(dayFmt)] = true
		total[(int(t.Weekday())+6)%7] += sl.Total
	}
	if len(weeks) < normalMinWeeks {
		return nil
	}
	var out []int
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		out = append(out, total[(int(d.Weekday())+6)%7]/len(weeks))
	}
	return out
}

// salesHistory: las ventas de las semanas anteriores a from, sin renglones (solo cuenta el total).
func (s *LedgerService) salesHistory(entityID uuid.UUID, from time.Time) ([]models.Sale, error) {
	var sales []models.Sale
	err := s.db.Where("entity_id = ? AND status = ? AND occurred_at >= ? AND occurred_at < ?",
		entityID, models.LedgerActive, from.AddDate(0, 0, -7*normalWeeks), from).Find(&sales).Error
	return sales, err
}
