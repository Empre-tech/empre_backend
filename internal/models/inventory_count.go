package models

import (
	"time"

	"github.com/google/uuid"
)

// InventoryCount es un conteo de una sección del inventario (tablero «Inventario», teléfonos 9–12): cuándo se contó,
// cuántas filas y cuántas cambiaron. Su ID es el client_id que manda la app: reenviar el mismo conteo (sin señal, un
// doble toque) no lo aplica dos veces. Con él se sabe cuándo se contó cada sección por última vez.
type InventoryCount struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	EntityID  uuid.UUID `gorm:"type:uuid;not null;index:idx_inventory_counts_entity,priority:1" json:"entity_id"`
	Tab       string    `gorm:"type:varchar(8);not null" json:"tab"`
	Section   string    `gorm:"size:60;not null;default:''" json:"section"`
	Lines     int       `gorm:"not null" json:"lines"`
	Changed   int       `gorm:"not null" json:"changed"`
	CountedAt time.Time `gorm:"not null;index:idx_inventory_counts_entity,priority:2" json:"counted_at"`
}

func (InventoryCount) TableName() string { return "inventory_counts" }
