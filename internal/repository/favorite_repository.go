package repository

import (
	"sort"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FavoriteRepository struct {
	DB *gorm.DB
}

func NewFavoriteRepository(db *gorm.DB) *FavoriteRepository {
	return &FavoriteRepository{DB: db}
}

// Add saves the (user, entity) pair if it isn't already there.
func (r *FavoriteRepository) Add(userID, entityID uuid.UUID) error {
	fav := models.Favorite{UserID: userID, EntityID: entityID}
	return r.DB.Where(models.Favorite{UserID: userID, EntityID: entityID}).FirstOrCreate(&fav).Error
}

func (r *FavoriteRepository) Remove(userID, entityID uuid.UUID) error {
	return r.DB.Where("user_id = ? AND entity_id = ?", userID, entityID).Delete(&models.Favorite{}).Error
}

func (r *FavoriteRepository) IsFavorite(userID, entityID uuid.UUID) (bool, error) {
	var count int64
	err := r.DB.Model(&models.Favorite{}).Where("user_id = ? AND entity_id = ?", userID, entityID).Count(&count).Error
	return count > 0, err
}

// FindEntitiesByUser returns the businesses a user has favorited, most
// recently favorited first.
func (r *FavoriteRepository) FindEntitiesByUser(userID uuid.UUID, page, pageSize int) ([]models.Entity, int64, error) {
	var favorites []models.Favorite
	var total int64

	db := r.DB.Model(&models.Favorite{}).Where("user_id = ?", userID)
	db.Count(&total)

	offset := (page - 1) * pageSize
	if err := db.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&favorites).Error; err != nil {
		return nil, 0, err
	}

	if len(favorites) == 0 {
		return []models.Entity{}, total, nil
	}

	ids := make([]uuid.UUID, 0, len(favorites))
	for _, f := range favorites {
		ids = append(ids, f.EntityID)
	}

	var entities []models.Entity
	err := r.DB.Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Subcategories").
		Where("entities.id IN ?", ids).Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}

	// The IN query doesn't preserve order: re-sort to match "most recently favorited first".
	order := make(map[uuid.UUID]int, len(ids))
	for i, id := range ids {
		order[id] = i
	}
	sort.Slice(entities, func(i, j int) bool { return order[entities[i].ID] < order[entities[j].ID] })

	return entities, total, nil
}
