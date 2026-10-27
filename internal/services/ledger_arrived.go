package services

import (
	"math"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ArrivedInput: «ya llegó» un producto o insumo que se acababa. Qty = cuántos llegaron (en semáforo puede ir en 0:
// solo vuelve a «hay»). Paid (opcional) = lo que se pagó: queda como gasto de mercancía y actualiza el costo.
type ArrivedInput struct {
	ClientID uuid.UUID `json:"client_id"`
	Qty      float64   `json:"qty"`
	Paid     int       `json:"paid"`
	Method   string    `json:"method"` // cash | transfer | card (a crédito se registra desde Gastar, con el proveedor)
}

// Arrived suma lo que llegó y, si se dijo cuánto se pagó, registra la compra; todo en una transacción e idempotente
// por client_id. En cantidad exacta suma (o, si nunca se había contado, cuenta); en semáforo vuelve a «hay».
func (s *LedgerService) Arrived(ownerID, entityID, ref uuid.UUID, in ArrivedInput) (InventoryOverview, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryOverview{}, err
	}
	if in.ClientID == uuid.Nil {
		return InventoryOverview{}, &InvalidError{"falta client_id"}
	}
	if in.Qty < 0 || in.Qty > maxQty || math.IsNaN(in.Qty) {
		return InventoryOverview{}, &InvalidError{"cantidad inválida"}
	}
	if in.Paid < 0 {
		return InventoryOverview{}, &InvalidError{"el pago no puede ser negativo"}
	}
	if in.Paid > 0 && in.Qty <= 0 {
		return InventoryOverview{}, &InvalidError{"para registrar lo que pagaste, di cuántos llegaron"}
	}
	if in.Method == "" {
		in.Method = models.PayCash
	}
	if in.Paid > 0 && (in.Method == models.PayCredit || !models.ValidPayMethod(in.Method)) {
		return InventoryOverview{}, &InvalidError{"paga en efectivo, transferencia o tarjeta; a crédito regístralo en Gastar con el proveedor"}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		it, err := s.stockItem(tx, entityID, ref, false, "", true)
		if err != nil {
			return err
		}
		if it.Mode != ledger.ModeLight && it.Mode != ledger.ModeExact {
			return &InvalidError{"este producto no lleva inventario"}
		}
		if it.Mode == ledger.ModeExact && in.Qty <= 0 {
			return &InvalidError{"di cuántos llegaron"}
		}
		now := nowCO()
		counted := it.CountedAt != nil
		if in.Paid > 0 && in.Qty > 0 {
			line := PurchaseLine{Qty: in.Qty, UnitCost: int(math.Round(float64(in.Paid) / in.Qty))}
			if it.ServiceItemID != nil {
				line.ItemID = it.ServiceItemID
			} else {
				line.StockItemID = &it.ID
			}
			// La compra suma sola si ya estaba contado y pone «hay» en semáforo.
			if _, _, err := s.createExpense(entityID, ExpenseInput{ClientID: in.ClientID, Amount: in.Paid, Category: "mercancia", Method: in.Method, Lines: []PurchaseLine{line}}, tx); err != nil {
				return err
			}
			if it.Mode == ledger.ModeLight || counted {
				return nil
			}
			// Exacto sin contar: lo que llegó es lo que hay.
			it, err = s.stockItem(tx, entityID, ref, false, "", true)
			if err != nil {
				return err
			}
		}
		switch {
		case it.Mode == ledger.ModeLight:
			return s.setLevel(tx, entityID, ref, ledger.LevelOK, in.ClientID)
		case counted:
			_, err := s.move(tx, it, models.MovePurchase, in.Qty, models.ReasonPurchase, nil, uuid.NewSHA1(in.ClientID, []byte("arrived")), now)
			return err
		default:
			if _, err := s.move(tx, it, models.MoveCount, in.Qty-it.Qty, models.ReasonCount, nil, uuid.NewSHA1(in.ClientID, []byte("arrived-count")), now); err != nil {
				return err
			}
			it.CountedAt = &now
			if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("counted_at", now).Error; err != nil {
				return err
			}
			return syncSoldOut(tx, it)
		}
	})
	if err != nil {
		return InventoryOverview{}, err
	}
	return s.inventory(entityID)
}

// lastPurchases: la cantidad de la última compra de cada producto o insumo, por id de existencia o de catálogo.
func (s *LedgerService) lastPurchases(entityID uuid.UUID) (map[uuid.UUID]float64, error) {
	var rows []struct {
		ItemID      *uuid.UUID
		StockItemID *uuid.UUID
		Qty         float64
	}
	err := s.db.Raw(`SELECT DISTINCT ON (COALESCE(l.item_id, l.stock_item_id)) l.item_id, l.stock_item_id, l.qty
		FROM expense_lines l JOIN expenses e ON e.id = l.expense_id
		WHERE e.entity_id = ? AND e.status = ?
		ORDER BY COALESCE(l.item_id, l.stock_item_id), e.occurred_at DESC`, entityID, models.LedgerActive).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]float64, len(rows))
	for _, r := range rows {
		switch {
		case r.ItemID != nil:
			out[*r.ItemID] = r.Qty
		case r.StockItemID != nil:
			out[*r.StockItemID] = r.Qty
		}
	}
	return out, nil
}
