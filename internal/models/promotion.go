package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PromotionStatus es el ciclo de vida de una promoción (editable).
type PromotionStatus string

const (
	// PromotionDraft es un borrador: solo lo ve el dueño, no cuenta para el límite de activas.
	PromotionDraft    PromotionStatus = "draft"
	PromotionActive   PromotionStatus = "active"
	PromotionPaused   PromotionStatus = "paused"
	PromotionFinished PromotionStatus = "finished" // final: no se reactiva
)

func (s PromotionStatus) Valid() bool {
	switch s {
	case PromotionDraft, PromotionActive, PromotionPaused, PromotionFinished:
		return true
	}
	return false
}

// PromotionTemplate es la plantilla visual de una campaña. Vacía = promoción de antes del editor por plantillas.
type PromotionTemplate string

const (
	TemplateNone     PromotionTemplate = ""
	TemplateCelebrar PromotionTemplate = "celebracion"       // aniversario, fecha: comunicar sin descuento
	TemplateEstrella PromotionTemplate = "servicio_estrella" // foto grande y un servicio
	TemplatePaquete  PromotionTemplate = "paquete"           // dos o más servicios juntos
)

func (t PromotionTemplate) Valid() bool {
	switch t {
	case TemplateNone, TemplateCelebrar, TemplateEstrella, TemplatePaquete:
		return true
	}
	return false
}

// Promotion es una oferta sostenida (días o semanas), sin cupos ni reclamo/QR.
// A diferencia del relámpago (Offer), es editable y se puede pausar o
// finalizar. El cliente la muestra o la menciona; "Guardar" es opcional.
type Promotion struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`

	Title       string `gorm:"not null" json:"title"`
	Description string `json:"description"`
	// MotiveInternal es el porqué de la campaña, SOLO para el dueño (organizar y analizar): nunca sale en una
	// respuesta pública. PublicMessage es lo que ve el cliente.
	MotiveInternal string `gorm:"size:200" json:"-"`
	PublicMessage  string `gorm:"size:240" json:"public_message"`
	// Template es la plantilla visual elegida (ver PromotionTemplate).
	Template PromotionTemplate `gorm:"type:varchar(20);default:''" json:"template"`
	// Items son los servicios del catálogo que destaca, congelados al publicar.
	Items []PromotionItem `gorm:"foreignKey:PromotionID" json:"-"`
	// Precio o porcentaje de descuento (al menos uno); valor normal opcional.
	Price       string `json:"price"`
	Discount    string `json:"discount"`
	NormalPrice string `json:"normal_price"`

	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `gorm:"index" json:"ends_at"`
	Conditions string    `json:"conditions"`

	// Foto opcional (Media). Queda preparada para cuando se conecte la subida.
	PhotoMediaID *uuid.UUID `gorm:"type:uuid" json:"photo_media_id,omitempty"`
	// Etiquetas (producto/servicio) separadas por coma; alimentan la búsqueda.
	Tags   string          `json:"tags"`
	Status PromotionStatus `gorm:"type:varchar(20);default:'active'" json:"status"`

	// Entidad dueña (para distancia y categoría en el listado de ofertas).
	Entity Entity `gorm:"foreignKey:EntityID" json:"-"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
