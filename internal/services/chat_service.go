package services

import (
	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type ChatService struct {
	repo *repository.ChatRepository
}

func NewChatService(repo *repository.ChatRepository) *ChatService {
	return &ChatService{repo: repo}
}

func (s *ChatService) FindAllConversations(userID uuid.UUID) ([]models.Message, error) {
	return s.repo.FindAllConversations(userID)
}

func (s *ChatService) FindMessagesHistory(entityID, userID uuid.UUID, page, pageSize int) ([]models.Message, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50 // Default for chat is usually larger
	}
	return s.repo.FindMessagesHistory(entityID, userID, page, pageSize)
}

func (s *ChatService) SendMessage(message *models.Message) error {
	return s.repo.CreateMessage(message)
}
