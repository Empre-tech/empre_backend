package services

import (
	"sort"
	"time"

	"github.com/google/uuid"

	"empre_backend/internal/models"
)

// debtEntry: algo que se quedó debiendo, con cuánto y cuándo.
type debtEntry struct {
	At     time.Time
	Amount int
}

// oldestUnpaid: la fecha de lo más viejo que sigue sin pagar. Los abonos cubren primero lo más antiguo, así que se
// recorre de lo más nuevo hacia atrás hasta completar lo que aún se debe.
func oldestUnpaid(entries []debtEntry, balance int) *time.Time {
	if balance <= 0 || len(entries) == 0 {
		return nil
	}
	sorted := append([]debtEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At.After(sorted[j].At) })
	left := balance
	for i := range sorted {
		left -= sorted[i].Amount
		if left <= 0 {
			return &sorted[i].At
		}
	}
	return &sorted[len(sorted)-1].At
}

// fillOldest le pone a cada quien desde cuándo debe.
func (s *LedgerService) fillOldest(entityID uuid.UUID, list []PartyBalance) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	entries := map[uuid.UUID][]debtEntry{}
	var sales []models.Sale
	if err := s.db.Select("party_id", "occurred_at", "total", "method", "method2", "amount2").
		Where("entity_id = ? AND status = ? AND party_id IN ? AND (method = ? OR method2 = ?)", entityID, models.LedgerActive, ids, models.PayCredit, models.PayCredit).
		Find(&sales).Error; err != nil {
		return err
	}
	for _, sl := range sales {
		for _, pay := range sl.Pays() {
			if pay.Method == models.PayCredit && sl.PartyID != nil {
				entries[*sl.PartyID] = append(entries[*sl.PartyID], debtEntry{sl.OccurredAt, pay.Amount})
			}
		}
	}
	var exps []models.Expense
	if err := s.db.Select("party_id", "occurred_at", "amount").
		Where("entity_id = ? AND status = ? AND party_id IN ? AND method = ?", entityID, models.LedgerActive, ids, models.PayCredit).
		Find(&exps).Error; err != nil {
		return err
	}
	for _, e := range exps {
		if e.PartyID != nil {
			entries[*e.PartyID] = append(entries[*e.PartyID], debtEntry{e.OccurredAt, e.Amount})
		}
	}
	for i := range list {
		list[i].OldestAt = oldestUnpaid(entries[list[i].ID], list[i].Balance)
	}
	return nil
}
