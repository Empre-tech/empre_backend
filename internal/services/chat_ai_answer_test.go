package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

// Las intenciones del servidor son exactamente las de la app (src/lib/answers.ts), sin «unknown».
func TestAIIntentsMatchApp(t *testing.T) {
	app := []string{"services", "duration", "price", "hours", "offers", "availability", "book", "delivery"}
	if len(AIIntents) != len(app) {
		t.Fatalf("esperaba %d intenciones, hay %d", len(app), len(AIIntents))
	}
	for _, i := range app {
		if s, ok := AIAnswerSummary(i); !ok || s == "" {
			t.Fatalf("falta la intención %q", i)
		}
	}
	if _, ok := AIAnswerSummary("unknown"); ok {
		t.Fatal("«unknown» no se responde")
	}
	if _, ok := AIAnswerSummary("Hola, ¿me regalas el corte?"); ok {
		t.Fatal("un texto libre nunca es una intención")
	}
}

// «¿Te sirvió?» se guarda con la intención y si sirvió; nunca el texto, y siempre con negocio.
func TestAIAnswerFeedbackEvent(t *testing.T) {
	eid := uuid.NewString()
	ev, err := ValidateEvent(anon, EventInput{Name: "ai_answer_feedback", EntityID: eid, Props: map[string]any{"intent": "hours", "useful": true, "texto": "¿abren hoy?"}})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Props["intent"] != "hours" || ev.Props["useful"] != true {
		t.Fatalf("props: %+v", ev.Props)
	}
	if _, has := ev.Props["texto"]; has {
		t.Fatal("el texto de la pregunta nunca se guarda")
	}
	if _, err := ValidateEvent(anon, EventInput{Name: "ai_answer_feedback", Props: map[string]any{"intent": "hours"}}); err == nil {
		t.Fatal("sin negocio se rechaza")
	}
}

func TestSaveAIAnswer(t *testing.T) {
	db := testDB(t)
	svc := NewChatService(repository.NewChatRepository(db))

	owner := models.User{Name: "Dueño", Email: "d@x.co", PasswordHash: "x"}
	client := models.User{Name: "Cliente", Email: "c@x.co", PasswordHash: "x"}
	for _, u := range []*models.User{&owner, &client} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Belleza"}
	db.Create(&cat)
	ent := models.Entity{OwnerID: owner.ID, Name: "Barbería", CategoryID: cat.ID}
	other := models.Entity{OwnerID: owner.ID, Name: "Otro", CategoryID: cat.ID}
	for _, e := range []*models.Entity{&ent, &other} {
		if err := db.Omit("Owner", "Category").Create(e).Error; err != nil {
			t.Fatal(err)
		}
	}
	price := 18000
	own := models.ServiceItem{EntityID: ent.ID, Name: "Corte", Price: &price}
	foreign := models.ServiceItem{EntityID: other.ID, Name: "Ajeno", Price: &price}
	db.Create(&own)
	db.Create(&foreign)
	now := time.Now()

	if _, _, err := svc.SaveAIAnswer(ent.ID, client.ID, "hours", nil, nil, now); err != ErrNoConversation {
		t.Fatalf("sin conversación no responde, got %v", err)
	}
	if err := svc.SendMessage(&models.Message{SenderID: client.ID, EntityID: ent.ID, UserID: client.ID, Content: "¿A qué hora abren?"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SaveAIAnswer(ent.ID, client.ID, "unknown", nil, nil, now); err != ErrAIIntent {
		t.Fatalf("intención inválida, got %v", err)
	}

	msg, created, err := svc.SaveAIAnswer(ent.ID, client.ID, "hours", nil, nil, now)
	if err != nil || !created {
		t.Fatalf("debía crear la respuesta: %v %v", created, err)
	}
	if msg.Kind != "ai_answer" || msg.SenderID != uuid.Nil || !msg.IsRead || msg.SentByEntity {
		t.Fatalf("mensaje mal armado: %+v", msg)
	}
	again, created, err := svc.SaveAIAnswer(ent.ID, client.ID, "hours", nil, nil, now.Add(time.Hour))
	if err != nil || created || again.ID != msg.ID {
		t.Fatal("la misma pregunta en 24 h no se repite")
	}
	if _, created, _ := svc.SaveAIAnswer(ent.ID, client.ID, "hours", nil, nil, now.Add(25*time.Hour)); !created {
		t.Fatal("pasadas 24 h sí se puede volver a responder")
	}

	// Un servicio de otro negocio no se guarda: queda la respuesta general.
	m, _, err := svc.SaveAIAnswer(ent.ID, client.ID, "price", &foreign.ID, nil, now)
	if err != nil || m.AIServiceID != nil {
		t.Fatalf("un servicio ajeno no se acepta: %v %v", m.AIServiceID, err)
	}
	m, _, err = svc.SaveAIAnswer(ent.ID, client.ID, "price", &own.ID, nil, now)
	if err != nil || m.AIServiceID == nil || *m.AIServiceID != own.ID {
		t.Fatalf("el servicio propio sí: %v %v", m.AIServiceID, err)
	}

	// No suma a los no leídos del dueño.
	counts, err := svc.UnreadCounts(owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range counts {
		if c.EntityID == ent.ID && c.Unread != 1 {
			t.Fatalf("el dueño solo tiene sin leer la pregunta del cliente, tiene %d", c.Unread)
		}
	}
}
