package repository

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RefreshTokenRepository struct {
	DB *gorm.DB
}

func NewRefreshTokenRepository(db *gorm.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{DB: db}
}

func (r *RefreshTokenRepository) Create(token *models.RefreshToken) error {
	return r.DB.Create(token).Error
}

func (r *RefreshTokenRepository) FindByToken(token string) (*models.RefreshToken, error) {
	var refreshToken models.RefreshToken
	err := r.DB.Where("token = ?", token).First(&refreshToken).Error
	return &refreshToken, err
}

func (r *RefreshTokenRepository) Delete(token *models.RefreshToken) error {
	return r.DB.Delete(token).Error
}

func (r *RefreshTokenRepository) DeleteByUserID(userID uuid.UUID) error {
	return r.DB.Where("user_id = ?", userID).Delete(&models.RefreshToken{}).Error
}
