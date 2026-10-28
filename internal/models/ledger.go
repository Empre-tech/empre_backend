package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Módulo de administración del negocio (docs: «Empre — Módulo de administración del negocio»). Los montos son pesos
// colombianos enteros. Todo lo que escribe el teléfono lleva un ClientID generado en el dispositivo: reenviar tras
// una caída de señal no duplica nada (índice único por negocio).

// Medios de pago. «credit» es fiado (venta) o compra a crédito (gasto).
const (
	PayCash     = "cash"
	PayTransfer = "transfer"
	PayCard     = "card"
	PayCredit   = "credit"
)

func ValidPayMethod(m string) bool {
	switch m {
	case PayCash, PayTransfer, PayCard, PayCredit:
		return true
	}
	return false
}

// Origen de una venta o un gasto.
const (
	SourceManual      = "manual"
	SourceVoice       = "voice"
	SourceAppointment = "appointment"
	SourceOfferClaim  = "offer_claim"
)

// Estado de una venta o un gasto: nunca se borran, se anulan con motivo.
const (
	LedgerActive = "active"
	LedgerVoided = "voided"
)

// Tipos de tercero.
const (
	PartyCustomer = "customer"
	PartySupplier = "supplier"
)

// Dirección de un abono.
const (
	DebtReceivable = "receivable" // me pagan un fiado
	DebtPayable    = "payable"    // yo pago una deuda con un proveedor
)

// Party es un cliente de fiado o un proveedor. UserID enlaza con una cuenta de Empre si esa persona la tiene.
type Party struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID    uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:uniq_parties_client,priority:1" json:"entity_id"`
	ClientID    uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:uniq_parties_client,priority:2" json:"client_id"`
	Kind        string     `gorm:"type:varchar(10);not null;default:'customer'" json:"kind"`
	Name        string     `gorm:"size:80;not null" json:"name"`
	NameKey     string     `gorm:"size:80;not null;index" json:"-"`
	Phone       string     `gorm:"size:20" json:"phone"`
	UserID      *uuid.UUID `gorm:"type:uuid" json:"user_id,omitempty"`
	CreditLimit *int       `json:"credit_limit"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Party) TableName() string { return "parties" }

// Sale es una venta. Si hay líneas, cada una guarda el nombre, el precio y el costo del momento: cambiar el catálogo
// después no reescribe la historia. Una venta sin líneas («vendí 50 mil») es válida.
type Sale struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID   uuid.UUID  `gorm:"type:uuid;not null;index:idx_sales_entity_time,priority:1;uniqueIndex:uniq_sales_client,priority:1" json:"entity_id"`
	ClientID   uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:uniq_sales_client,priority:2" json:"client_id"`
	OccurredAt time.Time  `gorm:"not null;index:idx_sales_entity_time,priority:2" json:"occurred_at"`
	Total      int        `gorm:"not null;check:chk_sales_total,total > 0 AND total <= 100000000" json:"total"`
	Method     string     `gorm:"type:varchar(10);not null" json:"method"`
	PartyID    *uuid.UUID `gorm:"type:uuid;index" json:"party_id,omitempty"`
	Party      *Party     `gorm:"foreignKey:PartyID" json:"party,omitempty"`
	Source     string     `gorm:"type:varchar(20);not null;default:'manual'" json:"source"`
	SourceID   *uuid.UUID `gorm:"type:uuid" json:"source_id,omitempty"`
	Status     string     `gorm:"type:varchar(10);not null;default:'active';index" json:"status"`
	VoidedAt   *time.Time `json:"voided_at,omitempty"`
	VoidReason string     `gorm:"size:120" json:"void_reason,omitempty"`
	Note       string     `gorm:"size:200" json:"note"`
	Lines      []SaleLine `gorm:"foreignKey:SaleID" json:"lines"`

	CreatedAt time.Time `json:"created_at"`
}

func (Sale) TableName() string { return "sales" }

