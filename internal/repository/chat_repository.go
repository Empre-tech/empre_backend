package repository

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ChatRepository struct {
	DB *gorm.DB
}

func NewChatRepository(db *gorm.DB) *ChatRepository {
	return &ChatRepository{DB: db}
}

// FindAllConversations returns unique conversations for a user.
// Since we don't have a Conversation table yet, we aggregate messages.
func (r *ChatRepository) FindAllConversations(userID uuid.UUID) ([]models.Message, error) {
	var messages []models.Message

	// Complex query to get the last message of each unique (UserID, EntityID) pair
	// This is a simplified version for MVP
	err := r.DB.Where("user_id = ? OR exists (select 1 from entities where entities.id = messages.entity_id and entities.owner_id = ?)", userID, userID).
		Order("created_at DESC").
		Find(&messages).Error

	return messages, err
}

func (r *ChatRepository) FindMessagesHistory(entityID, userID uuid.UUID) ([]models.Message, error) {
	var messages []models.Message
	err := r.DB.Where("entity_id = ? AND user_id = ?", entityID, userID).
		Order("created_at ASC").
		Find(&messages).Error
	return messages, err
}
