package services

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"empre_backend/internal/models"
)

// Para qué se saca o se mete plata. «Le pagué a alguien» no está: eso es un gasto en efectivo y va en los números.
var cashReasons = map[string]map[string]bool{
	models.CashOut: {"para_mi": true, "al_banco": true},
	models.CashIn:  {"mas_base": true, "sencillo": true, "plata_mia": true},
}

// CashMoveInput es lo que manda el teléfono al anotar plata que entró o salió.
type CashMoveInput struct {
	ClientID   uuid.UUID  `json:"client_id"`
	OccurredAt *time.Time `json:"occurred_at"`
	Direction  string     `json:"direction"`
	Amount     int        `json:"amount"`
	Reason     string     `json:"reason"`
}

// CashCloseInput es el cierre de un día.
type CashCloseInput struct {
	Day      string `json:"day"`
	Opening  *int   `json:"opening"`
	Counted  int    `json:"counted"`
	BaseNext int    `json:"base_next"`
}

// CashFlow es lo que pasó por la caja en un rango.
type CashFlow struct {
	Sales         int `json:"sales"`
	SalesCount    int `json:"sales_count"`
	Expenses      int `json:"expenses"`
	ExpensesCount int `json:"expenses_count"`
	DebtsIn       int `json:"debts_in"`
	DebtsOut      int `json:"debts_out"`
	MovesIn       int `json:"moves_in"`
	MovesOut      int `json:"moves_out"`
}

// Net: en cuánto cambió la caja.
func (f CashFlow) Net() int {
	return f.Sales + f.DebtsIn + f.MovesIn - f.Expenses - f.DebtsOut - f.MovesOut
}

// CashDay es la cuenta de la caja de un día.
type CashDay struct {
	Day string `json:"day"`
	// Base: con cuánto arrancó el día. BaseKnown es false mientras no haya un cierre del que sacarla: se le pregunta al dueño.
	Base      int  `json:"base"`
	BaseKnown bool `json:"base_known"`
	// Carried: lo que cambió la caja en los días sin cerrar desde el último cierre; ya va sumado en Base.
	Carried  int               `json:"carried"`
	Flow     CashFlow          `json:"flow"`
	Expected int               `json:"expected"`
	Moves    []models.CashMove `json:"moves"`
	Close    *models.CashClose `json:"close"`
}

// cashSpan suma lo que pasó por la caja: solo efectivo y solo lo que sigue activo.
func cashSpan(sales []models.Sale, expenses []models.Expense, pays []models.DebtPayment, moves []models.CashMove) CashFlow {
	var f CashFlow
	for _, sl := range sales {
		if sl.Method == models.PayCash && sl.Status == models.LedgerActive {
			f.Sales += sl.Total
			f.SalesCount++
		}
	}
	for _, e := range expenses {
		if e.Method == models.PayCash && e.Status == models.LedgerActive {
			f.Expenses += e.Amount
			f.ExpensesCount++
		}
	}
	for _, p := range pays {
		if p.Method != models.PayCash {
			continue
		}
		if p.Direction == models.DebtReceivable {
			f.DebtsIn += p.Amount
		} else {
			f.DebtsOut += p.Amount
		}
	}
	for _, m := range moves {
		if m.Status != models.LedgerActive {
			continue
		}
		if m.Direction == models.CashIn {
			f.MovesIn += m.Amount
		} else {
			f.MovesOut += m.Amount
		}
	}
	return f
}

// checkCashMove valida el sentido, el motivo y el monto de un movimiento.
func checkCashMove(in CashMoveInput) error {
	reasons, ok := cashReasons[in.Direction]
	if !ok {
		return &InvalidError{"di si sacaste o metiste plata"}
	}
	if !reasons[in.Reason] {
		return &InvalidError{"motivo inválido"}
	}
	return checkAmount(in.Amount, "el monto")
}

