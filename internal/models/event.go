package models

import (
	"time"

	"github.com/google/uuid"
)

// Event es un evento de uso de la app (búsqueda, perfil abierto, indicaciones…) para medir si Empre
// genera valor. NO guarda a la persona: solo un identificador anónimo de instalación (que la app puede
// borrar) y datos mínimos del evento. Se agrega, nunca se actualiza.
type Event struct {
	ID uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	// Name es uno de los nombres permitidos (ver services.EventSpecs).
	Name string `gorm:"size:40;not null;index" json:"name"`
	// AnonID identifica la instalación, no a la persona.
	AnonID string `gorm:"size:64;not null;index" json:"anon_id"`
	// EntityID es el negocio al que se refiere el evento, si aplica.
	EntityID *uuid.UUID `gorm:"type:uuid;index" json:"entity_id,omitempty"`
	// Props son datos mínimos y permitidos por evento (consulta normalizada, origen, etc.).
	Props map[string]any `gorm:"type:jsonb;serializer:json" json:"props,omitempty"`
	// CreatedAt lo pone el servidor: no se confía en la hora del teléfono.
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (Event) TableName() string {
	return "events"
}
