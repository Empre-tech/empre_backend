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
func (r *ChatRepository) FindAllConversations(userID uuid.UUID, page, pageSize int) ([]models.Message, int64, error) {
	var messages []models.Message
	var total int64

	// Count unique conversation pairs
	// We need a subquery or a complex count for DISTINCT ON
	r.DB.Model(&models.Message{}).
		Joins("LEFT JOIN entities ON messages.entity_id = entities.id").
		Where("messages.user_id = ? OR entities.owner_id = ?", userID, userID).
		Distinct("messages.entity_id, messages.user_id").
		Count(&total)

	offset := (page - 1) * pageSize
	err := r.DB.Preload("Entity").Preload("User").
		Joins("LEFT JOIN entities ON messages.entity_id = entities.id").
		Distinct("ON (messages.entity_id, messages.user_id) messages.*").
		Where("messages.user_id = ? OR entities.owner_id = ?", userID, userID).
		Order("messages.entity_id, messages.user_id, messages.created_at DESC").
		Limit(pageSize).Offset(offset).
		Find(&messages).Error

	return messages, total, err
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
