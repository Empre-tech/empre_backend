package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AIService is a thin client for OpenCode Zen (https://opencode.ai/zen), a
// gateway that exposes several model providers (Claude, GPT, Gemini, ...)
// behind one API key. We use its Anthropic-compatible endpoint
// (`/v1/messages`), which speaks the same request/response shape as
// Anthropic's own Messages API — including tool use, which is how we get
// structured data (category, hours, etc.) back out of a free-form chat.
type AIService struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

func NewAIService(apiKey, baseURL, model string) *AIService {
	return &AIService{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
		Client:  &http.Client{Timeout: 45 * time.Second},
	}
}

// Enabled reports whether an API key was configured. Callers should check
// this and return a clear error to the client instead of calling Chat.
func (s *AIService) Enabled() bool {
	return s.APIKey != ""
}

type AIRole string

const (
	AIRoleUser      AIRole = "user"
	AIRoleAssistant AIRole = "assistant"
)

// AIMessage is one turn of the conversation, in the shape the frontend sends
// and stores (this backend is stateless across requests: the client resends
// the whole conversation each turn).
type AIMessage struct {
	Role    AIRole `json:"role"`
	Content string `json:"content"`
}

// AITool mirrors the Anthropic Messages API tool definition shape.
type AITool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// AIToolCall is a structured "function call" the model made.
type AIToolCall struct {
	Name  string
	Input json.RawMessage
}

// AIReply is what one chat turn against the model produced: the plain-text
// part of the answer (if any) plus any tool calls it made.
type AIReply struct {
	Text      string
	ToolCalls []AIToolCall
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []AITool           `json:"tools,omitempty"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat sends the full conversation (system prompt + history + optional tool
// definitions) and returns the model's reply. Unlike PushService.Notify,
// this is NOT fire-and-forget: the person is waiting on this to answer them,
// so errors are returned to the caller instead of only logged.
func (s *AIService) Chat(systemPrompt string, messages []AIMessage, tools []AITool) (*AIReply, error) {
	if !s.Enabled() {
		return nil, errors.New("ai: no hay una API key de IA configurada (AI_API_KEY)")
	}

	reqMessages := make([]anthropicMessage, 0, len(messages))
	for _, m := range messages {
		reqMessages = append(reqMessages, anthropicMessage{Role: string(m.Role), Content: m.Content})
	}

	payload, err := json.Marshal(anthropicRequest{
		Model:     s.Model,
		MaxTokens: 1024,
		System:    systemPrompt,
		Messages:  reqMessages,
		Tools:     tools,
	})
	if err != nil {
		return nil, fmt.Errorf("ai: codificando la petición: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: construyendo la petición: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// El endpoint /v1/messages de OpenCode Zen imita la Anthropic Messages API
	// real, que se autentica con "x-api-key" (no "Authorization: Bearer").
	// "anthropic-version" también es obligatoria en esa API.
	req.Header.Set("x-api-key", s.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: la petición falló: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ai: no pudimos leer la respuesta (estado %d): %w", resp.StatusCode, err)
	}

	var parsed anthropicResponse
	// Un error del gateway (401, 404, 5xx) puede venir en texto plano o en un
	// JSON con otra forma; si no calza con la respuesta esperada, igual
	// queremos ver el cuerpo crudo en el error en vez de solo "no pudimos leer".
	unmarshalErr := json.Unmarshal(rawBody, &parsed)

	if resp.StatusCode >= 300 {
		msg := fmt.Sprintf("estado %d", resp.StatusCode)
		if parsed.Error != nil && parsed.Error.Message != "" {
			msg = fmt.Sprintf("%s: %s", msg, parsed.Error.Message)
		} else if len(rawBody) > 0 {
			body := string(rawBody)
			if len(body) > 300 {
				body = body[:300] + "…"
			}
			msg = fmt.Sprintf("%s: %s", msg, body)
		}
		return nil, fmt.Errorf("ai: %s", msg)
	}

	if unmarshalErr != nil {
		return nil, fmt.Errorf("ai: no pudimos interpretar la respuesta: %w", unmarshalErr)
	}

	reply := &AIReply{}
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			reply.Text += block.Text
		case "tool_use":
			reply.ToolCalls = append(reply.ToolCalls, AIToolCall{Name: block.Name, Input: block.Input})
		}
	}
	return reply, nil
}
