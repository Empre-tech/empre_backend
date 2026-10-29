package services

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Inventario fácil. Principios:
//   - nada es obligatorio: un producto sin StockItem no lleva inventario;
//   - se llena solo: vender descuenta y comprar suma, el dueño casi nunca «hace inventario»;
//   - nunca se bloquea una venta por falta de existencias (el inventario puede estar desactualizado);
//   - los cambios dejan un movimiento (no se reescribe la historia) y son idempotentes.

const (
	maxQty        = 1_000_000
	maxSupplies   = 300
	insightsLimit = 8
)

// ---------- resolver y mover existencias ----------

// stockItem busca el inventario de un producto (ref = id del producto del catálogo) o de un insumo/ítem (ref = id del
// StockItem). Con create=true y un producto sin inventario, lo crea con el modo dado (o el recomendado).
func (s *LedgerService) stockItem(db *gorm.DB, entityID, ref uuid.UUID, create bool, mode string, lock bool) (*models.StockItem, error) {
	q := db
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var it models.StockItem
	err := q.Where("entity_id = ? AND (id = ? OR service_item_id = ?) AND archived = false", entityID, ref, ref).First(&it).Error
	if err == nil {
		return &it, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !create {
		return nil, ErrLedgerNotFound
	}
	var prod models.ServiceItem
	if err := db.Where("id = ? AND entity_id = ? AND status = ?", ref, entityID, models.ServiceActive).First(&prod).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLedgerNotFound
		}
		return nil, err
	}
	if prod.IsService() {
		return nil, &InvalidError{"«" + prod.Name + "» es un servicio: no lleva inventario."}
	}
	if many, err := hasVariants(db, prod.ID); err != nil {
		return nil, err
	} else if many {
		return nil, &InvalidError{"«" + prod.Name + "» tiene variantes: cada una se cuenta por separado."}
	}
	if mode == "" {
		prof, _ := s.profile(entityID)
		mode = ledger.RecommendMode(prof, itemFacts(prod, nil))
		if mode == ledger.ModeNone {
			mode = ledger.ModeLight // el dueño pidió activar el control: el semáforo es lo mínimo
		}
	}
	it = models.StockItem{ID: uuid.New(), EntityID: entityID, ServiceItemID: &prod.ID, Kind: models.StockProduct, Mode: mode, Level: ledger.LevelOK, Unit: "unidad"}
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "service_item_id"}}, DoNothing: true}).Create(&it)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 { // alguien lo creó a la vez
		return s.stockItem(db, entityID, ref, false, "", lock)
	}
	return &it, nil
}

func itemFacts(p models.ServiceItem, units *float64) ledger.ItemFacts {
	return ledger.ItemFacts{Price: p.Price, IsService: p.IsService(), Units: units}
}

// profile devuelve los valores por defecto del negocio según su tipo guardado (business_profile). Si falta, usa el que
// sugiere su categoría y, como último respaldo, el que se deduce del nombre de la categoría.
func (s *LedgerService) profile(entityID uuid.UUID) (ledger.Profile, string) {
	var e models.Entity
	if err := s.db.Select("id", "category_id", "business_profile").First(&e, "id = ?", entityID).Error; err != nil {
		return ledger.ProfileForKind(ledger.KindOther), ""
	}
	var c models.Category
	_ = s.db.Select("id", "name", "profile").First(&c, "id = ?", e.CategoryID).Error
	switch {
	case ledger.ValidKind(e.BusinessProfile):
		return ledger.ProfileForKind(e.BusinessProfile), c.Name
	case ledger.ValidKind(c.Profile):
		return ledger.ProfileForKind(c.Profile), c.Name
	}
	return ledger.ProfileForCategory(c.Name), c.Name
}

// syncSoldOut copia al producto del catálogo si está agotado (lo único del inventario que ve el público).
func syncSoldOut(db *gorm.DB, it *models.StockItem) error {
	if it.ServiceItemID == nil {
		return nil
	}
	out := ledger.SoldOut(it.Mode, it.CountedAt != nil, it.Qty, it.Level)
	if err := db.Model(&models.ServiceItem{}).Where("id = ?", *it.ServiceItemID).UpdateColumn("sold_out", out).Error; err != nil {
		return err
	}
	var v models.ServiceItem
	if err := db.Select("id", "parent_id").First(&v, "id = ?", *it.ServiceItemID).Error; err == nil && v.ParentID != nil {
		return syncParentSoldOut(db, *v.ParentID)
	}
	return nil
}

// move aplica un cambio de cantidad y deja el movimiento. Idempotente por client_id: si ya existe, no hace nada y
// devuelve false.
func (s *LedgerService) move(db *gorm.DB, it *models.StockItem, kind string, delta float64, reason string, ref *uuid.UUID, clientID uuid.UUID, at time.Time) (bool, error) {
	after := ledger.RoundQty(it.Qty + delta)
	mv := models.StockMovement{
		EntityID: it.EntityID, ClientID: clientID, StockItemID: it.ID, Kind: kind, Delta: ledger.RoundQty(delta),
		QtyAfter: after, Reason: reason, RefID: ref, OccurredAt: at,
	}
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "client_id"}}, DoNothing: true}).Create(&mv)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	it.Qty = after
	if err := db.Model(&models.StockItem{}).Where("id = ?", it.ID).Updates(map[string]any{"qty": after, "updated_at": at}).Error; err != nil {
		return false, err
	}
	return true, syncSoldOut(db, it)
}

