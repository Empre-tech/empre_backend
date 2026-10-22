package services

import (
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type FavoriteService struct {
	Repo *repository.FavoriteRepository
}

func NewFavoriteService(repo *repository.FavoriteRepository) *FavoriteService {
	return &FavoriteService{Repo: repo}
}

func (s *FavoriteService) Add(userID, entityID uuid.UUID) error {
	return s.Repo.Add(userID, entityID)
}

func (s *FavoriteService) Remove(userID, entityID uuid.UUID) error {
	return s.Repo.Remove(userID, entityID)
}

func (s *FavoriteService) IsFavorite(userID, entityID uuid.UUID) (bool, error) {
	return s.Repo.IsFavorite(userID, entityID)
}

func (s *FavoriteService) FindEntitiesByUser(userID uuid.UUID, page, pageSize int) ([]models.Entity, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return s.Repo.FindEntitiesByUser(userID, page, pageSize)
}

// NovedadesWindow es cuánto hacia atrás se miran las novedades.
const NovedadesWindow = 30 * 24 * time.Hour

func (s *FavoriteService) Novedades(userID uuid.UUID, now time.Time) ([]repository.Novedad, error) {
	return s.Repo.Novedades(userID, now.Add(-NovedadesWindow))
}

func (s *FavoriteService) MarkNovedadesSeen(userID uuid.UUID, entityID *uuid.UUID, now time.Time) error {
	return s.Repo.MarkNovedadesSeen(userID, entityID, now)
}

func (s *FavoriteService) CountFollowers(entityID uuid.UUID) (int64, error) {
	return s.Repo.CountFollowers(entityID)
}
