package repository

import (
	"empre_backend/internal/models"

	"gorm.io/gorm"
)

type PasswordResetRepository struct {
	DB *gorm.DB
}

func NewPasswordResetRepository(db *gorm.DB) *PasswordResetRepository {
	return &PasswordResetRepository{DB: db}
}

func (r *PasswordResetRepository) Create(token *models.PasswordResetToken) error {
	return r.DB.Create(token).Error
}

func (r *PasswordResetRepository) FindByToken(token string) (*models.PasswordResetToken, error) {
	var resetToken models.PasswordResetToken
	err := r.DB.Where("token = ?", token).First(&resetToken).Error
	return &resetToken, err
}

func (r *PasswordResetRepository) DeleteByUserID(userID interface{}) error {
	return r.DB.Where("user_id = ?", userID).Delete(&models.PasswordResetToken{}).Error
}

func (r *PasswordResetRepository) Delete(token *models.PasswordResetToken) error {
	return r.DB.Delete(token).Error
}
