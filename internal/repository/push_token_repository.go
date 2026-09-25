package repository

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PushTokenRepository struct {
	DB *gorm.DB
}

func NewPushTokenRepository(db *gorm.DB) *PushTokenRepository {
	return &PushTokenRepository{DB: db}
}

// Upsert registers a device token for a user, or reassigns it if the same
// token was previously registered under a different account (shared device).
func (r *PushTokenRepository) Upsert(userID uuid.UUID, token string) error {
	pt := models.PushToken{UserID: userID, Token: token}
	return r.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token"}},
		DoUpdates: clause.AssignmentColumns([]string{"user_id", "updated_at"}),
	}).Create(&pt).Error
}

// Delete removes one token (e.g. on logout, so the device stops receiving
// notifications for an account no longer signed in on it).
func (r *PushTokenRepository) Delete(token string) error {
	return r.DB.Where("token = ?", token).Delete(&models.PushToken{}).Error
}

// FindTokensByUser returns every device token registered for a user.
func (r *PushTokenRepository) FindTokensByUser(userID uuid.UUID) ([]string, error) {
	var tokens []string
	err := r.DB.Model(&models.PushToken{}).Where("user_id = ?", userID).Pluck("token", &tokens).Error
	return tokens, err
}
