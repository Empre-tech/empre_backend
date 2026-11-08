package services

import (
	"reflect"
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

func TestNormalizeQuestionAndKeywords(t *testing.T) {
	if got := NormalizeQuestion("  ¿Atienden NIÑOS los sábados?? "); got != "atienden ninos los sabados" {
		t.Fatalf("normalizar: %q", got)
	}
	got := KeywordsFrom("Hola, ¿ustedes atienden niños los sábados? Gracias")
	if !reflect.DeepEqual(got, []string{"atienden", "ninos", "sabados"}) {
		t.Fatalf("palabras clave: %v", got)
	}
	if len(KeywordsFrom("¿y eso?")) != 0 {
		t.Fatal("sin palabras con sentido no hay palabras clave")
	}
}

func TestMedianReplyMinutes(t *testing.T) {
	if _, ok := MedianReplyMinutes([]float64{3, 4, 5, 6}); ok {
		t.Fatal("con menos de 5 conversaciones no se dice nada")
	}
	if m, ok := MedianReplyMinutes([]float64{90, 2, 15, 8, 400}); !ok || m != 15 {
		t.Fatalf("mediana impar: %d %v", m, ok)
	}
	if m, _ := MedianReplyMinutes([]float64{10, 20, 30, 40, 50, 60}); m != 35 {
		t.Fatalf("mediana par: %d", m)
	}
	if m, _ := MedianReplyMinutes([]float64{0.1, 0.2, 0.3, 0.2, 0.1}); m != 1 {
		t.Fatalf("nunca menos de 1 minuto: %d", m)
	}
}

func TestDedupeUnanswered(t *testing.T) {
	now := time.Now()
	msgs := []models.Message{
		{ID: uuid.New(), Content: "¿Atienden niños?", CreatedAt: now},
		{ID: uuid.New(), Content: "atienden niños", CreatedAt: now.Add(-time.Hour)},
		{ID: uuid.New(), Content: "¿Tienen parqueadero?", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: uuid.New(), Content: "   ", CreatedAt: now},
	}
	out := dedupeUnanswered(msgs, 20)
	if len(out) != 2 || out[0].Text != "¿Atienden niños?" || out[1].Text != "¿Tienen parqueadero?" {
		t.Fatalf("una sola vez cada pregunta, la más reciente: %+v", out)
	}
	if len(dedupeUnanswered(msgs, 1)) != 1 {
		t.Fatal("respeta el tope")
	}
}

func TestBusinessAnswersFlow(t *testing.T) {
	db := testDB(t)
	svc := NewBusinessAnswerService(db)
	chat := NewChatService(repository.NewChatRepository(db))

	owner := models.User{Name: "Dueño", Email: "d2@x.co", PasswordHash: "x"}
	client := models.User{Name: "Cliente", Email: "c2@x.co", PasswordHash: "x"}
	for _, u := range []*models.User{&owner, &client} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Belleza"}
	db.Create(&cat)
	ent := models.Entity{OwnerID: owner.ID, Name: "Barbería", CategoryID: cat.ID}
	if err := db.Omit("Owner", "Category").Create(&ent).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Upsert(ent.ID, client.ID, BusinessAnswerInput{Intent: "delivery", Answer: "No"}); err != ErrNotOwner {
		t.Fatalf("solo el dueño enseña, got %v", err)
	}
	a1, err := svc.Upsert(ent.ID, owner.ID, BusinessAnswerInput{Intent: "delivery", Answer: "No hacemos domicilio, pero te lo guardamos."})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := svc.Upsert(ent.ID, owner.ID, BusinessAnswerInput{Intent: "delivery", Answer: "Solo en el local."})
	if err != nil || a2.ID != a1.ID {
		t.Fatal("una sola respuesta por intención: la nueva reemplaza")
	}
	custom, err := svc.Upsert(ent.ID, owner.ID, BusinessAnswerInput{Question: "¿Atienden niños?", Answer: "Sí, desde los 3 años."})
	if err != nil || len(custom.Keywords) == 0 {
		t.Fatalf("pregunta propia con palabras clave: %v %v", custom, err)
	}
	if _, err := svc.Upsert(ent.ID, owner.ID, BusinessAnswerInput{Question: "¿y eso?", Answer: "x"}); err != ErrAnswerInput {
		t.Fatalf("una pregunta propia sin palabras no sirve, got %v", err)
	}
	list, off, _ := svc.List(ent.ID)
	if len(list) != 2 || off {
		t.Fatalf("lista: %d off=%v", len(list), off)
	}

	// Pregunta sin respuesta: la marca el cliente y la ve el dueño.
	q := models.Message{SenderID: client.ID, EntityID: ent.ID, UserID: client.ID, Content: "¿Tienen parqueadero?"}
	if err := chat.SendMessage(&q); err != nil {
		t.Fatal(err)
	}
	if err := chat.MarkUnanswered(ent.ID, owner.ID, q.ID); err != ErrMessageNotFound {
		t.Fatalf("solo el autor marca su pregunta, got %v", err)
	}
	if err := chat.MarkUnanswered(ent.ID, client.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	un, err := svc.Unanswered(ent.ID, owner.ID, time.Now())
	if err != nil || len(un) != 1 || un[0].Text != "¿Tienen parqueadero?" {
		t.Fatalf("sin responder: %+v %v", un, err)
	}

	// Respuesta propia en el chat y el interruptor.
	if _, created, err := chat.SaveAIAnswer(ent.ID, client.ID, AICustomIntent, nil, &custom.ID, time.Now()); err != nil || !created {
		t.Fatalf("respuesta enseñada: %v %v", created, err)
	}
	if _, _, err := chat.SaveAIAnswer(ent.ID, client.ID, AICustomIntent, nil, nil, time.Now()); err != ErrAIIntent {
		t.Fatalf("custom sin respuesta no vale, got %v", err)
	}
	if err := svc.SetChatOff(ent.ID, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := chat.SaveAIAnswer(ent.ID, client.ID, "hours", nil, nil, time.Now()); err != ErrAIChatOff {
		t.Fatalf("apagado no responde, got %v", err)
	}
	if err := svc.Delete(ent.ID, owner.ID, custom.ID); err != nil {
		t.Fatal(err)
	}
}
