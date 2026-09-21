package websocket

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"unicode/utf8"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxContentRunes is the maximum length (in characters) of a chat message.
const maxContentRunes = 1000

type Hub struct {
	// Registered clients mapped by UserID for private routing
	Clients map[uuid.UUID]*Client
	mu      sync.RWMutex

	// Register requests from the clients.
	Register chan *Client

	// Unregister requests from clients.
	Unregister chan *Client

	// Message channel
	Messages chan MessageEnvelope

	DB *gorm.DB
}

type MessageEnvelope struct {
	Data   []byte
	Client *Client
}

func NewHub(db *gorm.DB) *Hub {
	return &Hub{
		Messages:   make(chan MessageEnvelope),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Clients:    make(map[uuid.UUID]*Client),
		DB:         db,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			// A user has a single live connection: a newer one replaces the older one.
			if old, ok := h.Clients[client.UserID]; ok && old != client {
				old.closeSend()
			}
			h.Clients[client.UserID] = client
			h.mu.Unlock()
			log.Printf("User %s connected\n", client.UserID)

		case client := <-h.Unregister:
			h.mu.Lock()
			// Only remove the entry if it still points to this connection; otherwise a
			// late disconnect of an old socket would evict the user's newer connection.
			if current, ok := h.Clients[client.UserID]; ok && current == client {
				delete(h.Clients, client.UserID)
			}
			client.closeSend()
			h.mu.Unlock()
			log.Printf("User %s disconnected\n", client.UserID)

		case envelope := <-h.Messages:
			h.handleIncoming(envelope)
		}
	}
}

// handleIncoming validates, stores and routes a message received from a client.
func (h *Hub) handleIncoming(envelope MessageEnvelope) {
	var incoming models.Message
	if err := json.Unmarshal(envelope.Data, &incoming); err != nil {
		log.Println("Error unmarshaling message:", err)
		return
	}

	senderID := envelope.Client.UserID

	content := strings.TrimSpace(incoming.Content)
	if content == "" || utf8.RuneCountInString(content) > maxContentRunes {
		log.Printf("Rejected message from %s: empty or longer than %d characters\n", senderID, maxContentRunes)
		return
	}

	// Only the routing fields are taken from the client. Ids, timestamps, read flag and
	// nested entity/user objects are ignored so a client cannot forge them.
	msg := models.Message{
		SenderID:     senderID,
		EntityID:     incoming.EntityID,
		UserID:       incoming.UserID,
		SentByEntity: incoming.SentByEntity,
		Content:      content,
	}

	// Authorization: a customer can only write as themselves, and only the owner of the
	// business can write as the business.
	var entity models.Entity
	if err := h.DB.Select("id", "owner_id").First(&entity, "id = ?", msg.EntityID).Error; err != nil {
		log.Printf("Rejected message from %s: entity %s not found\n", senderID, msg.EntityID)
		return
	}
	if msg.SentByEntity {
		if entity.OwnerID != senderID {
			log.Printf("Rejected message from %s: not the owner of entity %s\n", senderID, msg.EntityID)
			return
		}
	} else if msg.UserID != senderID {
		log.Printf("Rejected message from %s: cannot write as user %s\n", senderID, msg.UserID)
		return
	}

	// Save to DB (without touching associations).
	if err := h.DB.Omit(clause.Associations).Create(&msg).Error; err != nil {
		log.Println("Error saving message to DB:", err)
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Println("Error marshaling message:", err)
		return
	}

	// Echo the stored message (with its real id and timestamp) back to the sender so the
	// client can replace its optimistic copy.
	h.mu.RLock()
	if sender, ok := h.Clients[senderID]; ok {
		select {
		case sender.Send <- data:
		default:
			log.Printf("Dropping echo to %s: send buffer full\n", senderID)
		}
	}
	h.mu.RUnlock()

	h.RouteMessage(&msg, data, senderID)
}

func (h *Hub) RouteMessage(msg *models.Message, rawData []byte, senderID uuid.UUID) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 1. Send to the Customer (UserID) if they are not the sender
	if msg.UserID != senderID {
		if client, ok := h.Clients[msg.UserID]; ok {
			h.deliver(client, rawData)
		}
	}

	// 2. Find the owner of the Entity (EntityID)
	var entity models.Entity
	if err := h.DB.Select("owner_id").First(&entity, "id = ?", msg.EntityID).Error; err == nil {
		// If owner is online AND they are not the sender, send to them
		if entity.OwnerID != senderID {
			if ownerClient, ok := h.Clients[entity.OwnerID]; ok {
				// Don't send twice if customer is the owner
				if entity.OwnerID != msg.UserID {
					h.deliver(ownerClient, rawData)
				}
			}
		}
	}
}

// deliver queues data for a client without ever blocking the hub on a slow connection.
func (h *Hub) deliver(client *Client, data []byte) {
	select {
	case client.Send <- data:
	default:
		log.Printf("Dropping message to %s: send buffer full\n", client.UserID)
	}
}
