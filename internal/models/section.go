package models

import (
	"time"

	"github.com/google/uuid"
)

// Secciones del catálogo. Las administra el equipo de Empre: el dueño de un negocio no crea secciones, elige de este
// catálogo global. Una sección es de lo que se vende (productos y servicios) o de insumos; «Bebidas» puede existir en
// los dos tipos y son dos filas distintas.
const (
	SectionSell   = "sell"
	SectionSupply = "supply"
)

// ValidSectionKind dice si k es un tipo de sección conocido.
func ValidSectionKind(k string) bool { return k == SectionSell || k == SectionSupply }

// SectionOther es el nombre de la sección que todo negocio tiene, en los dos tipos, para lo que no encaja en otra.
const SectionOther = "Otros"

type Section struct {
	ID   uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Kind string    `gorm:"type:varchar(10);not null;uniqueIndex:uniq_section_name,priority:1" json:"kind"`
	Name string    `gorm:"size:40;not null;uniqueIndex:uniq_section_name,priority:2" json:"name"`
	// Icon: nombre de un ícono de Ionicons («shirt-outline»). Lo pone el servidor según la sección.
	Icon      string    `gorm:"size:48;not null;default:''" json:"icon"`
	CreatedAt time.Time `json:"created_at"`
}

func (Section) TableName() string { return "sections" }

// SubcategorySection: las secciones más comunes de una subcategoría, en el orden en que se muestran. Es la plantilla
// con la que arranca cualquier negocio de esa subcategoría; se recalcula al arrancar el backend.
type SubcategorySection struct {
	SubcategoryID uuid.UUID `gorm:"type:uuid;primaryKey" json:"subcategory_id"`
	SectionID     uuid.UUID `gorm:"type:uuid;primaryKey;index" json:"section_id"`
	Position      int       `gorm:"not null;default:0" json:"position"`
}

func (SubcategorySection) TableName() string { return "subcategory_sections" }

// EntitySection: una sección del catálogo que el negocio agregó aunque no venga en la plantilla de su subcategoría.
type EntitySection struct {
	EntityID  uuid.UUID `gorm:"type:uuid;primaryKey" json:"entity_id"`
	SectionID uuid.UUID `gorm:"type:uuid;primaryKey;index" json:"section_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (EntitySection) TableName() string { return "entity_sections" }

// Estados de una petición de sección.
const (
	SectionRequestOpen      = "open"
	SectionRequestAdded     = "added"
	SectionRequestDismissed = "dismissed"
)

// SectionRequest: un negocio pidió una sección que no está en el catálogo. La revisa el equipo en el panel de admin.
type SectionRequest struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"entity_id"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null" json:"user_id"`
	Kind       string     `gorm:"type:varchar(10);not null" json:"kind"`
	Name       string     `gorm:"size:40;not null" json:"name"`
	Status     string     `gorm:"type:varchar(10);not null;default:'open';index" json:"status"`
	SectionID  *uuid.UUID `gorm:"type:uuid" json:"section_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

func (SectionRequest) TableName() string { return "section_requests" }
