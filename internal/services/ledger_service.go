package services

import (
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Administración del negocio: ventas, gastos, fiados y abonos. Reglas que no se negocian:
//   - todo lo que viene del teléfono trae client_id: reenviar no duplica (la respuesta devuelve lo ya guardado).
//     El client_id es también el id del registro: así el teléfono puede referirse a un cliente o a una venta que
//     todavía no se ha enviado (fiar a un cliente nuevo sin señal, deshacer una venta);
//   - nada se borra: se anula con motivo y deja de contar;
//   - los montos son pesos enteros y se validan aquí, no solo en la app.

var (
	ErrLedgerNotFound  = errors.New("ledger_not_found")
	ErrLedgerForbidden = errors.New("forbidden")
)

const (
	maxLedgerAmount   = 100_000_000
	maxBackdateDays   = 400
	futureTolerance   = 10 * time.Minute
	maxLinesPerSale   = 60
	maxListLimit      = 500
	defaultListLimit  = 100
	maxDebtAmountSlop = 0 // un abono no puede superar lo que se debe
)

var expenseCategories = map[string]bool{
	"mercancia": true, "arriendo": true, "servicios": true, "nomina": true, "transporte": true,
	"impuestos": true, "mantenimiento": true, "publicidad": true, "otro": true,
}

// ExpenseCategories lista las categorías válidas, para que la app y la voz usen las mismas.
func ExpenseCategories() []string {
	out := make([]string, 0, len(expenseCategories))
	for k := range expenseCategories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type LedgerService struct {
	db    *gorm.DB
	model VoiceModel
	// VoiceMonthlyLimit son los dictados que se pueden interpretar por negocio y mes (la IA cuesta).
	VoiceMonthlyLimit int
}

// SetVoiceModel conecta el modelo de IA que interpreta el dictado. Sin él, la voz responde ErrVoiceDisabled.
func (s *LedgerService) SetVoiceModel(m VoiceModel) { s.model = m }

func NewLedgerService(db *gorm.DB) *LedgerService {
	return &LedgerService{db: db, VoiceMonthlyLimit: 1000}
}

func (s *LedgerService) requireOwner(ownerID, entityID uuid.UUID) error {
	var e models.Entity
	if err := s.db.Select("id", "owner_id").First(&e, "id = ?", entityID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLedgerNotFound
		}
		return err
	}
	if e.OwnerID != ownerID {
		return ErrLedgerForbidden
	}
	return nil
}

// resolveTime valida la fecha de un registro: por defecto ahora; no en el futuro; no más de ~13 meses atrás.
func resolveTime(at *time.Time) (time.Time, error) {
	now := nowCO()
	if at == nil || at.IsZero() {
		return now, nil
	}
	t := at.In(now.Location())
	if t.After(now.Add(futureTolerance)) {
		return time.Time{}, &InvalidError{"la fecha no puede estar en el futuro"}
	}
	if t.Before(now.AddDate(0, 0, -maxBackdateDays)) {
		return time.Time{}, &InvalidError{"la fecha es demasiado antigua"}
	}
	if t.After(now) {
		t = now
	}
	return t, nil
}

func checkAmount(n int, what string) error {
	if n <= 0 {
		return &InvalidError{what + " debe ser mayor que cero"}
	}
	if n > maxLedgerAmount {
		return &InvalidError{what + " es demasiado grande"}
	}
	return nil
}

func cleanText(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

// ---------- terceros ----------

type PartyInput struct {
	ClientID    uuid.UUID `json:"client_id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Phone       string    `json:"phone"`
	CreditLimit *int      `json:"credit_limit"`
}

// PartyBalance es un tercero con lo que se debe. Para un cliente es lo que él debe al negocio; para un proveedor,
// lo que el negocio le debe a él.
type PartyBalance struct {
	models.Party
	Balance int `json:"balance"`
}

func (s *LedgerService) CreateParty(ownerID, entityID uuid.UUID, in PartyInput) (models.Party, bool, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.Party{}, false, err
	}
	return s.createParty(s.db, entityID, in)
}

func (s *LedgerService) createParty(db *gorm.DB, entityID uuid.UUID, in PartyInput) (models.Party, bool, error) {
	if in.ClientID == uuid.Nil {
		return models.Party{}, false, &InvalidError{"falta client_id"}
	}
	kind := in.Kind
	if kind == "" {
		kind = models.PartyCustomer
	}
	if kind != models.PartyCustomer && kind != models.PartySupplier {
		return models.Party{}, false, &InvalidError{"kind debe ser customer o supplier"}
	}
	name := cleanText(in.Name, 80)
	if name == "" {
		return models.Party{}, false, &InvalidError{"el nombre es obligatorio"}
	}
	if in.CreditLimit != nil && (*in.CreditLimit < 0 || *in.CreditLimit > maxLedgerAmount) {
		return models.Party{}, false, &InvalidError{"cupo de fiado inválido"}
	}
	p := models.Party{
		ID: in.ClientID, EntityID: entityID, ClientID: in.ClientID, Kind: kind, Name: name,
		NameKey: ledger.Normalize(name), Phone: cleanText(in.Phone, 20), CreditLimit: in.CreditLimit,
	}
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "client_id"}}, DoNothing: true}).Create(&p)
	if res.Error != nil {
		return models.Party{}, false, res.Error
	}
	if res.RowsAffected == 0 {
		var old models.Party
		if err := db.Where("entity_id = ? AND client_id = ?", entityID, in.ClientID).First(&old).Error; err != nil {
			return models.Party{}, false, err
		}
		return old, false, nil
	}
	return p, true, nil
}

func (s *LedgerService) party(entityID, partyID uuid.UUID, kind string) (models.Party, error) {
	var p models.Party
	err := s.db.Where("id = ? AND entity_id = ?", partyID, entityID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, &InvalidError{"ese cliente o proveedor no existe en este negocio"}
	}
	if err != nil {
		return p, err
	}
	if kind != "" && p.Kind != kind {
		return p, &InvalidError{"el tercero no es del tipo esperado"}
	}
	return p, nil
}

// balances devuelve lo que se debe por tercero (solo registros activos).
func (s *LedgerService) balances(entityID uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	type row struct {
		PartyID uuid.UUID
		Sum     int
	}
	var credit []row
	if err := s.db.Model(&models.Sale{}).Select("party_id, COALESCE(SUM(total),0) AS sum").
		Where("entity_id = ? AND status = ? AND method = ? AND party_id IS NOT NULL", entityID, models.LedgerActive, models.PayCredit).
		Group("party_id").Scan(&credit).Error; err != nil {
		return nil, err
	}
	for _, r := range credit {
		out[r.PartyID] += r.Sum
	}
	var buys []row
	if err := s.db.Model(&models.Expense{}).Select("party_id, COALESCE(SUM(amount),0) AS sum").
		Where("entity_id = ? AND status = ? AND method = ? AND party_id IS NOT NULL", entityID, models.LedgerActive, models.PayCredit).
		Group("party_id").Scan(&buys).Error; err != nil {
		return nil, err
	}
	for _, r := range buys {
		out[r.PartyID] += r.Sum
	}
	var pays []row
	if err := s.db.Model(&models.DebtPayment{}).Select("party_id, COALESCE(SUM(amount),0) AS sum").
		Where("entity_id = ?", entityID).Group("party_id").Scan(&pays).Error; err != nil {
		return nil, err
	}
	for _, r := range pays {
		out[r.PartyID] -= r.Sum
	}
	return out, nil
}

// ListParties lista terceros con su saldo. kind vacío = todos. onlyOwing=true oculta a los que no deben nada.
func (s *LedgerService) ListParties(ownerID, entityID uuid.UUID, kind string, onlyOwing bool) ([]PartyBalance, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	q := s.db.Where("entity_id = ?", entityID)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var parties []models.Party
	if err := q.Order("name_key").Find(&parties).Error; err != nil {
		return nil, err
	}
	bal, err := s.balances(entityID)
	if err != nil {
		return nil, err
	}
	out := make([]PartyBalance, 0, len(parties))
	for _, p := range parties {
		b := bal[p.ID]
		if onlyOwing && b <= 0 {
			continue
		}
		out = append(out, PartyBalance{Party: p, Balance: b})
	}
	if onlyOwing {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Balance > out[j].Balance })
	}
	return out, nil
}

// ---------- ventas ----------

type LineInput struct {
	ItemID    *uuid.UUID `json:"item_id"`
	Name      string     `json:"name"`
	Qty       float64    `json:"qty"`
	UnitPrice *int       `json:"unit_price"`
}

type SaleInput struct {
	ClientID   uuid.UUID   `json:"client_id"`
	OccurredAt *time.Time  `json:"occurred_at"`
	Total      *int        `json:"total"`
	Method     string      `json:"method"`
	PartyID    *uuid.UUID  `json:"party_id"`
	Note       string      `json:"note"`
	Lines      []LineInput `json:"lines"`
	Source     string      `json:"source"`
}

func validSource(src string) (string, error) {
	switch src {
	case "", models.SourceManual:
		return models.SourceManual, nil
	case models.SourceVoice:
		return models.SourceVoice, nil
	}
	return "", &InvalidError{"origen no permitido"}
}

func (s *LedgerService) CreateSale(ownerID, entityID uuid.UUID, in SaleInput) (models.Sale, bool, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.Sale{}, false, err
	}
	return s.createSale(entityID, in, nil)
}

// createSale es el cuerpo de CreateSale; tx opcional permite usarlo dentro de otra transacción (confirmar voz).
func (s *LedgerService) createSale(entityID uuid.UUID, in SaleInput, tx *gorm.DB) (models.Sale, bool, error) {
	db := s.db
	if tx != nil {
		db = tx
	}
	if in.ClientID == uuid.Nil {
		return models.Sale{}, false, &InvalidError{"falta client_id"}
	}
	if !models.ValidPayMethod(in.Method) {
		return models.Sale{}, false, &InvalidError{"medio de pago inválido"}
	}
	src, err := validSource(in.Source)
	if err != nil {
		return models.Sale{}, false, err
	}
	at, err := resolveTime(in.OccurredAt)
	if err != nil {
		return models.Sale{}, false, err
	}
	if len(in.Lines) > maxLinesPerSale {
		return models.Sale{}, false, &InvalidError{"demasiados renglones"}
	}

	lines := make([]models.SaleLine, 0, len(in.Lines))
	sum := 0
	for _, l := range in.Lines {
		if l.Qty <= 0 || l.Qty > 100000 || math.IsNaN(l.Qty) {
			return models.Sale{}, false, &InvalidError{"la cantidad de cada renglón debe ser mayor que cero"}
		}
		line := models.SaleLine{Qty: math.Round(l.Qty*1000) / 1000, Name: cleanText(l.Name, 80)}
		if l.ItemID != nil {
			var it models.ServiceItem
			if err := db.Where("id = ? AND entity_id = ?", *l.ItemID, entityID).First(&it).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return models.Sale{}, false, &InvalidError{"un producto del renglón no existe en este negocio"}
				}
				return models.Sale{}, false, err
			}
			if many, err := hasVariants(db, it.ID); err != nil {
				return models.Sale{}, false, err
			} else if many {
				return models.Sale{}, false, &InvalidError{"«" + it.Name + "» tiene variantes: elige cuál se vendió"}
			}
			if it.ParentID != nil && (it.Price == nil || it.Cost == nil) {
				var parent models.ServiceItem
				if err := db.Select("id", "price", "cost").First(&parent, "id = ?", *it.ParentID).Error; err == nil {
					byID := map[uuid.UUID]models.ServiceItem{parent.ID: parent}
					it.Price, it.Cost = effectivePrice(it, byID), effectiveCost(it, byID)
				}
			}
			line.ItemID = &it.ID
			if line.Name == "" {
				line.Name = it.Name
			}
			line.UnitCost = it.Cost
			if l.UnitPrice != nil {
				line.UnitPrice = *l.UnitPrice
			} else if it.Price != nil {
				line.UnitPrice = *it.Price
			} else {
				return models.Sale{}, false, &InvalidError{"«" + it.Name + "» no tiene precio: escríbelo"}
			}
		} else {
			if line.Name == "" {
				return models.Sale{}, false, &InvalidError{"cada renglón necesita nombre"}
			}
			if l.UnitPrice == nil {
				return models.Sale{}, false, &InvalidError{"cada renglón necesita precio"}
			}
			line.UnitPrice = *l.UnitPrice
		}
		if line.UnitPrice < 0 || line.UnitPrice > maxLedgerAmount {
			return models.Sale{}, false, &InvalidError{"precio de renglón inválido"}
		}
		sum += int(math.Round(line.Qty * float64(line.UnitPrice)))
		lines = append(lines, line)
	}
	total := sum
	if in.Total != nil {
		total = *in.Total // el total manda: permite descuentos y redondeos
	} else if len(lines) == 0 {
		return models.Sale{}, false, &InvalidError{"falta el total de la venta"}
	}
	if err := checkAmount(total, "el total"); err != nil {
		return models.Sale{}, false, err
	}
	if sum > 0 && total > sum*3 {
		return models.Sale{}, false, &InvalidError{"el total no cuadra con los renglones"}
	}

	if in.Method == models.PayCredit {
		if in.PartyID == nil {
			return models.Sale{}, false, &InvalidError{"un fiado necesita el cliente"}
		}
	}
	if in.PartyID != nil {
		want := ""
		if in.Method == models.PayCredit {
			want = models.PartyCustomer
		}
		var p models.Party
		err := db.Where("id = ? AND entity_id = ?", *in.PartyID, entityID).First(&p).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return models.Sale{}, false, &InvalidError{"ese cliente no existe en este negocio"}
			}
			return models.Sale{}, false, err
		}
		if want != "" && p.Kind != want {
			return models.Sale{}, false, &InvalidError{"el fiado va a un cliente, no a un proveedor"}
		}
	}

	sale := models.Sale{
		ID: in.ClientID, EntityID: entityID, ClientID: in.ClientID, OccurredAt: at, Total: total, Method: in.Method,
		PartyID: in.PartyID, Source: src, Status: models.LedgerActive, Note: cleanText(in.Note, 200),
	}
	var created bool
	body := func(t *gorm.DB) error {
		res := t.Omit("Lines", "Party").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "client_id"}}, DoNothing: true}).Create(&sale)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		created = true
		for i := range lines {
			lines[i].SaleID = sale.ID
		}
		if len(lines) > 0 {
			if err := t.Create(&lines).Error; err != nil {
				return err
			}
			return s.stockSale(t, entityID, sale.ID, at, lines)
		}
		return nil
	}
	if tx != nil {
		err = body(tx)
	} else {
		err = s.db.Transaction(body)
	}
	if err != nil {
		return models.Sale{}, false, err
	}
	if !created {
		var old models.Sale
		if err := db.Preload("Lines").Where("entity_id = ? AND client_id = ?", entityID, in.ClientID).First(&old).Error; err != nil {
			return models.Sale{}, false, err
		}
		return old, false, nil
	}
	sale.Lines = lines
	return sale, true, nil
}

// VoidSale anula una venta (con motivo). Anular dos veces es inofensivo.
func (s *LedgerService) VoidSale(ownerID, entityID, saleID uuid.UUID, reason string) (models.Sale, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.Sale{}, err
	}
	var sale models.Sale
	if err := s.db.Preload("Lines").Where("id = ? AND entity_id = ?", saleID, entityID).First(&sale).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return sale, ErrLedgerNotFound
		}
		return sale, err
	}
	if sale.Status == models.LedgerVoided {
		return sale, nil
	}
	now := nowCO()
	sale.Status, sale.VoidedAt, sale.VoidReason = models.LedgerVoided, &now, cleanText(reason, 120)
	err := s.db.Transaction(func(t *gorm.DB) error {
		if err := t.Model(&models.Sale{}).Where("id = ?", sale.ID).
			Updates(map[string]any{"status": sale.Status, "voided_at": now, "void_reason": sale.VoidReason}).Error; err != nil {
			return err
		}
		if err := s.reverseMoves(t, entityID, sale.ID, models.MoveSale, models.MoveSaleVoid, now); err != nil {
			return err
		}
		return s.reverseMoves(t, entityID, sale.ID, models.MoveUse, models.MoveUseVoid, now)
	})
	return sale, err
}

type ListFilter struct {
	From, To *time.Time
	Limit    int
	Before   *time.Time // paginación: solo lo anterior a esta fecha
}

func (f ListFilter) limit() int {
	if f.Limit <= 0 {
		return defaultListLimit
	}
	if f.Limit > maxListLimit {
		return maxListLimit
	}
	return f.Limit
}

func (s *LedgerService) ListSales(ownerID, entityID uuid.UUID, f ListFilter) ([]models.Sale, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	q := s.db.Preload("Lines").Preload("Party").Where("entity_id = ?", entityID)
	if f.From != nil {
		q = q.Where("occurred_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("occurred_at < ?", *f.To)
	}
	if f.Before != nil {
		q = q.Where("occurred_at < ?", *f.Before)
	}
	var out []models.Sale
	err := q.Order("occurred_at DESC, created_at DESC").Limit(f.limit()).Find(&out).Error
	return out, err
}

// ---------- gastos ----------

type ExpenseInput struct {
	ClientID   uuid.UUID  `json:"client_id"`
	OccurredAt *time.Time `json:"occurred_at"`
	Amount     int        `json:"amount"`
	Category   string     `json:"category"`
	Method     string     `json:"method"`
	PartyID    *uuid.UUID `json:"party_id"`
	Note       string     `json:"note"`
	Source     string     `json:"source"`
	// Lines: productos o insumos comprados. Suben las existencias y actualizan el costo.
	Lines []PurchaseLine `json:"lines"`
}

func (s *LedgerService) CreateExpense(ownerID, entityID uuid.UUID, in ExpenseInput) (models.Expense, bool, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.Expense{}, false, err
	}
	return s.createExpense(entityID, in, nil)
}

func (s *LedgerService) createExpense(entityID uuid.UUID, in ExpenseInput, tx *gorm.DB) (models.Expense, bool, error) {
	db := s.db
	if tx != nil {
		db = tx
	}
	if in.ClientID == uuid.Nil {
		return models.Expense{}, false, &InvalidError{"falta client_id"}
	}
	xlines, lsum, err := s.purchaseLines(db, entityID, in.Lines)
	if err != nil {
		return models.Expense{}, false, err
	}
	if in.Amount == 0 && lsum > 0 {
		in.Amount = lsum // «compré 10 de esto a tal costo»: el total sale de los renglones
	}
	if err := checkAmount(in.Amount, "el monto"); err != nil {
		return models.Expense{}, false, err
	}
	if !models.ValidPayMethod(in.Method) {
		return models.Expense{}, false, &InvalidError{"medio de pago inválido"}
	}
	cat := strings.ToLower(strings.TrimSpace(in.Category))
	if cat == "" {
		cat = "otro"
	}
	if !expenseCategories[cat] {
		return models.Expense{}, false, &InvalidError{"categoría de gasto inválida"}
	}
	src, err := validSource(in.Source)
	if err != nil {
		return models.Expense{}, false, err
	}
	at, err := resolveTime(in.OccurredAt)
	if err != nil {
		return models.Expense{}, false, err
	}
	if in.Method == models.PayCredit && in.PartyID == nil {
		return models.Expense{}, false, &InvalidError{"una compra a crédito necesita el proveedor"}
	}
	if in.PartyID != nil {
		var p models.Party
		if err := db.Where("id = ? AND entity_id = ?", *in.PartyID, entityID).First(&p).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return models.Expense{}, false, &InvalidError{"ese proveedor no existe en este negocio"}
			}
			return models.Expense{}, false, err
		}
		if in.Method == models.PayCredit && p.Kind != models.PartySupplier {
			return models.Expense{}, false, &InvalidError{"la compra a crédito va a un proveedor"}
		}
	}
	e := models.Expense{
		ID: in.ClientID, EntityID: entityID, ClientID: in.ClientID, OccurredAt: at, Amount: in.Amount, Category: cat, Method: in.Method,
		PartyID: in.PartyID, Source: src, Status: models.LedgerActive, Note: cleanText(in.Note, 200),
	}
	var made bool
	body := func(t *gorm.DB) error {
		res := t.Omit("Party", "Lines").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "client_id"}}, DoNothing: true}).Create(&e)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		made = true
		if len(xlines) == 0 {
			return nil
		}
		for i := range xlines {
			xlines[i].ExpenseID = e.ID
		}
		if err := t.Create(&xlines).Error; err != nil {
			return err
		}
		return s.stockPurchase(t, entityID, e.ID, at, xlines)
	}
	if tx != nil {
		err = body(tx)
	} else {
		err = s.db.Transaction(body)
	}
	if err != nil {
		return models.Expense{}, false, err
	}
	if !made {
		var old models.Expense
		if err := db.Preload("Lines").Where("entity_id = ? AND client_id = ?", entityID, in.ClientID).First(&old).Error; err != nil {
			return models.Expense{}, false, err
		}
		return old, false, nil
	}
	e.Lines = xlines
	return e, true, nil
}

func (s *LedgerService) VoidExpense(ownerID, entityID, expenseID uuid.UUID, reason string) (models.Expense, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.Expense{}, err
	}
	var e models.Expense
	if err := s.db.Where("id = ? AND entity_id = ?", expenseID, entityID).First(&e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return e, ErrLedgerNotFound
		}
		return e, err
	}
	if e.Status == models.LedgerVoided {
		return e, nil
	}
	now := nowCO()
	e.Status, e.VoidedAt, e.VoidReason = models.LedgerVoided, &now, cleanText(reason, 120)
	err := s.db.Transaction(func(t *gorm.DB) error {
		if err := t.Model(&models.Expense{}).Where("id = ?", e.ID).
			Updates(map[string]any{"status": e.Status, "voided_at": now, "void_reason": e.VoidReason}).Error; err != nil {
			return err
		}
		return s.reverseMoves(t, entityID, e.ID, models.MovePurchase, models.MovePurchaseVoid, now)
	})
	return e, err
}

func (s *LedgerService) ListExpenses(ownerID, entityID uuid.UUID, f ListFilter) ([]models.Expense, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	q := s.db.Preload("Party").Preload("Lines").Where("entity_id = ?", entityID)
	if f.From != nil {
		q = q.Where("occurred_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("occurred_at < ?", *f.To)
	}
	if f.Before != nil {
		q = q.Where("occurred_at < ?", *f.Before)
	}
	var out []models.Expense
	err := q.Order("occurred_at DESC, created_at DESC").Limit(f.limit()).Find(&out).Error
	return out, err
}

// ---------- abonos ----------

type PaymentInput struct {
	ClientID   uuid.UUID  `json:"client_id"`
	PartyID    uuid.UUID  `json:"party_id"`
	Amount     int        `json:"amount"`
	Method     string     `json:"method"`
	OccurredAt *time.Time `json:"occurred_at"`
	Note       string     `json:"note"`
}

// CreatePayment registra un abono: el cliente paga su fiado o el negocio paga a un proveedor. La dirección sale
// del tipo de tercero. No puede superar lo que se debe.
func (s *LedgerService) CreatePayment(ownerID, entityID uuid.UUID, in PaymentInput) (models.DebtPayment, bool, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.DebtPayment{}, false, err
	}
	return s.createPayment(entityID, in, nil)
}

func (s *LedgerService) createPayment(entityID uuid.UUID, in PaymentInput, tx *gorm.DB) (models.DebtPayment, bool, error) {
	db := s.db
	if tx != nil {
		db = tx
	}
	if in.ClientID == uuid.Nil {
		return models.DebtPayment{}, false, &InvalidError{"falta client_id"}
	}
	if err := checkAmount(in.Amount, "el abono"); err != nil {
		return models.DebtPayment{}, false, err
	}
	if in.Method == models.PayCredit || !models.ValidPayMethod(in.Method) {
		return models.DebtPayment{}, false, &InvalidError{"medio de pago inválido para un abono"}
	}
	at, err := resolveTime(in.OccurredAt)
	if err != nil {
		return models.DebtPayment{}, false, err
	}
	// Idempotencia primero: un reenvío no debe fallar porque el saldo ya bajó.
	var old models.DebtPayment
	if err := db.Where("entity_id = ? AND client_id = ?", entityID, in.ClientID).First(&old).Error; err == nil {
		return old, false, nil
	}
	var p models.Party
	if err := db.Where("id = ? AND entity_id = ?", in.PartyID, entityID).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.DebtPayment{}, false, &InvalidError{"ese cliente o proveedor no existe en este negocio"}
		}
		return models.DebtPayment{}, false, err
	}
	dir := models.DebtReceivable
	if p.Kind == models.PartySupplier {
		dir = models.DebtPayable
	}
	owed, err := s.partyBalance(db, entityID, p.ID)
	if err != nil {
		return models.DebtPayment{}, false, err
	}
	if in.Amount > owed {
		return models.DebtPayment{}, false, &InvalidError{fmt.Sprintf("el abono supera lo que se debe (%d)", owed)}
	}
	d := models.DebtPayment{
		ID: in.ClientID, EntityID: entityID, ClientID: in.ClientID, PartyID: p.ID, Direction: dir, Amount: in.Amount,
		Method: in.Method, OccurredAt: at, Note: cleanText(in.Note, 200),
	}
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "client_id"}}, DoNothing: true}).Create(&d)
	if res.Error != nil {
		return models.DebtPayment{}, false, res.Error
	}
	if res.RowsAffected == 0 {
		if err := db.Where("entity_id = ? AND client_id = ?", entityID, in.ClientID).First(&old).Error; err != nil {
			return models.DebtPayment{}, false, err
		}
		return old, false, nil
	}
	return d, true, nil
}

func (s *LedgerService) partyBalance(db *gorm.DB, entityID, partyID uuid.UUID) (int, error) {
	var credit, buys, pays int
	if err := db.Model(&models.Sale{}).Where("entity_id = ? AND party_id = ? AND status = ? AND method = ?", entityID, partyID, models.LedgerActive, models.PayCredit).
		Select("COALESCE(SUM(total),0)").Scan(&credit).Error; err != nil {
		return 0, err
	}
	if err := db.Model(&models.Expense{}).Where("entity_id = ? AND party_id = ? AND status = ? AND method = ?", entityID, partyID, models.LedgerActive, models.PayCredit).
		Select("COALESCE(SUM(amount),0)").Scan(&buys).Error; err != nil {
		return 0, err
	}
	if err := db.Model(&models.DebtPayment{}).Where("entity_id = ? AND party_id = ?", entityID, partyID).
		Select("COALESCE(SUM(amount),0)").Scan(&pays).Error; err != nil {
		return 0, err
	}
	return credit + buys - pays, nil
}

func (s *LedgerService) ListPayments(ownerID, entityID, partyID uuid.UUID) ([]models.DebtPayment, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	var out []models.DebtPayment
	err := s.db.Where("entity_id = ? AND party_id = ?", entityID, partyID).Order("occurred_at DESC").Limit(200).Find(&out).Error
	return out, err
}

// ---------- catálogo con costo ----------

type ItemCost struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Price *int      `json:"price"`
	Cost  *int      `json:"cost"`
}

// ListItems devuelve los productos del catálogo con su costo, solo para el dueño.
func (s *LedgerService) ListItems(ownerID, entityID uuid.UUID) ([]ItemCost, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	var items []models.ServiceItem
	if err := s.db.Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).Order("position, name").Find(&items).Error; err != nil {
		return nil, err
	}
	byID, children := variantInfo(items)
	out := make([]ItemCost, 0, len(items))
	for _, it := range items {
		if children[it.ID] > 0 {
			continue // un producto con variantes no se vende: se vende cada variante
		}
		out = append(out, ItemCost{ID: it.ID, Name: it.Name, Price: effectivePrice(it, byID), Cost: effectiveCost(it, byID)})
	}
	return out, nil
}

// SetItemCost fija (o borra, con nil) el costo de un producto. No toca el precio ni la historia de ventas.
func (s *LedgerService) SetItemCost(ownerID, entityID, itemID uuid.UUID, cost *int) error {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return err
	}
	if cost != nil && (*cost < 0 || *cost > 10_000_000) {
		return &InvalidError{"costo inválido"}
	}
	res := s.db.Model(&models.ServiceItem{}).Where("id = ? AND entity_id = ?", itemID, entityID).UpdateColumn("cost", cost)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLedgerNotFound
	}
	return nil
}

// ---------- resumen ----------

type MethodTotals struct {
	Cash     int `json:"cash"`
	Transfer int `json:"transfer"`
	Card     int `json:"card"`
	Credit   int `json:"credit"`
}

type TopItem struct {
	Name    string  `json:"name"`
	Qty     float64 `json:"qty"`
	Revenue int     `json:"revenue"`
}

type LedgerSummary struct {
	From   time.Time `json:"from"` // [From, To)
	To     time.Time `json:"to"`
	Period string    `json:"period"`

	SalesTotal    int            `json:"sales_total"`
	SalesCount    int            `json:"sales_count"`
	ExpensesTotal int            `json:"expenses_total"`
	Profit        int            `json:"profit"` // ventas − gastos del periodo
	ByMethod      MethodTotals   `json:"by_method"`
	ExpensesByCat map[string]int `json:"expenses_by_category"`
	// Margen bruto: solo sobre los renglones con costo conocido. MarginCoverage es el % de la venta en renglones
	// con costo; si es bajo, el margen no es confiable y la app lo dice.
	GrossMargin    int       `json:"gross_margin"`
	MarginCoverage int       `json:"margin_coverage_pct"`
	TopItems       []TopItem `json:"top_items"`

	DebtsCollected int `json:"debts_collected"` // abonos de clientes en el periodo
	DebtsPaid      int `json:"debts_paid"`      // abonos a proveedores en el periodo
	Receivable     int `json:"receivable"`      // total que le deben al negocio hoy
	Payable        int `json:"payable"`         // total que el negocio debe hoy

	// Caja del periodo: plata que realmente entró y salió (sin fiados nuevos ni compras a crédito).
	CashIn  int `json:"cash_in"`
	CashOut int `json:"cash_out"`

	PrevSalesTotal    int `json:"prev_sales_total"`
	PrevExpensesTotal int `json:"prev_expenses_total"`
}

// PeriodRange devuelve [from, to) en hora de Colombia para "day", "week" (lunes a domingo) o "month" que contiene ref.
func PeriodRange(period string, ref time.Time) (time.Time, time.Time, error) {
	loc := nowCO().Location()
	r := ref.In(loc)
	day := time.Date(r.Year(), r.Month(), r.Day(), 0, 0, 0, 0, loc)
	switch period {
	case "day", "":
		return day, day.AddDate(0, 0, 1), nil
	case "week":
		off := (int(day.Weekday()) + 6) % 7
		start := day.AddDate(0, 0, -off)
		return start, start.AddDate(0, 0, 7), nil
	case "month":
		start := time.Date(r.Year(), r.Month(), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), nil
	}
	return time.Time{}, time.Time{}, &InvalidError{"period debe ser day, week o month"}
}

func prevRange(period string, from, to time.Time) (time.Time, time.Time) {
	switch period {
	case "month":
		return from.AddDate(0, -1, 0), from
	case "week":
		return from.AddDate(0, 0, -7), from
	}
	return from.AddDate(0, 0, -1), from
}

func (s *LedgerService) Summary(ownerID, entityID uuid.UUID, period string, ref time.Time) (LedgerSummary, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return LedgerSummary{}, err
	}
	from, to, err := PeriodRange(period, ref)
	if err != nil {
		return LedgerSummary{}, err
	}
	sum := LedgerSummary{From: from, To: to, Period: period, ExpensesByCat: map[string]int{}}
	if sum.Period == "" {
		sum.Period = "day"
	}

	var sales []models.Sale
	if err := s.db.Preload("Lines").Where("entity_id = ? AND status = ? AND occurred_at >= ? AND occurred_at < ?",
		entityID, models.LedgerActive, from, to).Find(&sales).Error; err != nil {
		return sum, err
	}
	type agg struct {
		qty float64
		rev int
	}
	items := map[string]*agg{}
	costedRevenue, lineRevenue := 0, 0
	for _, sl := range sales {
		sum.SalesTotal += sl.Total
		sum.SalesCount++
		switch sl.Method {
		case models.PayCash:
			sum.ByMethod.Cash += sl.Total
			sum.CashIn += sl.Total
		case models.PayTransfer:
			sum.ByMethod.Transfer += sl.Total
			sum.CashIn += sl.Total
		case models.PayCard:
			sum.ByMethod.Card += sl.Total
			sum.CashIn += sl.Total
		case models.PayCredit:
			sum.ByMethod.Credit += sl.Total
		}
		// Reparto del descuento: el margen se calcula con el total real, repartido por renglón.
		lineSum := 0
		for _, l := range sl.Lines {
			lineSum += int(math.Round(l.Qty * float64(l.UnitPrice)))
		}
		for _, l := range sl.Lines {
			gross := int(math.Round(l.Qty * float64(l.UnitPrice)))
			rev := gross
			if lineSum > 0 {
				rev = int(math.Round(float64(gross) * float64(sl.Total) / float64(lineSum)))
			}
			key := ledger.Normalize(l.Name)
			a := items[key]
			if a == nil {
				a = &agg{}
				items[key] = a
			}
			a.qty += l.Qty
			a.rev += rev
			lineRevenue += rev
			if l.UnitCost != nil {
				costedRevenue += rev
				sum.GrossMargin += rev - int(math.Round(l.Qty*float64(*l.UnitCost)))
			}
		}
	}
	if lineRevenue > 0 {
		sum.MarginCoverage = costedRevenue * 100 / lineRevenue
	}
	// Nombre de muestra por clave: el de la última venta.
	names := map[string]string{}
	for _, sl := range sales {
		for _, l := range sl.Lines {
			names[ledger.Normalize(l.Name)] = l.Name
		}
	}
	for k, a := range items {
		sum.TopItems = append(sum.TopItems, TopItem{Name: names[k], Qty: a.qty, Revenue: a.rev})
	}
	sort.Slice(sum.TopItems, func(i, j int) bool {
		if sum.TopItems[i].Revenue != sum.TopItems[j].Revenue {
			return sum.TopItems[i].Revenue > sum.TopItems[j].Revenue
		}
		return sum.TopItems[i].Name < sum.TopItems[j].Name
	})
	if len(sum.TopItems) > 5 {
		sum.TopItems = sum.TopItems[:5]
	}

	var exps []models.Expense
	if err := s.db.Where("entity_id = ? AND status = ? AND occurred_at >= ? AND occurred_at < ?",
		entityID, models.LedgerActive, from, to).Find(&exps).Error; err != nil {
		return sum, err
	}
	for _, e := range exps {
		sum.ExpensesTotal += e.Amount
		sum.ExpensesByCat[e.Category] += e.Amount
		if e.Method != models.PayCredit {
			sum.CashOut += e.Amount
		}
	}
	sum.Profit = sum.SalesTotal - sum.ExpensesTotal

	var pays []models.DebtPayment
	if err := s.db.Where("entity_id = ? AND occurred_at >= ? AND occurred_at < ?", entityID, from, to).Find(&pays).Error; err != nil {
		return sum, err
	}
	for _, p := range pays {
		if p.Direction == models.DebtReceivable {
			sum.DebtsCollected += p.Amount
			sum.CashIn += p.Amount
		} else {
			sum.DebtsPaid += p.Amount
			sum.CashOut += p.Amount
		}
	}

	var parties []models.Party
	if err := s.db.Where("entity_id = ?", entityID).Find(&parties).Error; err != nil {
		return sum, err
	}
	bal, err := s.balances(entityID)
	if err != nil {
		return sum, err
	}
	for _, p := range parties {
		b := bal[p.ID]
		if b <= 0 {
			continue
		}
		if p.Kind == models.PartySupplier {
			sum.Payable += b
		} else {
			sum.Receivable += b
		}
	}

	pf, pt := prevRange(sum.Period, from, to)
	if err := s.db.Model(&models.Sale{}).Where("entity_id = ? AND status = ? AND occurred_at >= ? AND occurred_at < ?",
		entityID, models.LedgerActive, pf, pt).Select("COALESCE(SUM(total),0)").Scan(&sum.PrevSalesTotal).Error; err != nil {
		return sum, err
	}
	if err := s.db.Model(&models.Expense{}).Where("entity_id = ? AND status = ? AND occurred_at >= ? AND occurred_at < ?",
		entityID, models.LedgerActive, pf, pt).Select("COALESCE(SUM(amount),0)").Scan(&sum.PrevExpensesTotal).Error; err != nil {
		return sum, err
	}
	return sum, nil
}

// ---------- exportar ----------

// ExportCSV devuelve ventas y gastos del rango como CSV (para el contador). Incluye anulados, marcados.
func (s *LedgerService) ExportCSV(ownerID, entityID uuid.UUID, from, to time.Time) ([]byte, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	var sales []models.Sale
	if err := s.db.Preload("Party").Where("entity_id = ? AND occurred_at >= ? AND occurred_at < ?", entityID, from, to).
		Order("occurred_at").Find(&sales).Error; err != nil {
		return nil, err
	}
	var exps []models.Expense
	if err := s.db.Preload("Party").Where("entity_id = ? AND occurred_at >= ? AND occurred_at < ?", entityID, from, to).
		Order("occurred_at").Find(&exps).Error; err != nil {
		return nil, err
	}
	var buf strings.Builder
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM: Excel abre las tildes bien
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"fecha", "tipo", "monto", "medio", "categoria", "tercero", "nota", "estado"})
	type rec struct {
		at  time.Time
		row []string
	}
	var rows []rec
	party := func(p *models.Party) string {
		if p == nil {
			return ""
		}
		return p.Name
	}
	for _, x := range sales {
		rows = append(rows, rec{x.OccurredAt, []string{x.OccurredAt.Format("2006-01-02 15:04"), "venta", strconv.Itoa(x.Total), x.Method, "", csvSafe(party(x.Party)), csvSafe(x.Note), x.Status}})
	}
	for _, x := range exps {
		rows = append(rows, rec{x.OccurredAt, []string{x.OccurredAt.Format("2006-01-02 15:04"), "gasto", strconv.Itoa(x.Amount), x.Method, x.Category, csvSafe(party(x.Party)), csvSafe(x.Note), x.Status}})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.Before(rows[j].at) })
	for _, r := range rows {
		_ = w.Write(r.row)
	}
	w.Flush()
	return []byte(buf.String()), w.Error()
}

// csvSafe evita que Excel interprete un texto del usuario como fórmula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsAny(s[:1], "=+-@\t\r") {
		return "'" + s
	}
	return s
}
