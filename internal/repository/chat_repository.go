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

// ConversationUnreadCount is how many unread messages a given viewer has
// waiting in one conversation (one entity + one customer).
type ConversationUnreadCount struct {
	EntityID uuid.UUID
	UserID   uuid.UUID
	Unread   int64
}

// UnreadCountsForViewer returns, for every conversation the viewer is part of
// (as the customer, or as the owner of the business), how many messages sent
// by the OTHER side are still unread. This is what "unread messages" should
// mean, as opposed to just counting how many conversations exist.
func (r *ChatRepository) UnreadCountsForViewer(viewerID uuid.UUID) ([]ConversationUnreadCount, error) {
	var rows []ConversationUnreadCount
	err := r.DB.Raw(`
		SELECT m.entity_id AS entity_id, m.user_id AS user_id, COUNT(*) AS unread
		FROM messages m
		JOIN entities e ON m.entity_id = e.id
		WHERE m.deleted_at IS NULL
		  AND m.is_read = false
		  AND (
		    (e.owner_id = ? AND m.sent_by_entity = false)
		    OR (m.user_id = ? AND m.sent_by_entity = true)
		  )
		GROUP BY m.entity_id, m.user_id
	`, viewerID, viewerID).Scan(&rows).Error
	return rows, err
}

// MarkConversationRead marks every unread message sent by the OTHER side of
// one conversation as read, from readerID's point of view: if readerID owns
// the business, it marks the customer's messages; otherwise it marks the
// business' messages (readerID is then the customer, targetUserID == readerID).
func (r *ChatRepository) MarkConversationRead(entityID, targetUserID, readerID uuid.UUID) error {
	var entity models.Entity
	if err := r.DB.Select("owner_id").First(&entity, "id = ?", entityID).Error; err != nil {
		return err
	}
	// true  -> reader is the customer: mark the business' messages as read.
	// false -> reader is the owner: mark the customer's messages as read.
	markSentByEntity := entity.OwnerID != readerID
	return r.DB.Model(&models.Message{}).
		Where("entity_id = ? AND user_id = ? AND sent_by_entity = ? AND is_read = false", entityID, targetUserID, markSentByEntity).
		Update("is_read", true).Error
}
