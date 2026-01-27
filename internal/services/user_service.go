package services

import (
	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type UserService struct {
	Repo *repository.UserRepository
}

func NewUserService(repo *repository.UserRepository) *UserService {
	return &UserService{Repo: repo}
}

func (s *UserService) FindByID(id uuid.UUID) (*models.User, error) {
	return s.Repo.FindByID(id)
}

func (s *UserService) UpdateProfilePicture(userID uuid.UUID, url string) error {
	user, err := s.Repo.FindByID(userID)
	if err != nil {
		return err
	}
	user.ProfilePictureURL = url
	return s.Repo.Update(user)
}
