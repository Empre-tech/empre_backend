package services

import (
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type MediaService struct {
	Repo           *repository.MediaRepository
	StorageService *StorageService
}

func NewMediaService(repo *repository.MediaRepository, storageService *StorageService) *MediaService {
	return &MediaService{
		Repo:           repo,
		StorageService: storageService,
	}
}

func (s *MediaService) UploadAndMap(folder string, filename string, body io.Reader, contentType string, size int64) (*models.Media, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	s3Key := fmt.Sprintf("%s/%s%s", folder, uuid.New().String(), ext)

	// 1. Physical Upload
	err := s.StorageService.UploadFile(s3Key, body, contentType)
	if err != nil {
		return nil, err
	}

	// 2. Database Mapping
	media := &models.Media{
		S3Key:        s3Key,
		OriginalName: filename,
		ContentType:  contentType,
		Size:         size,
	}

	if err := s.Repo.Create(media); err != nil {
		return nil, err
	}

	return media, nil
}

func (s *MediaService) GetFile(mediaID uuid.UUID) (io.ReadCloser, string, error) {
	media, err := s.Repo.FindByID(mediaID)
	if err != nil {
		return nil, "", err
	}

	return s.StorageService.GetFile(media.S3Key)
}

func (s *MediaService) LinkToEntity(entityID, mediaID uuid.UUID, order int) error {
	photo := &models.EntityPhoto{
		EntityID: entityID,
		MediaID:  mediaID,
		Order:    order,
	}
	return s.Repo.CreateEntityPhoto(photo)
}
