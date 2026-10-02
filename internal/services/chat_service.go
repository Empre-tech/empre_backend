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

func (s *ChatService) FindAllConversations(userID uuid.UUID, page, pageSize int) ([]models.Message, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return s.repo.FindAllConversations(userID, page, pageSize)
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

// UnreadCounts returns, per conversation, how many unread messages the
// viewer has waiting from the other side.
func (s *ChatService) UnreadCounts(viewerID uuid.UUID) ([]repository.ConversationUnreadCount, error) {
	return s.repo.UnreadCountsForViewer(viewerID)
}

// MarkConversationRead marks the other side's messages in one conversation as
// read, from readerID's point of view. Best-effort: callers should log and
// continue rather than fail the request if this errors. Returns who sent
// those messages (to notify them in real time that they were read).
func (s *ChatService) MarkConversationRead(entityID, targetUserID, readerID uuid.UUID) (uuid.UUID, error) {
	return s.repo.MarkConversationRead(entityID, targetUserID, readerID)
}

// DistinctCustomerCount returns how many different customers have written to
// this business (used to gate the free trial by real demand).
func (s *ChatService) DistinctCustomerCount(entityID uuid.UUID) (int64, error) {
	return s.repo.CountDistinctCustomers(entityID)
}
