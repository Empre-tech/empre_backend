package models

import (
	"time"

	"github.com/google/uuid"
)

// OfferAlert es el «Avísame» de Ofertas: la persona pide que le avisen cuando un negocio de una
// zona publique una oferta, de una categoría o de cualquiera (CategoryID vacío). Una fila por
// persona, zona y categoría; pedirlo otra vez no la duplica. LastSentAt evita mandar un aviso
// por cada oferta: como mucho uno cada AlertCooldown.
type OfferAlert struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:idx_offer_alert_user_zone_cat" json:"user_id"`
	ZoneID     uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:idx_offer_alert_user_zone_cat" json:"zone_id"`
	CategoryID *uuid.UUID `gorm:"type:uuid;uniqueIndex:idx_offer_alert_user_zone_cat" json:"category_id,omitempty"`
	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (OfferAlert) TableName() string {
	return "offer_alerts"
}
