package repository

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MediaRepository struct {
	DB *gorm.DB
}

func NewMediaRepository(db *gorm.DB) *MediaRepository {
	return &MediaRepository{DB: db}
}

func (r *MediaRepository) Create(media *models.Media) error {
	return r.DB.Create(media).Error
}

func (r *MediaRepository) FindByID(id uuid.UUID) (*models.Media, error) {
	var media models.Media
	err := r.DB.First(&media, "id = ?", id).Error
	return &media, err
}

func (r *MediaRepository) CreateEntityPhoto(photo *models.EntityPhoto) error {
	return r.DB.Create(photo).Error
}
