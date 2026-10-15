package repository

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type OfferRepository struct {
	DB *gorm.DB
}

func NewOfferRepository(db *gorm.DB) *OfferRepository {
	return &OfferRepository{DB: db}
}

func (r *OfferRepository) Create(offer *models.Offer) error {
	return r.DB.Create(offer).Error
}

// Update persists quick-action changes (ends_at, radius, slots). Callers must
// never mutate Title/Price/Conditions through this — those are immutable.
func (r *OfferRepository) Update(offer *models.Offer) error {
	return r.DB.Save(offer).Error
}

func (r *OfferRepository) FindByID(id uuid.UUID) (*models.Offer, error) {
	var offer models.Offer
	err := r.DB.First(&offer, "id = ?", id).Error
	return &offer, err
}

// FindByIDs devuelve las ofertas pedidas, por id, en una sola consulta.
func (r *OfferRepository) FindByIDs(ids []uuid.UUID) (map[uuid.UUID]*models.Offer, error) {
	out := make(map[uuid.UUID]*models.Offer, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var offers []models.Offer
	if err := r.DB.Where("id IN ?", ids).Find(&offers).Error; err != nil {
		return nil, err
	}
	for i := range offers {
		out[offers[i].ID] = &offers[i]
	}
	return out, nil
}

func (r *OfferRepository) FindByEntity(entityID uuid.UUID) ([]models.Offer, error) {
	var offers []models.Offer
	err := r.DB.Where("entity_id = ?", entityID).Order("created_at DESC").Find(&offers).Error
	return offers, err
}

// CountActiveByType returns how many offers of a given type are currently
// active for a business (not ended, not soft-deleted). Used to enforce the
// per-type active limits.
func (r *OfferRepository) CountActiveByType(entityID uuid.UUID, typ models.OfferType, now time.Time) (int64, error) {
	var count int64
	err := r.DB.Model(&models.Offer{}).
		Where("entity_id = ? AND type = ? AND ends_at > ?", entityID, typ, now).
		Count(&count).Error
	return count, err
}

// ReserveSlot atomically increments used_slots only if the offer is still
// active and has room. Returns true when a slot was actually reserved.
func (r *OfferRepository) ReserveSlot(offerID uuid.UUID, now time.Time) (bool, error) {
	res := r.DB.Model(&models.Offer{}).
		Where("id = ? AND used_slots < total_slots AND ends_at > ?", offerID, now).
		Update("used_slots", gorm.Expr("used_slots + 1"))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ReleaseSlot decrements used_slots (never below zero) when a claim is
// canceled or expires unused.
func (r *OfferRepository) ReleaseSlot(offerID uuid.UUID) error {
	return r.DB.Model(&models.Offer{}).
		Where("id = ? AND used_slots > 0", offerID).
		Update("used_slots", gorm.Expr("used_slots - 1")).Error
}

// FindActive devuelve los relámpagos activos (con su entidad) para el listado
// de la pestaña Ofertas.
func (r *OfferRepository) FindActive(now time.Time) ([]models.Offer, error) {
	var offers []models.Offer
	err := r.DB.Preload("Entity.Category").
		Where("ends_at > ? AND used_slots < total_slots", now).
		Order("ends_at ASC").
		Find(&offers).Error
	return offers, err
}

// FindActiveForEntities returns, for each entity id, its soonest-ending active
// offer (or nil when it has none). Used by discovery to flag the "rayo".
func (r *OfferRepository) FindActiveForEntities(entityIDs []uuid.UUID, now time.Time) (map[uuid.UUID]*models.Offer, error) {
	result := make(map[uuid.UUID]*models.Offer, len(entityIDs))
	if len(entityIDs) == 0 {
		return result, nil
	}

	var offers []models.Offer
	err := r.DB.
		Where("entity_id IN ? AND ends_at > ? AND used_slots < total_slots", entityIDs, now).
		Order("ends_at ASC").
		Find(&offers).Error
	if err != nil {
		return nil, err
	}

	for i := range offers {
		if _, ok := result[offers[i].EntityID]; !ok {
			o := offers[i]
			result[offers[i].EntityID] = &o
		}
	}
	return result, nil
}
