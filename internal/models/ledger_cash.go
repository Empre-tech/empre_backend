package models

import (
	"time"

	"github.com/google/uuid"
)

// Direcciones de un movimiento de caja.
const (
	CashOut = "out" // saqué plata de la caja
	CashIn  = "in"  // metí plata a la caja
)

// CashMove es plata que entra o sale de la caja sin ser venta ni gasto («saqué para mí», «metí sencillo»).
// Cambia lo que debería haber en la caja; nunca toca las ventas, los gastos ni las ganancias.
type CashMove struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID   uuid.UUID  `gorm:"type:uuid;not null;index:idx_cash_moves_entity_time,priority:1;uniqueIndex:uniq_cash_moves_client,priority:1" json:"entity_id"`
	ClientID   uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:uniq_cash_moves_client,priority:2" json:"client_id"`
	OccurredAt time.Time  `gorm:"not null;index:idx_cash_moves_entity_time,priority:2" json:"occurred_at"`
	Direction  string     `gorm:"type:varchar(3);not null" json:"direction"`
	Amount     int        `gorm:"not null;check:chk_cash_moves_amount,amount > 0 AND amount <= 100000000" json:"amount"`
	Reason     string     `gorm:"type:varchar(20);not null" json:"reason"`
	Status     string     `gorm:"type:varchar(10);not null;default:'active'" json:"status"`
	VoidedAt   *time.Time `json:"voided_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

func (CashMove) TableName() string { return "cash_moves" }

// CashClose es el cierre de caja de un día: lo que se contó, lo que debía haber y la base que queda para el siguiente.
// Hay uno por día; cerrar otra vez el mismo día lo reemplaza.
type CashClose struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_cash_closes_day,priority:1" json:"entity_id"`
	Day      string    `gorm:"type:varchar(10);not null;uniqueIndex:uniq_cash_closes_day,priority:2" json:"day"`
	// Opening: con cuánto abrió, solo cuando no había un cierre anterior del que sacarlo.
	Opening  *int `json:"opening,omitempty"`
	Counted  int  `gorm:"not null" json:"counted"`
	Expected int  `gorm:"not null" json:"expected"`
	BaseNext int  `gorm:"not null" json:"base_next"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CashClose) TableName() string { return "cash_closes" }
