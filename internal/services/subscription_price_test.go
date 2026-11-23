package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
)

// TestProPriceExtraBusiness: el primer negocio con Pro paga completo; los demás del mismo dueño, la mitad. Un Pro
// vencido no cuenta, ni el de otro dueño.
func TestProPriceExtraBusiness(t *testing.T) {
	db, _, owner, other, first := ledgerFixture(t) // first ya tiene Pro activo
	svc := &SubscriptionService{Repo: repository.NewSubscriptionRepository(db)}
	second := models.Entity{OwnerID: owner.ID, Name: "Panadería Marta 2", CategoryID: first.CategoryID}
	foreign := models.Entity{OwnerID: other.ID, Name: "Otro negocio", CategoryID: first.CategoryID}
	for _, e := range []*models.Entity{&second, &foreign} {
		if err := db.Omit("Owner", "Category").Create(e).Error; err != nil {
			t.Fatal(err)
		}
	}

	if p, extra, err := svc.PriceFor(second.ID); err != nil || p != ProExtraPriceCOP || !extra {
		t.Fatalf("segundo negocio: %d %v %v", p, extra, err)
	}
	if p, extra, _ := svc.PriceFor(first.ID); p != ProMonthlyPriceCOP || extra {
		t.Fatalf("el que ya es Pro no se descuenta a sí mismo: %d %v", p, extra)
	}
	if p, _, _ := svc.PriceFor(foreign.ID); p != ProMonthlyPriceCOP {
		t.Fatalf("otro dueño paga completo: %d", p)
	}
	past := time.Now().AddDate(0, 0, -1)
	if err := db.Model(&models.Subscription{}).Where("entity_id = ?", first.ID).Update("current_period_end", past).Error; err != nil {
		t.Fatal(err)
	}
	if p, _, _ := svc.PriceFor(second.ID); p != ProMonthlyPriceCOP {
		t.Fatalf("con el Pro vencido ya no hay descuento: %d", p)
	}
}
