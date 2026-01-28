package services

import (
	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type UserService struct {
	Repo         *repository.UserRepository
	MediaService *MediaService
}

func NewUserService(repo *repository.UserRepository, mediaService *MediaService) *UserService {
	return &UserService{
		Repo:         repo,
		MediaService: mediaService,
	}
}

func (s *UserService) FindByID(id uuid.UUID) (*models.User, error) {
	user, err := s.Repo.FindByID(id)
	if err == nil && user != nil {
		if user.ProfilePictureURL != "" && user.ProfilePictureURL[0] == '/' {
			user.ProfilePictureURL = s.MediaService.BaseURL + user.ProfilePictureURL
		}
	}
	return user, err
}

func (s *UserService) UpdateProfilePicture(userID uuid.UUID, url string) error {
	user, err := s.Repo.FindByID(userID)
	if err != nil {
		return err
	}
	user.ProfilePictureURL = url
	return s.Repo.Update(user)
}
