package services

import (
	"errors"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type ReviewService struct {
	Repo *repository.ReviewRepository
}

func NewReviewService(repo *repository.ReviewRepository) *ReviewService {
	return &ReviewService{Repo: repo}
}

func (s *ReviewService) Upsert(entityID, userID uuid.UUID, rating int, comment string) (*models.Review, error) {
	if rating < 1 || rating > 5 {
		return nil, errors.New("la calificación debe estar entre 1 y 5")
	}
	return s.Repo.Upsert(entityID, userID, rating, comment)
}

func (s *ReviewService) FindByEntity(entityID uuid.UUID, page, pageSize int) ([]models.Review, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return s.Repo.FindByEntity(entityID, page, pageSize)
}

func (s *ReviewService) FindMine(entityID, userID uuid.UUID) (*models.Review, error) {
	return s.Repo.FindByEntityAndUser(entityID, userID)
}

func (s *ReviewService) Delete(entityID, userID uuid.UUID) error {
	return s.Repo.Delete(entityID, userID)
}

func (s *ReviewService) Summary(entityID uuid.UUID) (float64, int64, error) {
	return s.Repo.Summary(entityID)
}

// Summaries returns rating averages/counts for several entities at once (map view, lists).
func (s *ReviewService) Summaries(entityIDs []uuid.UUID) (map[uuid.UUID]repository.ReviewSummary, error) {
	return s.Repo.Summaries(entityIDs)
}
