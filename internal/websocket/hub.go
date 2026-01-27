package websocket

import (
	"encoding/json"
	"log"
	"sync"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
			h.Clients[client.UserID] = client
			h.mu.Unlock()
			log.Printf("User %s connected\n", client.UserID)

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.Clients[client.UserID]; ok {
				delete(h.Clients, client.UserID)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("User %s disconnected\n", client.UserID)

		case envelope := <-h.Messages:
			var msg models.Message
			if err := json.Unmarshal(envelope.Data, &msg); err != nil {
				log.Println("Error unmarshaling message:", err)
				continue
			}

			// Security: Set SenderID from the authenticated connection
			msg.SenderID = envelope.Client.UserID

			// Save to DB
			if err := h.DB.Create(&msg).Error; err != nil {
				log.Println("Error saving message to DB:", err)
			}

			// Update raw data with correct SenderID if needed for clients
			newData, _ := json.Marshal(msg)

			// Route message
			h.routeMessage(&msg, newData)
		}
	}
}

func (h *Hub) routeMessage(msg *models.Message, rawData []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 1. Always try to send to the Customer (UserID)
	if client, ok := h.Clients[msg.UserID]; ok {
		client.Send <- rawData
	}

	// 2. Find the owner of the Entity (EntityID)
	var entity models.Entity
	if err := h.DB.Select("owner_id").First(&entity, "id = ?", msg.EntityID).Error; err == nil {
		// If owner is online, send to them
		if ownerClient, ok := h.Clients[entity.OwnerID]; ok {
			// Don't send twice if customer is the owner (unlikely but possible in testing)
			if entity.OwnerID != msg.UserID {
				ownerClient.Send <- rawData
			}
		}
	}
}
