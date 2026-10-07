package repository

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SignalRepository struct {
	DB *gorm.DB
}

func NewSignalRepository(db *gorm.DB) *SignalRepository {
	return &SignalRepository{DB: db}
}

func (r *SignalRepository) Record(entityID uuid.UUID, typ models.SignalType) error {
	s := models.Signal{ID: uuid.New(), EntityID: entityID, Type: typ, CreatedAt: time.Now()}
	return r.DB.Create(&s).Error
}

type signalCountRow struct {
	EntityID uuid.UUID         `gorm:"column:entity_id"`
	Type     models.SignalType `gorm:"column:type"`
	Count    int64             `gorm:"column:count"`
}

// CountsForEntities returns, per entity and type, how many signals were
// recorded (one row per entity+type pair).
func (r *SignalRepository) CountsForEntities(entityIDs []uuid.UUID) ([]signalCountRow, error) {
	var rows []signalCountRow
	if len(entityIDs) == 0 {
		return rows, nil
	}
	err := r.DB.Model(&models.Signal{}).
		Select("entity_id, type, COUNT(*) as count").
		Where("entity_id IN ?", entityIDs).
		Group("entity_id, type").
		Scan(&rows).Error
	return rows, err
}
