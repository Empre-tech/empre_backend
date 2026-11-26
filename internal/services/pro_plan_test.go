package services

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
)

func proFixture(t *testing.T) (*gorm.DB, models.User, []models.Entity) {
	t.Helper()
	db := testDB(t)
	owner := models.User{Name: "Lina", Email: "lina@x.co", PasswordHash: "x"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	cat := models.Category{Name: "Ropa"}
	db.Create(&cat)
	var ents []models.Entity
	for i, name := range []string{"Moda Barrio", "Moda Centro", "Moda Playa"} {
		e := models.Entity{OwnerID: owner.ID, Name: name, CategoryID: cat.ID, CreatedAt: time.Now().Add(time.Duration(i) * time.Minute)}
		if err := db.Omit("Owner", "Category").Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		ents = append(ents, e)
	}
	return db, owner, ents
}

func rcEvent(id, typ string, user uuid.UUID, product string, exp time.Time, at time.Time) RevenueCatEvent {
	ms := exp.UnixMilli()
	return RevenueCatEvent{ID: id, Type: typ, AppUserID: user.String(), ProductID: product, ExpirationAtMs: &ms, EventTimestampMs: at.UnixMilli(), Store: "PLAY_STORE", Environment: "SANDBOX"}
}

func isPro(t *testing.T, l *LedgerService, id uuid.UUID) bool {
	t.Helper()
	ok, err := l.IsPro(id)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// TestOwnerProPlanLifecycle: compra, cambio de negocio cubierto, subir y bajar de nivel, cancelar y vencer.
func TestOwnerProPlanLifecycle(t *testing.T) {
	db, owner, ents := proFixture(t)
	svc := NewProPlanService(db, "s3cr3t")
	l := NewLedgerService(db)
	now := time.Now()
	month := now.AddDate(0, 1, 0)

	// Antes de comprar, marca el negocio que va a cubrir (el segundo).
	if _, err := svc.SetCoverage(owner.ID, []uuid.UUID{ents[1].ID}, now); err != nil {
		t.Fatal(err)
	}
	if isPro(t, l, ents[1].ID) {
		t.Fatal("sin plan vigente no es Pro")
	}
	// Compra «Pro 1 negocio».
	ok, err := svc.HandleRevenueCat(rcEvent("e1", "INITIAL_PURCHASE", owner.ID, "empre_pro_1:mensual", month, now), now)
	if err != nil || !ok {
		t.Fatalf("compra aplicada: %v %v", ok, err)
	}
	if !isPro(t, l, ents[1].ID) || isPro(t, l, ents[0].ID) {
		t.Fatal("cubre el negocio que eligió, no otro")
	}
	// El mismo aviso otra vez no hace nada.
	if ok, _ := svc.HandleRevenueCat(rcEvent("e1", "INITIAL_PURCHASE", owner.ID, "empre_pro_1:mensual", month, now), now); ok {
		t.Fatal("aviso repetido")
	}
	// Con nivel 1 no puede cubrir dos.
	if _, err := svc.SetCoverage(owner.ID, []uuid.UUID{ents[0].ID, ents[1].ID}, now); err != ErrProCoverageTooMany {
		t.Fatalf("más negocios que el nivel: %v", err)
	}
	// Cambia el negocio cubierto.
	if _, err := svc.SetCoverage(owner.ID, []uuid.UUID{ents[0].ID}, now); err != nil {
		t.Fatal(err)
	}
	if !isPro(t, l, ents[0].ID) || isPro(t, l, ents[1].ID) {
		t.Fatal("el Pro pasa al negocio elegido")
	}
	// Sube a «Pro 2 negocios»: se suma el siguiente negocio solo.
	t1 := now.Add(time.Minute)
	if _, err := svc.HandleRevenueCat(RevenueCatEvent{ID: "e2", Type: "PRODUCT_CHANGE", AppUserID: owner.ID.String(), ProductID: "empre_pro_1:mensual", NewProductID: "empre_pro_2:mensual", ExpirationAtMs: ptr(month.UnixMilli()), EventTimestampMs: t1.UnixMilli(), Store: "PLAY_STORE"}, now); err != nil {
		t.Fatal(err)
	}
	if !isPro(t, l, ents[0].ID) || !isPro(t, l, ents[1].ID) || isPro(t, l, ents[2].ID) {
		t.Fatal("nivel 2 cubre dos negocios")
	}
	// Bajar de nivel no aplica hasta renovar.
	t2 := now.Add(2 * time.Minute)
	svc.HandleRevenueCat(RevenueCatEvent{ID: "e3", Type: "PRODUCT_CHANGE", AppUserID: owner.ID.String(), ProductID: "empre_pro_2:mensual", NewProductID: "empre_pro_1:mensual", ExpirationAtMs: ptr(month.UnixMilli()), EventTimestampMs: t2.UnixMilli()}, now)
	if !isPro(t, l, ents[1].ID) {
		t.Fatal("bajar de nivel espera a la renovación")
	}
	// Renueva en nivel 1: queda el primero que eligió.
	month2 := month.AddDate(0, 1, 0)
	t3 := now.Add(3 * time.Minute)
	svc.HandleRevenueCat(rcEvent("e4", "RENEWAL", owner.ID, "empre_pro_1:mensual", month2, t3), now)
	if !isPro(t, l, ents[0].ID) || isPro(t, l, ents[1].ID) {
		t.Fatal("al renovar en nivel 1 queda solo el primero elegido")
	}
	// Un aviso viejo que llega tarde no pisa.
	svc.HandleRevenueCat(rcEvent("e0", "RENEWAL", owner.ID, "empre_pro_3", month, now.Add(-time.Hour)), now)
	var plan models.OwnerProPlan
	db.First(&plan, "user_id = ?", owner.ID)
	if plan.Tier != 1 || !plan.CurrentPeriodEnd.Equal(time.UnixMilli(month2.UnixMilli())) {
		t.Fatalf("el aviso viejo no cambia el plan: %+v", plan)
	}
	// Cancela: sigue con Pro hasta que venza.
	t4 := now.Add(4 * time.Minute)
	svc.HandleRevenueCat(RevenueCatEvent{ID: "e5", Type: "CANCELLATION", AppUserID: owner.ID.String(), ProductID: "empre_pro_1", EventTimestampMs: t4.UnixMilli()}, now)
	if !isPro(t, l, ents[0].ID) {
		t.Fatal("cancelado sigue Pro hasta vencer")
	}
	// Vence.
	t5 := now.Add(5 * time.Minute)
	svc.HandleRevenueCat(rcEvent("e6", "EXPIRATION", owner.ID, "empre_pro_1", now.Add(-time.Second), t5), now)
	if isPro(t, l, ents[0].ID) {
		t.Fatal("vencido ya no es Pro")
	}
	// La cobertura se queda para cuando vuelva a pagar.
	ov, _ := svc.Overview(owner.ID, now)
	if ov.Active || len(ov.Covered) != 1 || ov.Covered[0] != ents[0].ID || ov.Prices[2] != 59850 {
		t.Fatalf("resumen: %+v", ov)
	}
}

func ptr(v int64) *int64 { return &v }

// TestOwnerProPlanStatusAndAuth: la suscripción del negocio se ve activa (las apps leen el Pro de ahí), el secreto
// del aviso, «Pregúntale» y usuarios que no son de Empre.
func TestOwnerProPlanStatusAndAuth(t *testing.T) {
	db, owner, ents := proFixture(t)
	svc := NewProPlanService(db, "s3cr3t")
	now := time.Now()
	if svc.CheckWebhookAuth("otro") == nil || svc.CheckWebhookAuth("Bearer s3cr3t") != nil || svc.CheckWebhookAuth("s3cr3t") != nil {
		t.Fatal("secreto del aviso")
	}
	if NewProPlanService(db, "").CheckWebhookAuth("") == nil {
		t.Fatal("sin secreto configurado no se acepta nada")
	}
	svc.HandleRevenueCat(rcEvent("s1", "INITIAL_PURCHASE", owner.ID, "empre_pro_1", now.AddDate(0, 1, 0), now), now)
	subs := &SubscriptionService{Repo: repository.NewSubscriptionRepository(db)}
	st, err := subs.Status(ents[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Source != "store" || st.Subscription.Status != models.SubscriptionActive || st.Subscription.CurrentPeriodEnd == nil {
		t.Fatalf("la suscripción se ve activa por el plan: %+v %+v", st, st.Subscription)
	}
	st2, _ := subs.Status(ents[1].ID)
	if st2.Source != "" || st2.Subscription.Status == models.SubscriptionActive {
		t.Fatal("el negocio no cubierto sigue en Base")
	}
	// «Pregúntale a Empre» (paquete aparte).
	if ok, err := svc.HandleRevenueCat(rcEvent("p1", "INITIAL_PURCHASE", owner.ID, "empre_plus:mensual", now.AddDate(0, 1, 0), now), now); !ok || err != nil {
		t.Fatalf("plus: %v %v", ok, err)
	}
	if plus, _ := userPlanActive(db, owner.ID, now); !plus {
		t.Fatal("plus activo")
	}
	// Un usuario que no es de Empre, un producto desconocido y la prueba de RevenueCat no hacen nada.
	if ok, _ := svc.HandleRevenueCat(RevenueCatEvent{ID: "x1", Type: "INITIAL_PURCHASE", AppUserID: "$RCAnonymousID:abc", ProductID: "empre_pro_1"}, now); ok {
		t.Fatal("anónimo")
	}
	if ok, _ := svc.HandleRevenueCat(rcEvent("x2", "INITIAL_PURCHASE", owner.ID, "otra_cosa", now.AddDate(0, 1, 0), now), now); ok {
		t.Fatal("producto desconocido")
	}
	if ok, _ := svc.HandleRevenueCat(RevenueCatEvent{ID: "x3", Type: "TEST"}, now); ok {
		t.Fatal("prueba")
	}
	// No puede cubrir un negocio ajeno.
	other := models.User{Name: "Otro", Email: "otro2@x.co", PasswordHash: "x"}
	db.Create(&other)
	if _, err := svc.SetCoverage(other.ID, []uuid.UUID{ents[0].ID}, now); err != ErrProCoverageNotOwner {
		t.Fatalf("negocio ajeno: %v", err)
	}
}
