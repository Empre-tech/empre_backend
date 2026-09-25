package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

const expoPushURL = "https://exp.host/--/api/v2/push/send"

type PushService struct {
	Repo   *repository.PushTokenRepository
	Client *http.Client
}

func NewPushService(repo *repository.PushTokenRepository) *PushService {
	return &PushService{
		Repo:   repo,
		Client: &http.Client{Timeout: 10 * time.Second},
	}
}

// SaveToken registers (or reassigns) a device's Expo push token for a user.
func (s *PushService) SaveToken(userID uuid.UUID, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token is required")
	}
	return s.Repo.Upsert(userID, token)
}

// RemoveToken unregisters a device token (used on logout).
func (s *PushService) RemoveToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token is required")
	}
	return s.Repo.Delete(token)
}

// Notify pushes a notification to every device registered for a user.
// Best-effort and fire-and-forget: a push failure must never break or delay
// the action that triggered it (sending a chat message, leaving a review,
// etc.), so this never returns an error and does its work in a goroutine.
func (s *PushService) Notify(userID uuid.UUID, title, body string, data map[string]string) {
	tokens, err := s.Repo.FindTokensByUser(userID)
	if err != nil {
		log.Println("push: could not load tokens for user", userID, err)
		return
	}
	if len(tokens) == 0 {
		return
	}
	go s.send(tokens, title, body, data)
}

type expoMessage struct {
	To    string            `json:"to"`
	Title string            `json:"title"`
	Body  string            `json:"body"`
	Data  map[string]string `json:"data,omitempty"`
	Sound string            `json:"sound,omitempty"`
}

type expoPushResponse struct {
	Data []struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
}

func (s *PushService) send(tokens []string, title, body string, data map[string]string) {
	messages := make([]expoMessage, 0, len(tokens))
	for _, token := range tokens {
		messages = append(messages, expoMessage{To: token, Title: title, Body: body, Data: data, Sound: "default"})
	}

	payload, err := json.Marshal(messages)
	if err != nil {
		log.Println("push: could not encode payload:", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, expoPushURL, bytes.NewReader(payload))
	if err != nil {
		log.Println("push: could not build request:", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		log.Println("push: request failed:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Println("push: unexpected status from Expo:", resp.StatusCode)
		return
	}

	var parsed expoPushResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return
	}
	// A token that is no longer valid (app uninstalled, etc.) comes back as
	// DeviceNotRegistered: drop it so we stop trying to notify that device.
	for i, ticket := range parsed.Data {
		if ticket.Status == "error" && ticket.Details.Error == "DeviceNotRegistered" && i < len(tokens) {
			if err := s.Repo.Delete(tokens[i]); err != nil {
				log.Println("push: could not remove stale token:", err)
			}
		}
	}
}
