package models

import (
	"time"

	"github.com/google/uuid"
)

// Cuentas abiertas (fase 8): una venta que se va llenando y se cobra al final (la mesa 3, el cliente de la silla 2).
// Al cobrarla se convierte en una venta normal; mientras está abierta no mueve plata ni inventario.

const (
	TabOpen      = "open"
	TabPaid      = "paid"
	TabCancelled = "cancelled"
)

type OpenTab struct {
	// ID lo genera la app (sirve de client_id): así una cuenta abierta sin señal se puede llenar antes de enviarse.
	ID       uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	EntityID uuid.UUID  `gorm:"type:uuid;not null;index:idx_tabs_entity_status,priority:1" json:"entity_id"`
	Name     string     `gorm:"size:40;not null" json:"name"`
	Status   string     `gorm:"type:varchar(10);not null;default:'open';index:idx_tabs_entity_status,priority:2" json:"status"`
	OpenedAt time.Time  `gorm:"not null" json:"opened_at"`
	ClosedAt *time.Time `json:"closed_at,omitempty"`
	// SaleID: la venta en que se convirtió al cobrarla.
	SaleID       *uuid.UUID `gorm:"type:uuid" json:"sale_id,omitempty"`
	CancelReason string     `gorm:"size:120" json:"cancel_reason,omitempty"`
	// Tip: propina o servicio voluntario, aparte de la venta (no se suma a tus ingresos).
	Tip           int        `gorm:"default:0" json:"tip"`
	AppointmentID *uuid.UUID `gorm:"type:uuid" json:"appointment_id,omitempty"`

	Lines     []OpenTabLine `gorm:"foreignKey:TabID" json:"lines"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (OpenTab) TableName() string { return "open_tabs" }

// OpenTabLine es algo pedido en una cuenta. ID lo genera la app (idempotente). El precio queda fijo al pedirlo: un
// cambio de precio en el menú no altera lo ya pedido.
type OpenTabLine struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	TabID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"tab_id"`
	ItemID    *uuid.UUID `gorm:"type:uuid" json:"item_id,omitempty"`
	Name      string     `gorm:"size:80;not null" json:"name"`
	Qty       float64    `gorm:"type:numeric(12,3);not null" json:"qty"`
	UnitPrice int        `gorm:"not null" json:"unit_price"`
	Removed   bool       `gorm:"default:false" json:"removed"`
	CreatedAt time.Time  `json:"created_at"`
}

func (OpenTabLine) TableName() string { return "open_tab_lines" }