// stockSale descuenta de las existencias lo vendido (solo productos con cantidad exacta y ya contados).
func (s *LedgerService) stockSale(db *gorm.DB, entityID uuid.UUID, saleID uuid.UUID, at time.Time, lines []models.SaleLine) error {
	sums := map[uuid.UUID]float64{}
	for _, l := range lines {
		if l.ItemID != nil {
			sums[*l.ItemID] += l.Qty
		}
	}
	for itemID, qty := range sums {
		it, err := s.stockItem(db, entityID, itemID, false, "", true)
		if errors.Is(err, ErrLedgerNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if it.Mode != ledger.ModeExact || it.CountedAt == nil {
			continue
		}
		if _, err := s.move(db, it, models.MoveSale, -qty, models.ReasonSale, &saleID, uuid.NewSHA1(saleID, []byte("sale:"+it.ID.String())), at); err != nil {
			return err
		}
	}
	return s.stockUse(db, entityID, saleID, at, sums)
}

// stockUse descuenta lo que gastaron los ítems vendidos según su receta (fase 5): solo insumos con cantidad por
// unidad y llevados en cantidad exacta y ya contados. Idempotente por venta e insumo.
func (s *LedgerService) stockUse(db *gorm.DB, entityID, saleID uuid.UUID, at time.Time, sold map[uuid.UUID]float64) error {
	if len(sold) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(sold))
	for id := range sold {
		ids = append(ids, id)
	}
	var links []models.ProductSupply
	if err := db.Where("entity_id = ? AND product_id IN ? AND qty_per_unit IS NOT NULL", entityID, ids).Find(&links).Error; err != nil {
		return err
	}
	uses := map[uuid.UUID]float64{}
	for _, l := range links {
		uses[l.SupplyID] += *l.QtyPerUnit * sold[l.ProductID]
	}
	for stockID, q := range uses {
		var it models.StockItem
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND archived = false", stockID).First(&it).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		if it.Mode != ledger.ModeExact || it.CountedAt == nil || q <= 0 {
			continue
		}
		if _, err := s.move(db, &it, models.MoveUse, -q, models.ReasonUse, &saleID, uuid.NewSHA1(saleID, []byte("use:"+it.ID.String())), at); err != nil {
			return err
		}
	}
	return nil
}

// reverseMoves devuelve a las existencias lo que movió una venta o una compra que se anula.
func (s *LedgerService) reverseMoves(db *gorm.DB, entityID, refID uuid.UUID, origKind, voidKind string, at time.Time) error {
	var mvs []models.StockMovement
	if err := db.Where("entity_id = ? AND ref_id = ? AND kind = ?", entityID, refID, origKind).Find(&mvs).Error; err != nil {
		return err
	}
	for _, m := range mvs {
		var it models.StockItem
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&it, "id = ?", m.StockItemID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		if _, err := s.move(db, &it, voidKind, -m.Delta, "anulado", &refID, uuid.NewSHA1(m.ID, []byte("void")), at); err != nil {
			return err
		}
	}
	return nil
}

// ---------- compras con productos ----------

// PurchaseLine es un producto o insumo comprado. Qty va en unidades (la app convierte pacas y bultos a unidades).
type PurchaseLine struct {
	ItemID      *uuid.UUID `json:"item_id"`       // producto del catálogo
	StockItemID *uuid.UUID `json:"stock_item_id"` // insumo u otro ítem de inventario
	Name        string     `json:"name"`
	Qty         float64    `json:"qty"`
	UnitCost    int        `json:"unit_cost"`
}

// purchaseLines valida los renglones y los deja listos para guardar.
func (s *LedgerService) purchaseLines(db *gorm.DB, entityID uuid.UUID, in []PurchaseLine) ([]models.ExpenseLine, int, error) {
	if len(in) > maxLinesPerSale {
		return nil, 0, &InvalidError{"demasiados renglones"}
	}
	out := make([]models.ExpenseLine, 0, len(in))
	sum := 0
	for _, l := range in {
		if l.Qty <= 0 || l.Qty > maxQty || math.IsNaN(l.Qty) {
			return nil, 0, &InvalidError{"la cantidad de cada producto comprado debe ser mayor que cero"}
		}
		if l.UnitCost < 0 || l.UnitCost > 10_000_000 {
			return nil, 0, &InvalidError{"costo por unidad inválido"}
		}
		line := models.ExpenseLine{Qty: ledger.RoundQty(l.Qty), UnitCost: l.UnitCost, Name: cleanText(l.Name, 80)}
		switch {
		case l.ItemID != nil:
			var p models.ServiceItem
			if err := db.Select("id", "name", "kind", "bookable", "duration_min").Where("id = ? AND entity_id = ?", *l.ItemID, entityID).First(&p).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, 0, &InvalidError{"un producto comprado no existe en este negocio"}
				}
				return nil, 0, err
			}
			if p.IsService() {
				return nil, 0, &InvalidError{"«" + p.Name + "» es un servicio: no se compra para inventario."}
			}
			line.ItemID = &p.ID
			if line.Name == "" {
				line.Name = p.Name
			}
		case l.StockItemID != nil:
			var it models.StockItem
			if err := db.Where("id = ? AND entity_id = ? AND archived = false", *l.StockItemID, entityID).First(&it).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, 0, &InvalidError{"un insumo comprado no existe en este negocio"}
				}
				return nil, 0, err
			}
			line.StockItemID = &it.ID
			if line.Name == "" {
				line.Name = it.Name
			}
		default:
			return nil, 0, &InvalidError{"cada producto comprado necesita su referencia"}
		}
		if line.Name == "" {
			line.Name = "Producto"
		}
		sum += int(math.Round(line.Qty * float64(line.UnitCost)))
		out = append(out, line)
	}
	return out, sum, nil
}

