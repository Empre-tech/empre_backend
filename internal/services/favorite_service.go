package services

import (
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
