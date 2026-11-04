package services

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"empre_backend/internal/models"
)

// plainPrice: un precio escrito solo con cifras («25000», «$25.000»); «15%» o «2x1» no dicen cuánto se cobró.
var plainPrice = regexp.MustCompile(`^\s*\$?\s*\d[\d.,\s]*$`)

// offerSaleAmount: lo que se cobra al canjear una oferta. 0 si la oferta no lo dice en pesos.
func offerSaleAmount(o *models.Offer) int {
	if o.SpecialPrice != nil && *o.SpecialPrice > 0 {
		return *o.SpecialPrice
	}
	if !plainPrice.MatchString(o.Price) {
		return 0
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, o.Price)
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return n
}

// RecordRedemption registra sola la venta de un canje, en efectivo hasta que el dueño diga otra cosa. El id de la
// venta es el del reclamo: repetir no duplica. Devuelve nil sin error si la oferta no dice cuánto se cobró.
func (s *LedgerService) RecordRedemption(claim *models.OfferClaim, offer *models.Offer) (*models.Sale, error) {
	total := offerSaleAmount(offer)
	if total <= 0 {
		return nil, nil
	}
	if err := checkAmount(total, "el total"); err != nil {
		return nil, nil
	}
	var items []models.OfferItem
	if err := s.db.Where("offer_id = ?", offer.ID).Order("position ASC, created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	lines := make([]LineInput, 0, len(items))
	for _, it := range items {
		lines = append(lines, LineInput{ItemID: it.ServiceItemID, Name: it.Name, Qty: 1, UnitPrice: it.Price})
	}
	in := SaleInput{ClientID: claim.ID, OccurredAt: claim.RedeemedAt, Total: &total, Method: models.PayCash, Note: "Canje " + claim.ShortCode, Lines: lines}
	var sale models.Sale
	record := func(input SaleInput) error {
		return s.db.Transaction(func(tx *gorm.DB) error {
			made, _, err := s.createSale(claim.EntityID, input, tx)
			if err != nil {
				return err
			}
			sourceID := claim.ID
			if err := tx.Model(&models.Sale{}).Where("id = ?", made.ID).
				Updates(map[string]any{"source": models.SourceOfferClaim, "source_id": sourceID}).Error; err != nil {
				return err
			}
			made.Source, made.SourceID = models.SourceOfferClaim, &sourceID
			sale = made
			return nil
		})
	}
	err := record(in)
	var invalid *InvalidError
	if errors.As(err, &invalid) && len(lines) > 0 {
		// Un servicio incluido ya no se puede vender tal cual (variantes, sin precio): la plata sí se registra.
		in.Lines = nil
		err = record(in)
	}
	if err != nil {
		return nil, err
	}
	return &sale, nil
}

// SetSaleMethod cambia cómo se pagó una venta de una sola forma: lo elige el dueño después de un canje.
func (s *LedgerService) SetSaleMethod(ownerID, entityID, saleID uuid.UUID, method string) (models.Sale, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.Sale{}, err
	}
	if method == models.PayCredit || !models.ValidPayMethod(method) {
		return models.Sale{}, &InvalidError{"medio de pago inválido"}
	}
	var sale models.Sale
	if err := s.db.Where("id = ? AND entity_id = ?", saleID, entityID).First(&sale).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return sale, ErrLedgerNotFound
		}
		return sale, err
	}
	if sale.Status != models.LedgerActive || sale.Method == models.PayCredit || sale.Method2 != "" {
		return sale, &InvalidError{"esta venta no se puede cambiar: anúlala y regístrala otra vez"}
	}
	sale.Method = method
	err := s.db.Model(&models.Sale{}).Where("id = ?", sale.ID).Update("method", method).Error
	return sale, err
}
