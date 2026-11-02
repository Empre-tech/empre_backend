package services

import (
	"errors"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrMessageItem: el producto del mensaje no existe, no es de ese negocio o ya no se ofrece.
var ErrMessageItem = errors.New("ese producto no está en el catálogo del negocio")

// MessageItemCard arma la tarjeta de un producto o servicio para un mensaje del chat.
// Lo hace el servidor (no se confía en lo que mande la app): el ítem tiene que ser
// del mismo negocio de la conversación y estar activo. Una variante sale con el
// nombre de su producto y su opción («Camiseta polo (M)») y, si no tiene fotos
// propias, con la foto de su producto.
func MessageItemCard(db *gorm.DB, entityID, itemID uuid.UUID) (*models.MessageItem, error) {
	var item models.ServiceItem
	err := db.Where("id = ? AND entity_id = ? AND status = ?", itemID, entityID, models.ServiceActive).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMessageItem
	}
	if err != nil {
		return nil, err
	}
	name, photos, section := item.Name, item.Photos, item.Section
	if item.ParentID != nil {
		var parent models.ServiceItem
		if err := db.Where("id = ? AND entity_id = ?", *item.ParentID, entityID).First(&parent).Error; err == nil {
			if item.VariantLabel != "" {
				name = parent.Name + " (" + item.VariantLabel + ")"
			}
			if len(photos) == 0 {
				photos = parent.Photos
			}
			section = firstNonEmpty(section, parent.Section)
		}
	}
	card := &models.MessageItem{ID: item.ID, Name: name, Price: item.Price, PriceFrom: item.PriceFrom, Section: section, Kind: item.Kind}
	if len(photos) > 0 {
		card.Photo = photos[0]
	}
	return card, nil
}