// stockPurchase suma lo comprado: el costo del producto se actualiza (último costo) y las existencias suben si el
// producto lleva cantidad exacta y ya está contado; en semáforo vuelve a «hay».
func (s *LedgerService) stockPurchase(db *gorm.DB, entityID, expenseID uuid.UUID, at time.Time, lines []models.ExpenseLine) error {
	for _, l := range lines {
		if l.ItemID != nil && l.UnitCost > 0 {
			if err := db.Model(&models.ServiceItem{}).Where("id = ? AND entity_id = ?", *l.ItemID, entityID).UpdateColumn("cost", l.UnitCost).Error; err != nil {
				return err
			}
		}
		ref := uuid.Nil
		switch {
		case l.ItemID != nil:
			ref = *l.ItemID
		case l.StockItemID != nil:
			ref = *l.StockItemID
		}
		it, err := s.stockItem(db, entityID, ref, false, "", true)
		if errors.Is(err, ErrLedgerNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		switch it.Mode {
		case ledger.ModeLight:
			if it.Level != ledger.LevelOK {
				it.Level = ledger.LevelOK
				if err := db.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("level", it.Level).Error; err != nil {
					return err
				}
				if err := syncSoldOut(db, it); err != nil {
					return err
				}
			}
		case ledger.ModeExact:
			if it.CountedAt == nil {
				continue
			}
			if _, err := s.move(db, it, models.MovePurchase, l.Qty, models.ReasonPurchase, &expenseID, uuid.NewSHA1(expenseID, []byte("buy:"+it.ID.String())), at); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------- el inventario que ve el dueño ----------

type InventoryItem struct {
	Ref           uuid.UUID  `json:"ref"` // producto: su id de catálogo · insumo: su id propio
	ItemID        *uuid.UUID `json:"item_id,omitempty"`
	Kind          string     `json:"kind"`
	Name          string     `json:"name"`
	Section       string     `json:"section,omitempty"`
	Price         *int       `json:"price"`
	Cost          *int       `json:"cost"`
	Mode          string     `json:"mode"` // none | light | exact | unset (aún sin decidir)
	SuggestedMode string     `json:"suggested_mode"`
	Qty           float64    `json:"qty"`
	Level         string     `json:"level"`
	Status        string     `json:"status"` // ok | low | out | unknown | none
	Counted       bool       `json:"counted"`
	Unit          string     `json:"unit"`
	PackSize      *int       `json:"pack_size"`
	LowAt         *float64   `json:"low_at"`
	DaysLeft      *float64   `json:"days_left"`
	Sold28        float64    `json:"sold_28"`
	IsService     bool       `json:"is_service"`
	// Ingredients: los insumos que usa este plato o producto. UsedIn: los platos que usan este insumo.
	Ingredients []NameStatus `json:"ingredients"`
	UsedIn      []NameStatus `json:"used_in"`
	// StockID: el id de su existencia (si la tiene). AlsoSupply: producto que también se usa como insumo.
	StockID    *uuid.UUID `json:"stock_id,omitempty"`
	AlsoSupply bool       `json:"also_supply"`
	// Group: el grupo del insumo (vacío = sin grupo). Un producto que también se usa toma su sección.
	Group string `json:"group,omitempty"`
	// Servings: para cuántas unidades alcanzan sus insumos (solo si todos tienen cantidad y se cuentan exacto).
	// Limit: el insumo que pone ese límite.
	Servings *float64 `json:"servings"`
	Limit    string   `json:"limit,omitempty"`
	// Variantes: ParentID/VariantLabel en una variante; Variants = cuántas tiene un producto (que entonces agrupa).
	ParentID     *uuid.UUID `json:"parent_id,omitempty"`
	VariantLabel string     `json:"variant_label,omitempty"`
	Variants     int        `json:"variants"`
	// LastPurchase: cuántos llegaron en la última compra registrada (para «como la última vez»).
	LastPurchase *float64 `json:"last_purchase,omitempty"`
}

// NameStatus es un insumo o plato citado dentro de otro, con su estado.
type NameStatus struct {
	Ref    uuid.UUID `json:"ref"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	// Qty/Unit: cuánto gasta cada unidad vendida (solo en Ingredients, si el dueño lo puso).
	Qty  *float64 `json:"qty,omitempty"`
	Unit string   `json:"unit,omitempty"`
}

type InventoryOverview struct {
	Kind      string          `json:"kind"`     // shop | valuable | food | service | other
	Category  string          `json:"category"` // nombre de la categoría del negocio
	Mode      string          `json:"mode"`     // control recomendado para los productos
	QuickSale bool            `json:"quick_sale"`
	Unset     int             `json:"unset"` // productos sin decidir
	Explain   string          `json:"explain"`
	Items     []InventoryItem `json:"items"`
}

func explain(kind string) string {
	switch kind {
	case ledger.KindShop:
		return "Para tu negocio te recomendamos el semáforo: marcas si hay, queda poco o se acabó, sin contar uno por uno. Lo caro o lo que tienes en pocas unidades se cuenta exacto."
	case ledger.KindValuable:
		return "Para tu negocio te recomendamos contar la cantidad exacta de cada producto: son pocas unidades y así sabes siempre qué tienes y cuándo comprar."
	case ledger.KindFood:
		return "En un restaurante los platos se cuentan al venderlos, no al guardarlos. Lo que sí puedes controlar es lo que se agota: bebidas empacadas e insumos, con el semáforo."
	case ledger.KindService:
		return "Tus servicios no llevan inventario. Los productos que vendes (shampoo, cremas, accesorios) se cuentan exactos, porque son pocas unidades."
	}
	return "Te recomendamos empezar con el semáforo, que es lo más simple: hay, queda poco o se acabó. Después puedes contar exacto lo que quieras."
}

// sold28 devuelve lo vendido por producto en las últimas 4 semanas.
func (s *LedgerService) sold28(entityID uuid.UUID) (map[uuid.UUID]float64, error) {
	type row struct {
		ItemID uuid.UUID
		Qty    float64
	}
	var rows []row
	since := nowCO().AddDate(0, 0, -28)
	err := s.db.Table("sale_lines").Select("sale_lines.item_id AS item_id, COALESCE(SUM(sale_lines.qty),0) AS qty").
		Joins("JOIN sales ON sales.id = sale_lines.sale_id").
		Where("sales.entity_id = ? AND sales.status = ? AND sales.occurred_at >= ? AND sale_lines.item_id IS NOT NULL", entityID, models.LedgerActive, since).
		Group("sale_lines.item_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]float64, len(rows))
	for _, r := range rows {
		out[r.ItemID] = r.Qty
	}
	return out, nil
}

func (s *LedgerService) inventory(entityID uuid.UUID) (InventoryOverview, error) {
	prof, cat := s.profile(entityID)
	ov := InventoryOverview{Kind: prof.Kind, Category: cat, Mode: prof.Mode, QuickSale: prof.QuickSale, Explain: explain(prof.Kind), Items: []InventoryItem{}}

	var products []models.ServiceItem
	if err := s.db.Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).Order("position, name").Find(&products).Error; err != nil {
		return ov, err
	}
	var rows []models.StockItem
	if err := s.db.Where("entity_id = ? AND archived = false", entityID).Find(&rows).Error; err != nil {
		return ov, err
	}
	byItem := map[uuid.UUID]models.StockItem{}
	var supplies []models.StockItem
	for _, r := range rows {
		if r.ServiceItemID != nil {
			byItem[*r.ServiceItemID] = r
		} else {
			supplies = append(supplies, r)
		}
	}
	sold, err := s.sold28(entityID)
	if err != nil {
		return ov, err
	}

	build := func(ref uuid.UUID, itemID *uuid.UUID, kind, name, section string, price, cost *int, isService bool, st *models.StockItem, suggested string) InventoryItem {
		it := InventoryItem{Ref: ref, ItemID: itemID, Kind: kind, Name: name, Section: section, Price: price, Cost: cost, IsService: isService,
			Mode: "unset", SuggestedMode: suggested, Level: ledger.LevelOK, Status: ledger.StatusNone, Unit: "unidad"}
		if itemID != nil {
			it.Sold28 = sold[*itemID]
		}
		if st != nil {
			it.Mode, it.Qty, it.Level, it.Unit, it.PackSize, it.LowAt = st.Mode, st.Qty, st.Level, st.Unit, st.PackSize, st.LowAt
			sid := st.ID
			it.StockID, it.AlsoSupply = &sid, st.AlsoSupply
			it.Group = st.Group
			if st.ServiceItemID != nil {
				it.Group = section
			}
			it.Counted = st.CountedAt != nil
			it.Status = ledger.Status(st.Mode, it.Counted, st.Qty, st.Level, st.LowAt, it.Sold28)
			if st.Mode == ledger.ModeExact && it.Counted {
				if d, ok := ledger.DaysLeft(st.Qty, it.Sold28); ok {
					d = math.Round(d*10) / 10
					it.DaysLeft = &d
				}
			}
		}
		return it
	}

	byID, children := variantInfo(products)
	for _, p := range products {
		p.Price, p.Cost = effectivePrice(p, byID), effectiveCost(p, byID)
		if p.ParentID != nil && p.Section == "" {
			if par, ok := byID[*p.ParentID]; ok {
				p.Section = par.Section
			}
		}
		facts := itemFacts(p, nil)
		var st *models.StockItem
		if r, ok := byItem[p.ID]; ok {
			st = &r
		}
		if facts.IsService {
			st = nil // un servicio no lleva inventario (si tuvo, quedó archivado)
		}
		it := build(p.ID, &p.ID, models.StockProduct, p.Name, p.Section, p.Price, p.Cost, facts.IsService, st, ledger.RecommendMode(prof, facts))
		if facts.IsService {
			it.Mode = ledger.ModeNone
		}
		it.ParentID, it.VariantLabel = p.ParentID, p.VariantLabel
		if n := children[p.ID]; n > 0 {
			// Un producto con variantes no se cuenta: agrupa (su estado lo da la app a partir de sus variantes).
			it.Variants, it.Mode, it.Status, it.Qty, it.StockID = n, ledger.ModeNone, ledger.StatusNone, 0, nil
		}
		if it.Mode == "unset" && !it.IsService {
			ov.Unset++
		}
		ov.Items = append(ov.Items, it)
	}
	for _, sp := range supplies {
		sp := sp
		ov.Items = append(ov.Items, build(sp.ID, nil, models.StockSupply, sp.Name, "", nil, nil, false, &sp, sp.Mode))
	}
	last, err := s.lastPurchases(entityID)
	if err != nil {
		return ov, err
	}
	for i := range ov.Items {
		if q, ok := last[ov.Items[i].Ref]; ok {
			q := q
			ov.Items[i].LastPurchase = &q
		}
	}
	var links []models.ProductSupply
	if err := s.db.Where("entity_id = ?", entityID).Find(&links).Error; err != nil {
		return ov, err
	}
	idx := map[uuid.UUID]int{}      // por ítem del catálogo (o insumo)
	stockIdx := map[uuid.UUID]int{} // por existencia
	for i, it := range ov.Items {
		idx[it.Ref] = i
		if it.StockID != nil {
			stockIdx[*it.StockID] = i
		}
	}
	for i := range ov.Items {
		ov.Items[i].Ingredients = []NameStatus{}
		ov.Items[i].UsedIn = []NameStatus{}
	}
	for _, l := range links {
		pi, ok1 := idx[l.ProductID]
		si, ok2 := stockIdx[l.SupplyID]
		if !ok1 || !ok2 {
			continue
		}
		ov.Items[pi].Ingredients = append(ov.Items[pi].Ingredients, NameStatus{Ref: ov.Items[si].Ref, Name: ov.Items[si].Name, Status: ov.Items[si].Status, Qty: l.QtyPerUnit, Unit: ov.Items[si].Unit})
		ov.Items[si].UsedIn = append(ov.Items[si].UsedIn, NameStatus{Ref: l.ProductID, Name: ov.Items[pi].Name, Status: ov.Items[pi].Status})
	}
	// Para cuántas unidades alcanzan los insumos: el mínimo entre ellos, solo si TODOS tienen cantidad y conteo.
	for i := range ov.Items {
		it := &ov.Items[i]
		if len(it.Ingredients) == 0 {
			continue
		}
		var best *float64
		limit := ""
		for _, g := range it.Ingredients {
			si, ok := idx[g.Ref]
			if !ok || g.Qty == nil {
				best = nil
				break
			}
			sup := ov.Items[si]
			if sup.Mode != ledger.ModeExact || !sup.Counted {
				best = nil
				break
			}
			n := math.Floor(math.Max(0, sup.Qty) / *g.Qty)
			if best == nil || n < *best {
				v := n
				best, limit = &v, sup.Name
			}
		}
		if best != nil {
			it.Servings, it.Limit = best, limit
		}
	}
	return ov, nil
}

// SetIngredients reemplaza los insumos que usa un producto o plato. Solo acepta insumos activos del mismo negocio.
func (s *LedgerService) SetIngredients(ownerID, entityID, productID uuid.UUID, supplies []uuid.UUID) (InventoryOverview, error) {
	in := make([]IngredientInput, 0, len(supplies))
	for _, r := range supplies {
		in = append(in, IngredientInput{Ref: r})
	}
	return s.SetRecipe(ownerID, entityID, productID, in)
}

// IngredientInput es un insumo de un ítem y, opcional, cuánto gasta cada unidad vendida.
type IngredientInput struct {
	Ref uuid.UUID `json:"ref"`
	Qty *float64  `json:"qty"`
}

// SetRecipe reemplaza los insumos de un ítem, con cantidad por unidad donde el dueño la puso (fase 5).
func (s *LedgerService) SetRecipe(ownerID, entityID, productID uuid.UUID, items []IngredientInput) (InventoryOverview, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryOverview{}, err
	}
	var n int64
	if err := s.db.Model(&models.ServiceItem{}).Where("id = ? AND entity_id = ? AND status = ?", productID, entityID, models.ServiceActive).Count(&n).Error; err != nil {
		return InventoryOverview{}, err
	}
	if n == 0 {
		return InventoryOverview{}, ErrLedgerNotFound
	}
	// Cada referencia es un insumo (id de su existencia) o un producto marcado «también lo uso» (id del producto o de
	// su existencia). Se guarda siempre el id de la existencia.
	uniq := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	qtys := map[uuid.UUID]*float64{}
	for _, in := range items {
		ref := in.Ref
		if in.Qty != nil && (*in.Qty <= 0 || *in.Qty > maxQty || math.IsNaN(*in.Qty)) {
			return InventoryOverview{}, &InvalidError{"La cantidad de cada insumo debe ser mayor que cero."}
		}
		var st models.StockItem
		if err := s.db.Where("entity_id = ? AND (id = ? OR service_item_id = ?) AND archived = false", entityID, ref, ref).First(&st).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return InventoryOverview{}, &InvalidError{"Alguno de esos insumos ya no existe."}
			}
			return InventoryOverview{}, err
		}
		if st.ServiceItemID != nil {
			if *st.ServiceItemID == productID {
				return InventoryOverview{}, &InvalidError{"Un ítem no puede usarse a sí mismo."}
			}
			if !st.AlsoSupply {
				return InventoryOverview{}, &InvalidError{"Ese producto no está marcado como «también lo uso»."}
			}
		}
		if !uniq[st.ID] {
			uniq[st.ID] = true
			ids = append(ids, st.ID)
		}
		if in.Qty != nil {
			q := ledger.RoundQty(*in.Qty)
			qtys[st.ID] = &q
		}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("entity_id = ? AND product_id = ?", entityID, productID).Delete(&models.ProductSupply{}).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Create(&models.ProductSupply{EntityID: entityID, ProductID: productID, SupplyID: id, QtyPerUnit: qtys[id]}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return InventoryOverview{}, err
	}
	return s.inventory(entityID)
}

// Inventory devuelve el inventario completo: productos (con o sin control) e insumos.
func (s *LedgerService) Inventory(ownerID, entityID uuid.UUID) (InventoryOverview, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryOverview{}, err
	}
	return s.inventory(entityID)
}

// SetupChoice: cómo arrancar el inventario de los productos que aún no decidieron.
const (
	SetupRecommended = "recommended"
	SetupSimple      = "simple"
	SetupNone        = "none"
)

// SetupInventory activa el inventario de los productos sin decidir con un solo toque: lo recomendado según el tipo de
// negocio, «lo más simple» (semáforo en todo lo que no es servicio) o sin control.
func (s *LedgerService) SetupInventory(ownerID, entityID uuid.UUID, choice string) (InventoryOverview, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryOverview{}, err
	}
	if choice != SetupRecommended && choice != SetupSimple && choice != SetupNone {
		return InventoryOverview{}, &InvalidError{"choice debe ser recommended, simple o none"}
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return ov, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for _, it := range ov.Items {
			if it.Mode != "unset" || it.ItemID == nil || it.IsService {
				continue
			}
			mode := it.SuggestedMode
			switch choice {
			case SetupSimple:
				mode = ledger.ModeLight
				if it.IsService {
					mode = ledger.ModeNone
				}
			case SetupNone:
				mode = ledger.ModeNone
			}
			row := models.StockItem{ID: uuid.New(), EntityID: entityID, ServiceItemID: it.ItemID, Kind: models.StockProduct, Mode: mode, Level: ledger.LevelOK, Unit: "unidad"}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "service_item_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ov, err
	}
	return s.inventory(entityID)
}

// ItemSettings: lo que el dueño puede cambiar de un ítem de inventario. Los campos nulos no se tocan.
type ItemSettings struct {
	Mode     *string  `json:"mode"`
	Unit     *string  `json:"unit"`
	PackSize *int     `json:"pack_size"`
	LowAt    *float64 `json:"low_at"`
	// ClearPack/ClearLowAt: borrar el valor (un nulo en JSON no distingue «no tocar» de «borrar»).
	ClearPack  bool `json:"clear_pack"`
	ClearLowAt bool `json:"clear_low_at"`
	// AlsoSupply: «también lo uso en mis servicios o platos» (solo productos). Apagarlo lo quita de donde se usaba.
	AlsoSupply *bool `json:"also_supply"`
	// Group: el grupo de un insumo (solo insumos; "" lo deja sin grupo).
	Group *string `json:"group"`
}

func (s *LedgerService) UpdateStockSettings(ownerID, entityID, ref uuid.UUID, in ItemSettings) (InventoryOverview, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryOverview{}, err
	}
	if in.Mode != nil && !ledger.ValidMode(*in.Mode) {
		return InventoryOverview{}, &InvalidError{"modo inválido"}
	}
	if in.PackSize != nil && (*in.PackSize < 1 || *in.PackSize > 10000) {
		return InventoryOverview{}, &InvalidError{"el contenido de la paca debe estar entre 1 y 10.000"}
	}
	if in.LowAt != nil && (*in.LowAt < 0 || *in.LowAt > maxQty) {
		return InventoryOverview{}, &InvalidError{"umbral inválido"}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		mode := ""
		if in.Mode != nil {
			mode = *in.Mode
		}
		it, err := s.stockItem(tx, entityID, ref, true, mode, true)
		if err != nil {
			return err
		}
		upd := map[string]any{}
		if in.Mode != nil && *in.Mode != it.Mode {
			it.Mode = *in.Mode
			upd["mode"] = it.Mode
			if it.Mode == ledger.ModeLight {
				it.Level = ledger.LevelOK
				upd["level"] = it.Level
			}
		}
		if in.Unit != nil {
			it.Unit = cleanText(*in.Unit, 20)
			if it.Unit == "" {
				it.Unit = "unidad"
			}
			upd["unit"] = it.Unit
		}
		if in.PackSize != nil {
			upd["pack_size"] = *in.PackSize
		} else if in.ClearPack {
			upd["pack_size"] = nil
		}
		if in.LowAt != nil {
			upd["low_at"] = *in.LowAt
		} else if in.ClearLowAt {
			upd["low_at"] = nil
		}
		if in.Group != nil {
			if it.ServiceItemID != nil {
				return &InvalidError{"un producto se agrupa por su sección, no por grupo"}
			}
			upd["supply_group"] = cleanGroup(*in.Group)
		}
		if in.AlsoSupply != nil && it.ServiceItemID != nil && *in.AlsoSupply != it.AlsoSupply {
			upd["also_supply"] = *in.AlsoSupply
			if !*in.AlsoSupply {
				if err := tx.Where("entity_id = ? AND supply_id = ?", entityID, it.ID).Delete(&models.ProductSupply{}).Error; err != nil {
					return err
				}
			}
		}
		if len(upd) > 0 {
			if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Updates(upd).Error; err != nil {
				return err
			}
		}
		return syncSoldOut(tx, it)
	})
	if err != nil {
		return InventoryOverview{}, err
	}
	return s.inventory(entityID)
}

// SetLevel marca el semáforo de un producto: hay, queda poco o se acabó. Un producto con cantidad exacta solo acepta
// «se acabó» (queda en cero).
func (s *LedgerService) SetLevel(ownerID, entityID, ref uuid.UUID, level string, clientID uuid.UUID) error {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return err
	}
	return s.setLevel(s.db, entityID, ref, level, clientID)
}

func (s *LedgerService) setLevel(db *gorm.DB, entityID, ref uuid.UUID, level string, clientID uuid.UUID) error {
	if !ledger.ValidLevel(level) {
		return &InvalidError{"nivel inválido: usa ok, low u out"}
	}
	run := func(tx *gorm.DB) error {
		it, err := s.stockItem(tx, entityID, ref, true, ledger.ModeLight, true)
		if err != nil {
			return err
		}
		if it.Mode == ledger.ModeNone {
			it.Mode = ledger.ModeLight
			if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("mode", it.Mode).Error; err != nil {
				return err
			}
		}
		if it.Mode == ledger.ModeExact {
			if level != ledger.LevelOut {
				return &InvalidError{"este producto lleva cantidad exacta: cuenta cuántos tienes"}
			}
			if clientID == uuid.Nil {
				clientID = uuid.New()
			}
			_, err := s.move(tx, it, models.MoveCount, -it.Qty, models.ReasonCount, nil, clientID, nowCO())
			if err != nil {
				return err
			}
			now := nowCO()
			it.CountedAt = &now
			if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("counted_at", now).Error; err != nil {
				return err
			}
			return syncSoldOut(tx, it)
		}
		it.Level = level
		if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("level", level).Error; err != nil {
			return err
		}
		return syncSoldOut(tx, it)
	}
	if db != s.db { // ya dentro de una transacción
		return run(db)
	}
	return s.db.Transaction(run)
}

type AdjustInput struct {
	ClientID uuid.UUID `json:"client_id"`
	Ref      uuid.UUID `json:"ref"`
	// Counted: «conté y hay tantos» (fija la cantidad). Delta: «se dañaron 2» (suma o resta, con motivo).
	Counted *float64 `json:"counted"`
	Delta   *float64 `json:"delta"`
	Reason  string   `json:"reason"`
}

// Adjust cuenta o ajusta las existencias de un producto. Contar activa la cantidad exacta del producto.
func (s *LedgerService) Adjust(ownerID, entityID uuid.UUID, in AdjustInput) (InventoryItem, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryItem{}, err
	}
	if err := s.adjust(s.db, entityID, in); err != nil {
		return InventoryItem{}, err
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return InventoryItem{}, err
	}
	for _, it := range ov.Items {
		if it.Ref == in.Ref || (it.ItemID != nil && *it.ItemID == in.Ref) {
			return it, nil
		}
	}
	return InventoryItem{}, ErrLedgerNotFound
}

func (s *LedgerService) adjust(db *gorm.DB, entityID uuid.UUID, in AdjustInput) error {
	if in.ClientID == uuid.Nil {
		return &InvalidError{"falta client_id"}
	}
	if (in.Counted == nil) == (in.Delta == nil) {
		return &InvalidError{"indica la cantidad contada o el ajuste, no las dos"}
	}
	if in.Counted != nil && (*in.Counted < 0 || *in.Counted > maxQty || math.IsNaN(*in.Counted)) {
		return &InvalidError{"cantidad inválida"}
	}
	if in.Delta != nil && (*in.Delta == 0 || math.Abs(*in.Delta) > maxQty || math.IsNaN(*in.Delta)) {
		return &InvalidError{"ajuste inválido"}
	}
	if in.Delta != nil && !models.ValidAdjustReason(in.Reason) {
		return &InvalidError{"elige el motivo del ajuste"}
	}
	run := func(tx *gorm.DB) error {
		it, err := s.stockItem(tx, entityID, in.Ref, true, ledger.ModeExact, true)
		if err != nil {
			return err
		}
		now := nowCO()
		if in.Counted != nil {
			if it.Mode != ledger.ModeExact {
				it.Mode = ledger.ModeExact // contar es pasar a cantidad exacta
				if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("mode", it.Mode).Error; err != nil {
					return err
				}
			}
			counted := ledger.RoundQty(*in.Counted)
			if _, err := s.move(tx, it, models.MoveCount, counted-it.Qty, models.ReasonCount, nil, in.ClientID, now); err != nil {
				return err
			}
			it.CountedAt = &now
			if err := tx.Model(&models.StockItem{}).Where("id = ?", it.ID).Update("counted_at", now).Error; err != nil {
				return err
			}
			return syncSoldOut(tx, it)
		}
		if it.Mode != ledger.ModeExact || it.CountedAt == nil {
			return &InvalidError{"primero cuenta cuántos tienes"}
		}
		if it.Qty+*in.Delta < 0 {
			return &InvalidError{fmt.Sprintf("no puedes quitar más de lo que hay (%g)", it.Qty)}
		}
		_, err = s.move(tx, it, models.MoveAdjust, *in.Delta, in.Reason, nil, in.ClientID, now)
		return err
	}
	if db != s.db {
		return run(db)
	}
	return s.db.Transaction(run)
}

// ---------- insumos ----------

type SupplyInput struct {
	ClientID uuid.UUID `json:"client_id"`
	Name     string    `json:"name"`
	Unit     string    `json:"unit"`
	Mode     string    `json:"mode"` // light (por defecto) o exact
	Qty      *float64  `json:"qty"`  // cuántos hay hoy (solo cantidad exacta)
	Group    string    `json:"group"` // opcional: Carnes, Verduras, Empaques…
}

// cleanGroup deja el grupo con mayúscula inicial y sin espacios de sobra, para que «carnes» y «Carnes » sean uno solo.
func cleanGroup(g string) string {
	g = cleanText(g, 40)
	if g == "" {
		return ""
	}
	r := []rune(g)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// CreateSupply agrega un insumo que no se vende (lechuga, harina, vasos). Si ya hay uno con ese nombre, lo devuelve.
func (s *LedgerService) CreateSupply(ownerID, entityID uuid.UUID, in SupplyInput) (models.StockItem, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.StockItem{}, err
	}
	if in.ClientID == uuid.Nil {
		return models.StockItem{}, &InvalidError{"falta client_id"}
	}
	name := cleanText(in.Name, 80)
	if name == "" {
		return models.StockItem{}, &InvalidError{"el nombre es obligatorio"}
	}
	mode := in.Mode
	if mode == "" {
		mode = ledger.ModeLight
	}
	if mode != ledger.ModeLight && mode != ledger.ModeExact {
		return models.StockItem{}, &InvalidError{"los insumos van en semáforo o cantidad exacta"}
	}
	if in.Qty != nil && (*in.Qty < 0 || *in.Qty > maxQty) {
		return models.StockItem{}, &InvalidError{"cantidad inválida"}
	}
	// Un insumo no puede repetir un producto del catálogo: quedaría un segundo registro suelto, sin ventas ni precio.
	var products []models.ServiceItem
	if err := s.db.Select("id", "name").Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).Find(&products).Error; err != nil {
		return models.StockItem{}, err
	}
	for _, p := range products {
		if ledger.Normalize(p.Name) == ledger.Normalize(name) {
			return models.StockItem{}, &InvalidError{"«" + p.Name + "» ya está en tus productos: búscalo en la lista del inventario."}
		}
	}
	var out models.StockItem
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing []models.StockItem
		if err := tx.Where("entity_id = ? AND service_item_id IS NULL", entityID).Find(&existing).Error; err != nil {
			return err
		}
		active := 0
		key := ledger.Normalize(name)
		for _, e := range existing {
			if e.ID == in.ClientID || (!e.Archived && ledger.Normalize(e.Name) == key) {
				out = e
				// Ya existía: si no tenía grupo y ahora lo traen, se lo pone.
				if g := cleanGroup(in.Group); g != "" && e.Group == "" {
					out.Group = g
					return tx.Model(&models.StockItem{}).Where("id = ?", e.ID).Update("supply_group", g).Error
				}
				return nil
			}
			if !e.Archived {
				active++
			}
		}
		if active >= maxSupplies {
			return &InvalidError{"llegaste al máximo de insumos"}
		}
		unit := cleanText(in.Unit, 20)
		if unit == "" {
			unit = "unidad"
		}
		out = models.StockItem{ID: in.ClientID, EntityID: entityID, Kind: models.StockSupply, Name: name, Mode: mode, Level: ledger.LevelOK, Unit: unit, Group: cleanGroup(in.Group)}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		if mode == ledger.ModeExact && in.Qty != nil {
			now := nowCO()
			if _, err := s.move(tx, &out, models.MoveCount, *in.Qty, models.ReasonInitial, nil, uuid.NewSHA1(in.ClientID, []byte("initial")), now); err != nil {
				return err
			}
			out.CountedAt = &now
			return tx.Model(&models.StockItem{}).Where("id = ?", out.ID).Update("counted_at", now).Error
		}
		return nil
	})
	return out, err
}

// ArchiveSupply quita un insumo de la lista (no se borra: conserva su historia).
func (s *LedgerService) ArchiveSupply(ownerID, entityID, id uuid.UUID) error {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return err
	}
	res := s.db.Model(&models.StockItem{}).Where("id = ? AND entity_id = ? AND service_item_id IS NULL", id, entityID).Update("archived", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLedgerNotFound
	}
	return s.db.Where("entity_id = ? AND supply_id = ?", entityID, id).Delete(&models.ProductSupply{}).Error
}

// ---------- avisos (inteligencia de negocios sencilla) ----------

type Insight struct {
	Kind     string     `json:"kind"`     // soon | out | low | idle | margin | loss | setup
	Severity string     `json:"severity"` // high | medium | low
	Title    string     `json:"title"`
	Body     string     `json:"body"`
	Ref      *uuid.UUID `json:"ref,omitempty"` // el producto o insumo al que se refiere
}

func fmtQty(q float64) string {
	if q == math.Trunc(q) {
		return fmt.Sprintf("%d", int(q))
	}
	return fmt.Sprintf("%.1f", q)
}

func pesos(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += "."
		}
		out += string(c)
	}
	if neg {
		return "-$" + out
	}
	return "$" + out
}

// Insights arma los avisos que le sirven al dueño hoy, con reglas simples y explicables (sin gráficas): lo que se
// acaba, lo agotado, lo que no rota, lo que deja poco margen y lo perdido por daño o vencimiento.
func (s *LedgerService) Insights(ownerID, entityID uuid.UUID) ([]Insight, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return nil, err
	}
	var out []Insight
	add := func(i Insight) { out = append(out, i) }

	for _, it := range ov.Items {
		ref := it.Ref
		unit := it.Unit
		switch it.Status {
		case ledger.StatusOut:
			sev := "medium"
			if it.Sold28 > 0 {
				sev = "high"
			}
			body := "Lo marcamos como agotado en tu perfil de Empre. Cuando lo compres, vuelve solo."
			if it.Kind == models.StockSupply {
				body = "Cuando lo compres, vuelve a estar disponible."
				sev = "medium"
			}
			if len(it.UsedIn) > 0 {
				body = "Afecta a: " + joinNames(it.UsedIn) + ". " + body
				sev = "high"
			}
			add(Insight{Kind: "out", Severity: sev, Title: it.Name + ": se acabó", Body: body, Ref: &ref})
		case ledger.StatusLow:
			if it.Mode == ledger.ModeExact && it.DaysLeft != nil {
				add(Insight{Kind: "soon", Severity: "high", Title: it.Name + ": se te acaba pronto",
					Body: fmt.Sprintf("Te quedan %s %s y vendes unas %s al día: alcanza para unos %s días.", fmtQty(it.Qty), unit, fmtQty(math.Max(1, math.Round(it.Sold28/28))), fmtQty(math.Max(0, math.Floor(*it.DaysLeft)))), Ref: &ref})
			} else {
				body := "Ve pensando en comprar más."
				if len(it.UsedIn) > 0 {
					body = "Lo usan: " + joinNames(it.UsedIn) + ". " + body
				}
				add(Insight{Kind: "low", Severity: "medium", Title: it.Name + ": queda poco", Body: body, Ref: &ref})
			}
		}
		if it.Servings != nil {
			perDay := math.Max(1, math.Round(it.Sold28/28))
			if *it.Servings < perDay {
				sev := "medium"
				if *it.Servings == 0 {
					sev = "high"
				}
				add(Insight{Kind: "servings", Severity: sev, Title: fmt.Sprintf("%s: alcanza para %s", it.Name, fmtQty(*it.Servings)),
					Body: fmt.Sprintf("Lo que limita es %s. Vendes unas %s al día.", it.Limit, fmtQty(perDay)), Ref: &ref})
			}
		}
		if it.Mode == ledger.ModeExact && it.Counted && it.Cost != nil && it.Qty > 0 && it.Sold28 == 0 {
			if v := int(it.Qty * float64(*it.Cost)); v >= 100_000 {
				add(Insight{Kind: "idle", Severity: "low", Title: "Plata quieta en " + it.Name,
					Body: fmt.Sprintf("Tienes %s en %s y no se ha vendido en 4 semanas. Una oferta en Empre podría moverlo.", pesos(v), it.Name), Ref: &ref})
			}
		}
		if it.Price != nil && it.Cost != nil && *it.Price > 0 && *it.Cost > 0 && it.Sold28 > 0 {
			if m := float64(*it.Price-*it.Cost) / float64(*it.Price) * 100; m < 15 {
				add(Insight{Kind: "margin", Severity: "medium", Title: fmt.Sprintf("%s te deja solo %d %%", it.Name, int(math.Round(m))),
					Body: "Cuesta " + pesos(*it.Cost) + " y lo vendes en " + pesos(*it.Price) + ". Revisa el precio o a quién se lo compras.", Ref: &ref})
			}
		}
	}

	// Pérdidas del mes por daño, vencimiento o pérdida.
	type loss struct {
		Qty  float64
		Cost *int
	}
	var rows []struct {
		StockItemID uuid.UUID
		Qty         float64
	}
	since := nowCO().AddDate(0, 0, -30)
	if err := s.db.Table("stock_movements").Select("stock_item_id, COALESCE(SUM(-delta),0) AS qty").
		Where("entity_id = ? AND kind = ? AND reason IN ? AND delta < 0 AND occurred_at >= ?", entityID, models.MoveAdjust, []string{models.ReasonDamaged, models.ReasonExpired, models.ReasonLost}, since).
		Group("stock_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	total := 0
	costOf := map[uuid.UUID]*int{}
	for _, it := range ov.Items {
		// Los movimientos van por existencia (StockID), no por id del catálogo.
		if it.StockID != nil {
			costOf[*it.StockID] = it.Cost
		}
	}
	for _, r := range rows {
		if c := costOf[r.StockItemID]; c != nil {
			total += int(r.Qty * float64(*c))
		}
	}
	if total >= 20_000 {
		add(Insight{Kind: "loss", Severity: "medium", Title: "Este mes perdiste " + pesos(total), Body: "Es lo que se dañó, venció o se perdió. Revisa cuánto compras de esos productos."})
	}

	if n, err := s.staleTabs(entityID); err == nil && n > 0 {
		title := "1 cuenta sigue abierta"
		if n > 1 {
			title = fmt.Sprintf("%d cuentas siguen abiertas", n)
		}
		add(Insight{Kind: "tabs", Severity: "medium", Title: title, Body: "Cóbralas, pásalas a fiado o cancélalas para que tus ventas del día cuadren."})
	}
	if ov.Unset > 0 {
		add(Insight{Kind: "setup", Severity: "low", Title: fmt.Sprintf("%d productos sin inventario", ov.Unset), Body: "Actívalo con un toque, según el tipo de tu negocio."})
	}

	rank := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Severity] < rank[out[j].Severity] })
	if len(out) > insightsLimit {
		out = out[:insightsLimit]
	}
	if out == nil {
		out = []Insight{}
	}
	return out, nil
}

func joinNames(list []NameStatus) string {
	names := make([]string, 0, len(list))
	for _, n := range list {
		names = append(names, n.Name)
	}
	if len(names) > 3 {
		return strings.Join(names[:3], ", ") + " y " + fmt.Sprintf("%d más", len(names)-3)
	}
	return strings.Join(names, ", ")
}
