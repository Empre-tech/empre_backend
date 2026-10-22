package services

import (
	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

// SignalService records telemetry events and derives a business's popularity
// score, which feeds the discovery ranking.
type SignalService struct {
	Repo *repository.SignalRepository
}

func NewSignalService(repo *repository.SignalRepository) *SignalService {
	return &SignalService{Repo: repo}
}

// Record logs an interaction best-effort (never blocks or fails the caller).
func (s *SignalService) Record(entityID uuid.UUID, typ models.SignalType) {
	_ = s.Repo.Record(entityID, typ)
}

// PopularityForEntities returns a weighted popularity score per entity:
// redeems weigh most, then saves, then views.
func (s *SignalService) PopularityForEntities(entityIDs []uuid.UUID) map[uuid.UUID]float64 {
	out := make(map[uuid.UUID]float64, len(entityIDs))
	rows, err := s.Repo.CountsForEntities(entityIDs)
	if err != nil {
		return out
	}
	for _, row := range rows {
		var weight float64
		switch row.Type {
		case models.SignalRedeem:
			weight = 3
		case models.SignalSave:
			weight = 2
		case models.SignalView:
			weight = 0.1
		}
		out[row.EntityID] += float64(row.Count) * weight
	}
	return out
}
