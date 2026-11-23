package services

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"empre_backend/internal/models"
)

// Recordatorio de fiados (2026-11-23): los lunes en la mañana, cada dueño con clientes que le deben recibe una sola
// notificación con el total, cuántos son y quién lleva más tiempo debiendo. Al tocarla abre Cuentas, donde les
// recuerda uno a uno por WhatsApp. Al cliente nunca le llega nada de Empre.

// Notifier manda una notificación push a un usuario (PushService.Notify).
type Notifier func(userID uuid.UUID, title, body string, data map[string]string)

const debtNoticeKind = "debts_weekly"

// StartDebtReminders revisa cada 30 minutos si ya es lunes en la mañana. Cada aviso se marca en owner_notices, así que
// un reinicio o una segunda instancia no lo repiten.
func (s *LedgerService) StartDebtReminders(ctx context.Context) {
	if s.Notify == nil {
		return
	}
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Println("fiados: tick panic:", r)
						}
					}()
					s.DebtReminderTick(time.Now())
				}()
			}
		}
	}()
}

// DebtReminderTick manda los avisos de la semana si es lunes entre las 9 a. m. y las 8 p. m. (hora de Colombia).
// Devuelve cuántos mandó.
func (s *LedgerService) DebtReminderTick(now time.Time) int {
	if s.Notify == nil {
		return 0
	}
	t := now
	if loc, err := time.LoadLocation("America/Bogota"); err == nil {
		t = now.In(loc)
	}
	if t.Weekday() != time.Monday || t.Hour() < 9 || t.Hour() >= 20 {
		return 0
	}
	year, week := t.ISOWeek()
	period := fmt.Sprintf("%d-W%02d", year, week)

	var entityIDs []uuid.UUID
	if err := s.db.Model(&models.Sale{}).Distinct("entity_id").
		Where("status = ? AND party_id IS NOT NULL AND (method = ? OR method2 = ?)", models.LedgerActive, models.PayCredit, models.PayCredit).
		Pluck("entity_id", &entityIDs).Error; err != nil {
		log.Println("fiados: no se pudieron listar los negocios:", err)
		return 0
	}
	sent := 0
	for _, id := range entityIDs {
		title, body, ok := s.debtNotice(id, now)
		if !ok {
			continue
		}
		res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.OwnerNotice{EntityID: id, Kind: debtNoticeKind, Period: period, SentAt: now})
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		var ent models.Entity
		if err := s.db.Select("id", "owner_id").First(&ent, "id = ?", id).Error; err != nil {
			continue
		}
		s.Notify(ent.OwnerID, title, body, map[string]string{"type": "debts", "entity_id": id.String()})
		sent++
	}
	return sent
}

// debtNotice arma el aviso de un negocio: «Te deben $85.000» / «3 clientes. El que más lleva: Ana, hace 20 días…».
func (s *LedgerService) debtNotice(entityID uuid.UUID, now time.Time) (string, string, bool) {
	bal, err := s.balances(entityID)
	if err != nil {
		return "", "", false
	}
	var parties []models.Party
	if err := s.db.Where("entity_id = ? AND kind = ?", entityID, models.PartyCustomer).Find(&parties).Error; err != nil {
		return "", "", false
	}
	var list []PartyBalance
	total := 0
	for _, p := range parties {
		if b := bal[p.ID]; b > 0 {
			list = append(list, PartyBalance{Party: p, Balance: b})
			total += b
		}
	}
	if total <= 0 {
		return "", "", false
	}
	_ = s.fillOldest(entityID, list)
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i].OldestAt, list[j].OldestAt
		if a == nil || b == nil {
			return a != nil
		}
		return a.Before(*b)
	})
	first := list[0]
	body := ""
	if len(list) == 1 {
		body = fmt.Sprintf("%s te debe %s", first.Name, pesos(first.Balance))
	} else {
		body = fmt.Sprintf("%d clientes. El que más lleva: %s", len(list), first.Name)
	}
	if first.OldestAt != nil {
		if days := int(now.Sub(*first.OldestAt).Hours() / 24); days >= 2 {
			body += fmt.Sprintf(", desde hace %d días", days)
		}
	}
	body += ". Toca para recordarles por WhatsApp."
	return "Te deben " + pesos(total), body, true
}
