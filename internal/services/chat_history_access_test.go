package services

import (
	"errors"
	"testing"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

// TestChatHistoryAccess: la conversación de un negocio con un cliente solo la leen ese cliente y el dueño del
// negocio. Otro usuario con sesión no puede pedirla aunque conozca los ids.
func TestChatHistoryAccess(t *testing.T) {
	db := testDB(t)
	svc := NewChatService(repository.NewChatRepository(db))

	owner := models.User{Name: "Dueña", Email: "duena@x.co", PasswordHash: "x"}
	client := models.User{Name: "Cliente", Email: "cliente@x.co", PasswordHash: "x"}
	intruder := models.User{Name: "Curioso", Email: "curioso@x.co", PasswordHash: "x"}
	for _, u := range []*models.User{&owner, &client, &intruder} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Comida"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	ent := models.Entity{OwnerID: owner.ID, Name: "Arepas Doña Luz", CategoryID: cat.ID}
	if err := db.Omit("Owner", "Category").Create(&ent).Error; err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		customer uuid.UUID
		viewer   uuid.UUID
		want     error
	}{
		{"el cliente lee su propia conversación", client.ID, client.ID, nil},
		{"el dueño lee la conversación con un cliente", client.ID, owner.ID, nil},
		{"otro usuario no lee la conversación de un cliente", client.ID, intruder.ID, ErrChatForbidden},
		{"un cliente no lee la de otro cliente", intruder.ID, client.ID, ErrChatForbidden},
	}
	for _, c := range cases {
		if err := svc.AuthorizeHistory(ent.ID, c.customer, c.viewer); !errors.Is(err, c.want) {
			t.Errorf("%s: esperaba %v, llegó %v", c.name, c.want, err)
		}
	}
	if err := svc.AuthorizeHistory(uuid.New(), client.ID, owner.ID); !errors.Is(err, ErrChatEntityNotFound) {
		t.Errorf("negocio inexistente: esperaba ErrChatEntityNotFound, llegó %v", err)
	}
}
