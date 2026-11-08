package services

import (
	"errors"
	"time"

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

// AIIntents son las intenciones que Empre AI puede responder en el chat (3.62): las de la app en src/lib/answers.ts,
// sin «unknown» (lo que no se reconoce no se responde). El texto es el que se guarda como contenido: lo que se ve
// en la lista de conversaciones.
var AIIntents = map[string]string{
	"services":     "Empre AI respondió con su catálogo publicado",
	"duration":     "Empre AI respondió con la duración publicada",
	"price":        "Empre AI respondió con el precio publicado",
	"hours":        "Empre AI respondió con su horario publicado",
	"offers":       "Empre AI respondió con sus ofertas vigentes",
	"availability": "Empre AI respondió con su disponibilidad de hoy",
	"book":         "Empre AI respondió cómo reservar",
	"delivery":     "Empre AI respondió si hace domicilios",
}

// AIAnswerWindow: la misma pregunta no se responde dos veces en este tiempo.
const AIAnswerWindow = 24 * time.Hour

var (
	ErrAIIntent       = errors.New("intención no válida")
	ErrNoConversation = errors.New("el cliente no le ha escrito a este negocio")
)

// AICustomIntent: Empre AI respondió con algo que el dueño le enseñó (3.62, F3).
const AICustomIntent = "custom"

// AIAnswerSummary es el texto corto de una intención válida.
func AIAnswerSummary(intent string) (string, bool) {
	if intent == AICustomIntent {
		return "Empre AI respondió con lo que enseñó el negocio", true
	}
	s, ok := AIIntents[intent]
	return s, ok
}

// SaveAIAnswer guarda que Empre AI respondió `intent` en la conversación del cliente con el negocio. Devuelve el
// mensaje y si es nuevo (false = esa misma pregunta ya se respondió en las últimas 24 h y se devuelve esa).
func (s *ChatService) SaveAIAnswer(entityID, customerID uuid.UUID, intent string, serviceID, answerID *uuid.UUID, now time.Time) (*models.Message, bool, error) {
	summary, ok := AIAnswerSummary(intent)
	if !ok {
		return nil, false, ErrAIIntent
	}
	off, err := s.repo.AIChatOff(entityID)
	if err != nil {
		return nil, false, err
	}
	if off {
		return nil, false, ErrAIChatOff
	}
	if intent == AICustomIntent {
		if answerID == nil {
			return nil, false, ErrAIIntent
		}
		belongs, err := s.repo.AnswerBelongs(entityID, *answerID)
		if err != nil {
			return nil, false, err
		}
		if !belongs {
			return nil, false, ErrAIIntent
		}
	} else {
		answerID = nil
	}
	wrote, err := s.repo.CustomerHasWritten(entityID, customerID)
	if err != nil {
		return nil, false, err
	}
	if !wrote {
		return nil, false, ErrNoConversation
	}
	if serviceID != nil {
		belongs, err := s.repo.ServiceBelongs(entityID, *serviceID)
		if err != nil {
			return nil, false, err
		}
		if !belongs {
			serviceID = nil
		}
	}
	prev, err := s.repo.FindRecentAIAnswer(entityID, customerID, intent, serviceID, answerID, now.Add(-AIAnswerWindow))
	if err != nil {
		return nil, false, err
	}
	if prev != nil {
		return prev, false, nil
	}
	msg := &models.Message{
		SenderID:     uuid.Nil,
		EntityID:     entityID,
		UserID:       customerID,
		SentByEntity: false,
		Kind:         "ai_answer",
		AIIntent:     intent,
		AIServiceID:  serviceID,
		AIAnswerID:   answerID,
		Content:      summary,
		IsRead:       true,
	}
	if err := s.repo.CreateMessage(msg); err != nil {
		return nil, false, err
	}
	return msg, true, nil
}

// ErrMessageNotFound: el mensaje no existe o no es una pregunta del cliente en esa conversación.
var ErrMessageNotFound = errors.New("mensaje no encontrado")

// MarkUnanswered: Empre AI no supo responder esta pregunta del cliente; el dueño la ve para enseñarle (3.62, F3).
func (s *ChatService) MarkUnanswered(entityID, customerID, messageID uuid.UUID) error {
	ok, err := s.repo.MarkUnanswered(entityID, customerID, messageID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrMessageNotFound
	}
	return nil
}
