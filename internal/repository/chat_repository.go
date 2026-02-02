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
// Returns the latest message for each distinct conversation pair (Entity, User).
func (r *ChatRepository) FindAllConversations(userID uuid.UUID) ([]models.Message, error) {
	var messages []models.Message

	// DISTINCT ON (entity_id, user_id): Keeps the latest message for each unique pair.
	// This covers both cases:
	// 1. I am a User looking at my chats with various Entities.
	// 2. I am an Owner looking at my chats with various Users (for a specific Entity).

	// We filter messages where:
	// - I am the User involved (messages.user_id = ME)
	// - OR I own the Entity involved (entities.owner_id = ME)

	err := r.DB.Preload("Entity").Preload("User").
		Joins("LEFT JOIN entities ON messages.entity_id = entities.id").
		Distinct("ON (messages.entity_id, messages.user_id) messages.*").
		Where("messages.user_id = ? OR entities.owner_id = ?", userID, userID).
		Order("messages.entity_id, messages.user_id, messages.created_at DESC").
		Find(&messages).Error

	// Note: The resulting list contains the "head" of every conversation.
	// The frontend can now sort this list by created_at DESC to show Recent Chats first.

	return messages, err
}

func (r *ChatRepository) FindMessagesHistory(entityID, userID uuid.UUID, page, pageSize int) ([]models.Message, int64, error) {
	var messages []models.Message
	var total int64

	db := r.DB.Model(&models.Message{}).Where("entity_id = ? AND user_id = ?", entityID, userID)

	// Count total messages
	db.Count(&total)

	// Apply Pagination (Last messages first, but ordered ascending for the chat view)
	// Usually chat history is fetched from newest to oldest for pagination
	offset := (page - 1) * pageSize
	err := db.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&messages).Error

	return messages, total, err
}

func (r *ChatRepository) CreateMessage(message *models.Message) error {
	return r.DB.Create(message).Error
}
