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
		s.populateProfileURL(user)
	}
	return user, err
}

func (s *UserService) UpdateProfilePicture(userID uuid.UUID, mediaID uuid.UUID) error {
	user, err := s.Repo.FindByID(userID)
	if err != nil {
		return err
	}
	user.ProfileMediaID = &mediaID
	return s.Repo.Update(user)
}

func (s *UserService) populateProfileURL(u *models.User) {
	if u == nil {
		return
	}
	if u.ProfileMedia != nil {
		s.MediaService.PopulateURL(u.ProfileMedia)
		u.ProfilePictureURL = u.ProfileMedia.URL
	} else if u.ProfileMediaID != nil {
		media, err := s.MediaService.Repo.FindByID(*u.ProfileMediaID)
		if err == nil {
			s.MediaService.PopulateURL(media)
			u.ProfilePictureURL = media.URL
		}
	}
}
