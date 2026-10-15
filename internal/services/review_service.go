package services

import (
	"errors"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type ReviewService struct {
	Repo      *repository.ReviewRepository
	ClaimRepo *repository.ClaimRepository
}

func NewReviewService(repo *repository.ReviewRepository, claimRepo *repository.ClaimRepository) *ReviewService {
	return &ReviewService{Repo: repo, ClaimRepo: claimRepo}
}

// Upsert guarda la reseña del usuario. Con `claimID`, la reseña queda ligada a
// un canje y se muestra como «Canje verificado»; el canje debe ser del mismo
// usuario y del mismo negocio, estar canjeado y haber sido confirmado por el
// cliente (el mismo criterio con el que se cuenta la popularidad).
func (s *ReviewService) Upsert(entityID, userID uuid.UUID, rating int, comment string, claimID *uuid.UUID) (*models.Review, error) {
	if rating < 1 || rating > 5 {
		return nil, errors.New("la calificación debe estar entre 1 y 5")
	}
	if claimID != nil {
		claim, err := s.ClaimRepo.FindByID(*claimID)
		if err != nil || claim.UserID != userID || claim.EntityID != entityID {
			return nil, errors.New("ese canje no existe o no es tuyo")
		}
		if claim.Status != models.ClaimRedeemed || !claim.VerifiedByClient {
			return nil, errors.New("el canje todavía no está confirmado")
		}
	}
	return s.Repo.Upsert(entityID, userID, rating, comment, claimID)
}

// Breakdown: reseñas por calificación (índice 0 = 1 estrella) y cuántas son verificadas.
func (s *ReviewService) Breakdown(entityID uuid.UUID) ([5]int64, int64, error) {
	return s.Repo.Breakdown(entityID)
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
