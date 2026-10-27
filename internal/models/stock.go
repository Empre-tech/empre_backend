package models

import (
	"time"

	"github.com/google/uuid"
)

// Inventario fácil. Un StockItem es el estado de inventario de un producto del catálogo (ServiceItemID) o de un
// insumo que no se vende (lechuga, harina: ServiceItemID nulo y Name propio). Nada es obligatorio: un producto sin
// StockItem simplemente no lleva inventario. Todo lo que cambia una cantidad deja un StockMovement (no se reescribe).

// Tipos de ítem de inventario.
const (
	StockProduct = "product"
	StockSupply  = "supply"
)

// Motivos de un ajuste de existencias.
const (
	ReasonInitial  = "inicial"
	ReasonCount    = "conteo"
	ReasonDamaged  = "danado"
	ReasonExpired  = "vencido"
	ReasonOwnUse   = "consumo"
	ReasonLost     = "perdida"
	ReasonOther    = "otro"
	ReasonPurchase = "compra"
	ReasonSale     = "venta"
	ReasonUse      = "uso"
)

// ValidAdjustReason: los motivos que el dueño puede elegir al ajustar.
func ValidAdjustReason(r string) bool {
	switch r {
	case ReasonDamaged, ReasonExpired, ReasonOwnUse, ReasonLost, ReasonOther:
		return true
	}
	return false
}

// IsLossReason: motivos que cuentan como pérdida de plata.
func IsLossReason(r string) bool {
	return r == ReasonDamaged || r == ReasonExpired || r == ReasonLost
}

// Tipos de movimiento.
const (
	MoveSale         = "sale"
	MoveSaleVoid     = "sale_void"
	MovePurchase     = "purchase"
	MovePurchaseVoid = "purchase_void"
	MoveAdjust       = "adjust"
	MoveCount        = "count"
	// MoveUse: lo que una venta gastó de un insumo según la receta (fase 5); se revierte al anular la venta.
	MoveUse     = "use"
	MoveUseVoid = "use_void"
)

type StockItem struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	EntityID      uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:uniq_stock_product,priority:1" json:"entity_id"`
	ServiceItemID *uuid.UUID `gorm:"type:uuid;uniqueIndex:uniq_stock_product,priority:2" json:"service_item_id,omitempty"`
	Kind          string     `gorm:"type:varchar(10);not null;default:'product'" json:"kind"`
	// Name solo para insumos; los productos toman el nombre del catálogo.
	Name     string   `gorm:"size:80" json:"name"`
	Mode     string   `gorm:"type:varchar(6);not null;default:'light'" json:"mode"`
	Qty      float64  `gorm:"type:numeric(12,3);not null;default:0" json:"qty"`
	Level    string   `gorm:"type:varchar(4);not null;default:'ok'" json:"level"`
	Unit     string   `gorm:"size:20;not null;default:'unidad'" json:"unit"`
	PackSize *int     `gorm:"check:chk_stock_pack,pack_size IS NULL OR (pack_size >= 1 AND pack_size <= 10000)" json:"pack_size"`
	LowAt    *float64 `gorm:"type:numeric(12,3)" json:"low_at"`
	// CountedAt: cuándo se contó por última vez. Nulo = aún no se sabe cuántos hay (no se descuenta ni se avisa).
	CountedAt *time.Time `json:"counted_at"`
	Archived  bool       `gorm:"default:false;index" json:"archived"`
	// AlsoSupply: un producto que además se usa en otros ítems (el champú que se vende y con el que se lava). Así se
	// puede elegir como insumo sin crear un segundo registro: una sola existencia baja por venta y por uso.
	AlsoSupply bool `gorm:"default:false" json:"also_supply"`
	// Group: el grupo de un insumo (Carnes, Verduras, Empaques…), para encontrarlo entre cientos. Lo escribe el dueño;
	// vacío = sin grupo. La columna no se llama «group» porque es palabra reservada en SQL.
	Group string `gorm:"column:supply_group;size:40;not null;default:''" json:"group"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (StockItem) TableName() string { return "stock_items" }

// StockMovement es un cambio de existencias. ClientID lo hace idempotente (único por negocio). RefID apunta a la
// venta o compra que lo originó, para poder revertirlo al anular.
type StockMovement struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID    uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:uniq_stock_moves_client,priority:1" json:"entity_id"`
	ClientID    uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:uniq_stock_moves_client,priority:2" json:"client_id"`
	StockItemID uuid.UUID  `gorm:"type:uuid;not null;index:idx_stock_moves_item_time,priority:1" json:"stock_item_id"`
	Kind        string     `gorm:"type:varchar(14);not null" json:"kind"`
	Delta       float64    `gorm:"type:numeric(12,3);not null" json:"delta"`
	QtyAfter    float64    `gorm:"type:numeric(12,3);not null" json:"qty_after"`
	Reason      string     `gorm:"size:12" json:"reason"`
	RefID       *uuid.UUID `gorm:"type:uuid;index" json:"ref_id,omitempty"`
	OccurredAt  time.Time  `gorm:"not null;index:idx_stock_moves_item_time,priority:2" json:"occurred_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (StockMovement) TableName() string { return "stock_movements" }

// ExpenseLine es un producto o insumo comprado en un gasto de mercancía.
type ExpenseLine struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	ExpenseID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"expense_id"`
	ItemID      *uuid.UUID `gorm:"type:uuid" json:"item_id,omitempty"`
	StockItemID *uuid.UUID `gorm:"type:uuid" json:"stock_item_id,omitempty"`
	Name        string     `gorm:"size:80;not null" json:"name"`
	Qty         float64    `gorm:"type:numeric(12,3);not null" json:"qty"`
	UnitCost    int        `gorm:"not null" json:"unit_cost"`
}

func (ExpenseLine) TableName() string { return "expense_lines" }

// ProductSupply dice qué insumos usa un producto o plato (la carne de la bandeja paisa). Sin cantidades: sirve para
// avisar «se acabó la carne: afecta a la bandeja paisa» y para ver de un vistazo qué plato no se puede preparar.
type ProductSupply struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID  uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`
	ProductID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_product_supply,priority:1;index" json:"product_id"`
	SupplyID  uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_product_supply,priority:2;index" json:"supply_id"`
	// QtyPerUnit (opcional): cuánto de ese insumo gasta cada unidad vendida, en la unidad del insumo (150 g de carne
	// por bandeja). Con cantidad, el insumo baja solo al vender; sin ella, se lleva a mano con el semáforo.
	QtyPerUnit *float64  `gorm:"type:numeric(12,3)" json:"qty_per_unit"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ProductSupply) TableName() string { return "product_supplies" }