// checkCashClose valida un cierre: la base que queda sale de lo que se contó.
func checkCashClose(in CashCloseInput) error {
	if in.Counted < 0 || in.Counted > maxLedgerAmount {
		return &InvalidError{"lo contado no es válido"}
	}
	if in.BaseNext < 0 {
		return &InvalidError{"la base no puede ser negativa"}
	}
	if in.BaseNext > in.Counted {
		return &InvalidError{"la base no puede ser mayor que lo contado"}
	}
	if in.Opening != nil && (*in.Opening < 0 || *in.Opening > maxLedgerAmount) {
		return &InvalidError{"la base con que abriste no es válida"}
	}
	return nil
}

// cashDayStart: el día AAAA-MM-DD en hora de Colombia; vacío es hoy. No acepta días futuros.
func cashDayStart(day string) (time.Time, error) {
	now := nowCO()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if day == "" {
		return today, nil
	}
	d, err := time.ParseInLocation(dayFmt, day, now.Location())
	if err != nil {
		return time.Time{}, &InvalidError{"fecha inválida"}
	}
	if d.After(today) {
		return time.Time{}, &InvalidError{"la fecha no puede estar en el futuro"}
	}
	return d, nil
}

func (s *LedgerService) cashFlow(entityID uuid.UUID, from, to time.Time) (CashFlow, []models.CashMove, error) {
	var (
		sales    []models.Sale
		expenses []models.Expense
		pays     []models.DebtPayment
		moves    []models.CashMove
	)
	inRange := "entity_id = ? AND occurred_at >= ? AND occurred_at < ?"
	if err := s.db.Where(inRange, entityID, from, to).Find(&sales).Error; err != nil {
		return CashFlow{}, nil, err
	}
	if err := s.db.Where(inRange, entityID, from, to).Find(&expenses).Error; err != nil {
		return CashFlow{}, nil, err
	}
	if err := s.db.Where(inRange, entityID, from, to).Find(&pays).Error; err != nil {
		return CashFlow{}, nil, err
	}
	if err := s.db.Where(inRange, entityID, from, to).Order("occurred_at").Find(&moves).Error; err != nil {
		return CashFlow{}, nil, err
	}
	return cashSpan(sales, expenses, pays, moves), moves, nil
}

// cashDay arma la cuenta de un día. La base sale del último cierre anterior más lo que pasó en los días sin cerrar;
// si nunca se ha cerrado, de lo que el dueño dijo al abrir ese día.
func (s *LedgerService) cashDay(entityID uuid.UUID, start time.Time) (CashDay, error) {
	day := start.Format(dayFmt)
	out := CashDay{Day: day, Moves: []models.CashMove{}}

	var mine models.CashClose
	err := s.db.Where("entity_id = ? AND day = ?", entityID, day).First(&mine).Error
	if err == nil {
		out.Close = &mine
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return out, err
	}

	var prev models.CashClose
	err = s.db.Where("entity_id = ? AND day < ?", entityID, day).Order("day DESC").First(&prev).Error
	switch {
	case err == nil:
		out.Base, out.BaseKnown = prev.BaseNext, true
		prevDay, perr := time.ParseInLocation(dayFmt, prev.Day, start.Location())
		if perr != nil {
			return out, perr
		}
		if gapFrom := prevDay.AddDate(0, 0, 1); gapFrom.Before(start) {
			gap, _, gerr := s.cashFlow(entityID, gapFrom, start)
			if gerr != nil {
				return out, gerr
			}
			out.Carried = gap.Net()
			out.Base += out.Carried
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		if out.Close != nil && out.Close.Opening != nil {
			out.Base, out.BaseKnown = *out.Close.Opening, true
		}
	default:
		return out, err
	}

	flow, moves, err := s.cashFlow(entityID, start, start.AddDate(0, 0, 1))
	if err != nil {
		return out, err
	}
	out.Flow, out.Moves = flow, moves
	out.Expected = out.Base + flow.Net()
	return out, nil
}

// CashDay devuelve la cuenta de la caja de un día (vacío: hoy).
func (s *LedgerService) CashDay(ownerID, entityID uuid.UUID, day string) (CashDay, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return CashDay{}, err
	}
	start, err := cashDayStart(day)
	if err != nil {
		return CashDay{}, err
	}
	return s.cashDay(entityID, start)
}

