package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"empre_backend/internal/models"
)

// El ticket digital de una venta (2026-11-22): una página pública en /t/<id de la venta> que el negocio manda por
// WhatsApp a su cliente. El id es el UUID aleatorio que creó el teléfono al anotar la venta: no se puede adivinar.
// No es factura electrónica y lo dice.

// ReceiptLine es un renglón del ticket, ya escrito.
type ReceiptLine struct {
	Qty   string
	Name  string
	Price string
}

// Receipt es lo que muestra el ticket.
type Receipt struct {
	Business string
	Address  string
	When     string
	Number   string
	Lines    []ReceiptLine
	Total    string
	Pays     []string
	Customer string
	Note     string
	Voided   bool
}

var receiptMonths = []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

var payLabel = map[string]string{
	models.PayCash:     "Efectivo",
	models.PayTransfer: "Transferencia",
	models.PayCard:     "Tarjeta",
	models.PayCredit:   "Fiado",
}

func receiptWhen(t time.Time) string {
	if loc, err := time.LoadLocation("America/Bogota"); err == nil {
		t = t.In(loc)
	}
	h, ampm := t.Hour(), "a. m."
	if h >= 12 {
		ampm = "p. m."
	}
	if h%12 == 0 {
		h = 12
	} else {
		h %= 12
	}
	return fmt.Sprintf("%d %s %d · %d:%02d %s", t.Day(), receiptMonths[t.Month()-1], t.Year(), h, t.Minute(), ampm)
}

// PublicReceipt arma el ticket de una venta para su página pública. ErrLedgerNotFound si no existe.
func (s *LedgerService) PublicReceipt(saleID uuid.UUID) (Receipt, error) {
	var sale models.Sale
	err := s.db.Preload("Lines").Preload("Party").First(&sale, "id = ?", saleID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Receipt{}, ErrLedgerNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	var ent models.Entity
	if err := s.db.Select("id", "name", "address", "city").First(&ent, "id = ?", sale.EntityID).Error; err != nil {
		return Receipt{}, ErrLedgerNotFound
	}
	r := Receipt{
		Business: ent.Name,
		Address:  strings.Trim(strings.Join([]string{ent.Address, ent.City}, ", "), ", "),
		When:     receiptWhen(sale.OccurredAt),
		Number:   strings.ToUpper(sale.ID.String()[:8]),
		Total:    pesos(sale.Total),
		Note:     sale.Note,
		Voided:   sale.Status == models.LedgerVoided,
	}
	for _, l := range sale.Lines {
		r.Lines = append(r.Lines, ReceiptLine{Qty: fmtQty(l.Qty), Name: l.Name, Price: pesos(int(float64(l.UnitPrice)*l.Qty + 0.5))})
	}
	for _, p := range sale.Pays() {
		label := payLabel[p.Method]
		if label == "" {
			label = p.Method
		}
		r.Pays = append(r.Pays, fmt.Sprintf("%s %s", label, pesos(p.Amount)))
	}
	if sale.Party != nil {
		// Solo el primer nombre: la página es pública para quien tenga el enlace.
		r.Customer = strings.Fields(sale.Party.Name + " ")[0]
	}
	return r, nil
}
