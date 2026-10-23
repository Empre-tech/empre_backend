package models

import (
	"time"

	"github.com/google/uuid"
)

// ServiceItemStatus es el estado de un servicio del catálogo.
type ServiceItemStatus string

const (
	ServiceActive  ServiceItemStatus = "active"
	ServiceRetired ServiceItemStatus = "retired" // ya no se presta: no se muestra, se conserva el historial
)

// ServiceItem es algo que el negocio ofrece habitualmente (un servicio o producto), con su PRECIO HABITUAL.
// Es el catálogo: el negocio lo registra una vez y las ofertas y promociones lo referencian; una oferta nunca
// escribe aquí su precio temporal.
type ServiceItem struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`

	Name        string `gorm:"size:80;not null" json:"name"`
	Description string `gorm:"size:240" json:"description"`
	// Section agrupa el catálogo («Almuerzos», «Cortes», «Calzado»): texto libre del dueño, opcional. Vale para cualquier
	// tipo de negocio; la app le sugiere secciones según su categoría.
	Section string `gorm:"size:40" json:"section"`

	// Price es el precio habitual en pesos colombianos, sin decimales. Nil = el negocio no publica precio
	// (un dato desconocido NO es cero).
	Price *int `gorm:"check:chk_service_items_price,price IS NULL OR (price >= 0 AND price <= 10000000)" json:"price"`
	// PriceFrom indica «desde»: el precio es el mínimo y varía según el caso.
	PriceFrom bool `gorm:"default:false" json:"price_from"`
	// PriceUpdatedAt es cuándo se fijó o cambió el precio por última vez.
	PriceUpdatedAt *time.Time `json:"price_updated_at"`
	// ConfirmedAt es cuándo el dueño confirmó que el catálogo sigue vigente.
	ConfirmedAt *time.Time `json:"confirmed_at"`

	// DurationMin es cuánto ocupa en la agenda (5–480 min). Nulo = no publicada.
	DurationMin *int `gorm:"check:chk_service_items_duration,duration_min IS NULL OR (duration_min >= 5 AND duration_min <= 480)" json:"duration_min"`
	// Bookable: se puede reservar por Empre. Exige duración (CONVERSACIONES_Y_AGENDA.md, D2: una sola lista de servicios).
	Bookable bool `gorm:"default:false" json:"bookable"`

	Status   ServiceItemStatus `gorm:"type:varchar(20);not null;default:'active';index" json:"status"`
	Position int               `gorm:"default:0" json:"position"`
	// Cost es lo que le cuesta al negocio (pesos). SOLO lo ve el dueño, por el módulo de administración: no sale en
	// ninguna respuesta pública del catálogo (json:"-").
	Cost *int `gorm:"check:chk_service_items_cost,cost IS NULL OR (cost >= 0 AND cost <= 10000000)" json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ServiceItem) TableName() string {
	return "service_items"
}