// CreateCashMove anota plata que entró o salió de la caja. Reenviar el mismo client_id no duplica nada.
func (s *LedgerService) CreateCashMove(ownerID, entityID uuid.UUID, in CashMoveInput) (models.CashMove, bool, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.CashMove{}, false, err
	}
	if in.ClientID == uuid.Nil {
		return models.CashMove{}, false, &InvalidError{"falta client_id"}
	}
	if err := checkCashMove(in); err != nil {
		return models.CashMove{}, false, err
	}
	at, err := resolveTime(in.OccurredAt)
	if err != nil {
		return models.CashMove{}, false, err
	}
	m := models.CashMove{
		ID: in.ClientID, EntityID: entityID, ClientID: in.ClientID, OccurredAt: at, Direction: in.Direction,
		Amount: in.Amount, Reason: in.Reason, Status: models.LedgerActive,
	}
	res := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}, {Name: "client_id"}}, DoNothing: true}).Create(&m)
	if res.Error != nil {
		return models.CashMove{}, false, res.Error
	}
	if res.RowsAffected == 0 {
		var old models.CashMove
		if err := s.db.Where("entity_id = ? AND client_id = ?", entityID, in.ClientID).First(&old).Error; err != nil {
			return models.CashMove{}, false, err
		}
		return old, false, nil
	}
	return m, true, nil
}

// VoidCashMove anula un movimiento: deja de contar en la caja, pero no se borra.
func (s *LedgerService) VoidCashMove(ownerID, entityID, moveID uuid.UUID) (models.CashMove, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.CashMove{}, err
	}
	var m models.CashMove
	if err := s.db.Where("id = ? AND entity_id = ?", moveID, entityID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return m, ErrLedgerNotFound
		}
		return m, err
	}
	if m.Status == models.LedgerVoided {
		return m, nil
	}
	now := nowCO()
	m.Status, m.VoidedAt = models.LedgerVoided, &now
	err := s.db.Model(&models.CashMove{}).Where("id = ?", m.ID).Updates(map[string]any{"status": m.Status, "voided_at": now}).Error
	return m, err
}

// CloseCash guarda el cierre de un día. Lo que debía haber lo calcula el servidor con lo que tenga en ese momento;
// cerrar otra vez el mismo día reemplaza el cierre.
func (s *LedgerService) CloseCash(ownerID, entityID uuid.UUID, in CashCloseInput) (models.CashClose, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return models.CashClose{}, err
	}
	if err := checkCashClose(in); err != nil {
		return models.CashClose{}, err
	}
	start, err := cashDayStart(in.Day)
	if err != nil {
		return models.CashClose{}, err
	}
	cd, err := s.cashDay(entityID, start)
	if err != nil {
		return models.CashClose{}, err
	}
	opening := in.Opening
	if opening == nil && cd.Close != nil {
		opening = cd.Close.Opening
	}
	expected := cd.Expected
	if !cd.BaseKnown {
		if opening == nil {
			return models.CashClose{}, &InvalidError{"falta con cuánto abriste la caja"}
		}
		expected = *opening + cd.Flow.Net()
	}
	c := models.CashClose{EntityID: entityID, Day: cd.Day, Opening: opening, Counted: in.Counted, Expected: expected, BaseNext: in.BaseNext}
	err = s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "entity_id"}, {Name: "day"}},
		DoUpdates: clause.AssignmentColumns([]string{"opening", "counted", "expected", "base_next", "updated_at"}),
	}).Create(&c).Error
	return c, err
}
