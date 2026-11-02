package services

import (
	"errors"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// La tarjeta del producto en el chat: la arma el servidor y solo con el catálogo del negocio.
func TestMessageItemCard(t *testing.T) {
	db, owner, other, ent, _ := sectionsFixture(t)
	cat := NewCatalogService(db)
	now := time.Now()
	m := models.Media{S3Key: "uploads/x.jpg", ContentType: "image/jpeg"}
	if err := db.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	price := 120000
	polo, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Camiseta polo", Kind: "product", Price: &price}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.SetPhotos(ent.ID, polo.ID, owner.ID, []uuid.UUID{m.ID}); err != nil {
		t.Fatal(err)
	}
	card, err := MessageItemCard(db, ent.ID, polo.ID)
	if err != nil || card.Name != "Camiseta polo" || card.Price == nil || *card.Price != price || card.Photo != m.ID.String() {
		t.Fatalf("tarjeta: %v %+v", err, card)
	}
	// Una variante: nombre con su opción y la foto de su producto.
	talla, err := cat.Create(ent.ID, owner.ID, ServiceInput{ParentID: &polo.ID, VariantLabel: "M"}, now)
	if err != nil {
		t.Fatal(err)
	}
	card, err = MessageItemCard(db, ent.ID, talla.ID)
	if err != nil || card.Name != "Camiseta polo (M)" || card.Photo != m.ID.String() {
		t.Fatalf("variante: %v %+v", err, card)
	}
	// De otro negocio, inexistente o retirado: no hay tarjeta.
	otro := models.Entity{Name: "Otro", OwnerID: other.ID, CategoryID: ent.CategoryID}
	if err := db.Create(&otro).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := MessageItemCard(db, otro.ID, polo.ID); !errors.Is(err, ErrMessageItem) {
		t.Fatalf("de otro negocio: %v", err)
	}
	if _, err := MessageItemCard(db, ent.ID, uuid.New()); !errors.Is(err, ErrMessageItem) {
		t.Fatalf("no existe: %v", err)
	}
	if err := cat.Retire(ent.ID, polo.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := MessageItemCard(db, ent.ID, polo.ID); !errors.Is(err, ErrMessageItem) {
		t.Fatalf("retirado: %v", err)
	}
	// Queda guardada en el mensaje (jsonb) y vuelve igual.
	msg := models.Message{SenderID: other.ID, EntityID: ent.ID, UserID: other.ID, Content: "¿La tienen en azul?", ItemID: &talla.ID, Item: &models.MessageItem{ID: talla.ID, Name: "Camiseta polo (M)", Price: &price}}
	if err := db.Create(&msg).Error; err != nil {
		t.Fatal(err)
	}
	var back models.Message
	if err := db.First(&back, "id = ?", msg.ID).Error; err != nil || back.Item == nil || back.Item.Name != "Camiseta polo (M)" {
		t.Fatalf("guardado: %v %+v", err, back.Item)
	}
}
