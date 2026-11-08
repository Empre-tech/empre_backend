package models

import (
	"time"

	"github.com/google/uuid"
)

// BusinessAnswer es algo que el dueño le enseñó a Empre AI (3.62, F3): «No hacemos domicilio, pero te lo guardamos»
// o «Sí atendemos niños desde los 3 años». Con Intent (una de services.AIIntents) completa o corrige esa respuesta;
// con Intent vacío es una pregunta propia del negocio y se reconoce por Keywords. Lo usan el chat y «Pregúntale».
type BusinessAnswer struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`
	Intent   string    `gorm:"type:varchar(20)" json:"intent"`
	// Question es la pregunta como la escribió un cliente o el dueño; Keywords, las palabras con que se reconoce.
	Question  string    `gorm:"size:200" json:"question"`
	Keywords  []string  `gorm:"type:jsonb;serializer:json" json:"keywords"`
	Answer    string    `gorm:"size:500;not null" json:"answer"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
