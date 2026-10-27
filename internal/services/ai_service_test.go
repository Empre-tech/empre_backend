package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// El dictado obliga a usar la herramienta y deja espacio de sobra en max_tokens; el chat normal no fuerza nada.
func TestAIServiceToolChoice(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"thinking","thinking":"..."},{"type":"tool_use","name":"registrar","input":{"actions":[]}}]}`))
	}))
	defer srv.Close()
	ai := NewAIService("k", srv.URL, "claude-haiku-5-5")

	reply, err := ai.ChatTool("sys", []AIMessage{{Role: AIRoleUser, Content: "hola"}}, AITool{Name: "registrar", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Name != "registrar" {
		t.Fatalf("respuesta: %+v", reply)
	}
	tc, _ := got["tool_choice"].(map[string]any)
	if tc["type"] != "tool" || tc["name"] != "registrar" {
		t.Fatalf("tool_choice: %v", got["tool_choice"])
	}
	if got["model"] != "claude-haiku-5-5" || got["max_tokens"].(float64) < 4096 {
		t.Fatalf("petición: %v", got)
	}
	if _, ok := got["temperature"]; ok {
		t.Fatal("los modelos 5.x no aceptan temperature")
	}

	if _, err := ai.Chat("sys", []AIMessage{{Role: AIRoleUser, Content: "hola"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["tool_choice"]; ok {
		t.Fatal("el chat normal no debe forzar herramienta")
	}
}