// SaleLine es un renglón de una venta.
type SaleLine struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	SaleID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"sale_id"`
	ItemID    *uuid.UUID `gorm:"type:uuid;index" json:"item_id,omitempty"`
	Name      string     `gorm:"size:80;not null" json:"name"`
	Qty       float64    `gorm:"type:numeric(10,3);not null" json:"qty"`
	UnitPrice int        `gorm:"not null" json:"unit_price"`
	UnitCost  *int       `json:"unit_cost,omitempty"`
}

func (SaleLine) TableName() string { return "sale_lines" }

// Expense es un gasto. Con método «credit» es una compra a crédito: queda como deuda con el proveedor.
type Expense struct {
	ID         uuid.UUID     `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID   uuid.UUID     `gorm:"type:uuid;not null;index:idx_expenses_entity_time,priority:1;uniqueIndex:uniq_expenses_client,priority:1" json:"entity_id"`
	ClientID   uuid.UUID     `gorm:"type:uuid;not null;uniqueIndex:uniq_expenses_client,priority:2" json:"client_id"`
	OccurredAt time.Time     `gorm:"not null;index:idx_expenses_entity_time,priority:2" json:"occurred_at"`
	Amount     int           `gorm:"not null;check:chk_expenses_amount,amount > 0 AND amount <= 100000000" json:"amount"`
	Category   string        `gorm:"size:30;not null;default:'otro'" json:"category"`
	Method     string        `gorm:"type:varchar(10);not null" json:"method"`
	PartyID    *uuid.UUID    `gorm:"type:uuid;index" json:"party_id,omitempty"`
	Party      *Party        `gorm:"foreignKey:PartyID" json:"party,omitempty"`
	Source     string        `gorm:"type:varchar(20);not null;default:'manual'" json:"source"`
	Status     string        `gorm:"type:varchar(10);not null;default:'active';index" json:"status"`
	VoidedAt   *time.Time    `json:"voided_at,omitempty"`
	VoidReason string        `gorm:"size:120" json:"void_reason,omitempty"`
	Note       string        `gorm:"size:200" json:"note"`
	Lines      []ExpenseLine `gorm:"foreignKey:ExpenseID" json:"lines,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

func (Expense) TableName() string { return "expenses" }

// DebtPayment es un abono: un cliente le paga un fiado al negocio o el negocio le paga a un proveedor.
type DebtPayment struct {
	ID         uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID   uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:uniq_debt_payments_client,priority:1" json:"entity_id"`
	ClientID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_debt_payments_client,priority:2" json:"client_id"`
	PartyID    uuid.UUID `gorm:"type:uuid;not null;index" json:"party_id"`
	Direction  string    `gorm:"type:varchar(12);not null" json:"direction"`
	Amount     int       `gorm:"not null;check:chk_debt_payments_amount,amount > 0 AND amount <= 100000000" json:"amount"`
	Method     string    `gorm:"type:varchar(10);not null" json:"method"`
	OccurredAt time.Time `gorm:"not null" json:"occurred_at"`
	Note       string    `gorm:"size:200" json:"note"`

	CreatedAt time.Time `json:"created_at"`
}

func (DebtPayment) TableName() string { return "debt_payments" }

// Estados de un borrador de voz.
const (
	VoicePending   = "pending"
	VoiceConfirmed = "confirmed"
	VoiceDiscarded = "discarded"
)

// VoiceDraft guarda lo dictado (solo el texto, nunca el audio) y lo que se entendió, hasta que el dueño confirma.
type VoiceDraft struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID    uuid.UUID  `gorm:"type:uuid;not null;index:idx_voice_entity_time,priority:1" json:"entity_id"`
	RawText     string     `gorm:"size:600;not null" json:"raw_text"`
	Parsed      string     `gorm:"type:text" json:"-"`
	Status      string     `gorm:"type:varchar(10);not null;default:'pending'" json:"status"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt   time.Time  `gorm:"index:idx_voice_entity_time,priority:2" json:"created_at"`
}

func (VoiceDraft) TableName() string { return "voice_drafts" }
