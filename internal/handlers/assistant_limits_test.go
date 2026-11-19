package handlers

import (
	"strings"
	"testing"

	"empre_backend/internal/services"
)

func TestAssistantTooBig(t *testing.T) {
	short := []services.AIMessage{{Role: services.AIRoleUser, Content: "Vendo arepas en Manga"}}
	if msg := assistantTooBig(short); msg != "" {
		t.Fatalf("una conversación normal no debería rechazarse: %q", msg)
	}
	long := []services.AIMessage{{Role: services.AIRoleUser, Content: strings.Repeat("a", assistantMaxMessageRunes+1)}}
	if assistantTooBig(long) == "" {
		t.Fatal("un mensaje enorme debería rechazarse")
	}
	many := make([]services.AIMessage, assistantMaxMessages+1)
	for i := range many {
		many[i] = services.AIMessage{Role: services.AIRoleUser, Content: "hola"}
	}
	if assistantTooBig(many) == "" {
		t.Fatal("demasiados mensajes deberían rechazarse")
	}
	total := make([]services.AIMessage, 15)
	for i := range total {
		total[i] = services.AIMessage{Role: services.AIRoleUser, Content: strings.Repeat("b", 1500)}
	}
	if assistantTooBig(total) == "" {
		t.Fatal("una conversación que en total pasa el tope debería rechazarse")
	}
}
