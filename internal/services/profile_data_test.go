package services

// Integración con PostgreSQL de lo que alimenta el perfil del negocio: reseñas
// verificadas por un canje, distribución de estrellas, ofertas activas
// públicas y el conteo de canjes verificados. Se omiten sin TEST_DATABASE_URL.

import (
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func newReviewService(db *gorm.DB) *ReviewService {
	return NewReviewService(repository.NewReviewRepository(db), repository.NewClaimRepository(db))
}

// redeemAndConfirm lleva un reclamo hasta «canjeado y confirmado por el cliente».
func (f *fixture) redeemAndConfirm(t *testing.T, client uuid.UUID) *models.OfferClaim {
	t.Helper()
	res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	if _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "scanner"); err != nil {
		t.Fatalf("canjear: %v", err)
	}
	if err := f.svc.Confirm(res.Claim.ID, client); err != nil {
		t.Fatalf("confirmar: %v", err)
	}
	return res.Claim
}

// Una reseña es «verificada» solo si la respalda un canje del mismo usuario y
// del mismo negocio que ya fue canjeado y confirmado.
func TestReview_VerifiedOnlyByAConfirmedRedemptionOfTheSameUserAndBusiness(t *testing.T) {
	db := openTestDB(t, true)
	reviews := newReviewService(db)
	here := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	elsewhere := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	entityID := here.offer.EntityID
	client := newUser(t, db)

	// Un reclamo sin canjear no respalda nada.
	pending, err := here.svc.Claim(here.offer.ID, client, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	if _, err := reviews.Upsert(entityID, client, 5, "", &pending.Claim.ID); err == nil {
		t.Fatal("una reseña no debía verificarse con un reclamo sin canjear")
	}

	// Canjeado pero sin confirmar por el cliente: tampoco.
	if _, err := here.svc.RedeemByToken(pending.Claim.Token, here.ownerID, "s"); err != nil {
		t.Fatalf("canjear: %v", err)
	}
	if _, err := reviews.Upsert(entityID, client, 5, "", &pending.Claim.ID); err == nil {
		t.Fatal("una reseña no debía verificarse con un canje sin confirmar")
	}
	if err := here.svc.Confirm(pending.Claim.ID, client); err != nil {
		t.Fatalf("confirmar: %v", err)
	}

	// El canje es de otra persona.
	if _, err := reviews.Upsert(entityID, newUser(t, db), 5, "", &pending.Claim.ID); err == nil {
		t.Fatal("otra persona pudo verificar su reseña con un canje ajeno")
	}
	// El canje es de otro negocio.
	if _, err := reviews.Upsert(elsewhere.offer.EntityID, client, 5, "", &pending.Claim.ID); err == nil {
		t.Fatal("una reseña de otro negocio pudo verificarse con este canje")
	}
	// Un id que no existe.
	missing := uuid.New()
	if _, err := reviews.Upsert(entityID, client, 5, "", &missing); err == nil {
		t.Fatal("un canje inexistente no debía verificar nada")
	}

	// Ahora sí: mismo usuario, mismo negocio, canjeado y confirmado.
	review, err := reviews.Upsert(entityID, client, 5, "Excelente", &pending.Claim.ID)
	if err != nil {
		t.Fatalf("reseña verificada: %v", err)
	}
	if review.ClaimID == nil || *review.ClaimID != pending.Claim.ID {
		t.Fatalf("la reseña debía quedar ligada al canje, ClaimID = %v", review.ClaimID)
	}

	// Editar el texto no quita la verificación.
	edited, err := reviews.Upsert(entityID, client, 4, "Cambié de opinión", nil)
	if err != nil {
		t.Fatalf("editar: %v", err)
	}
	if edited.ClaimID == nil || edited.Rating != 4 {
		t.Errorf("editada: ClaimID = %v, Rating = %d; debía conservar la verificación y cambiar la calificación", edited.ClaimID, edited.Rating)
	}

	// Una reseña normal, sin canje, sigue siendo posible y no es verificada.
	plain, err := reviews.Upsert(entityID, newUser(t, db), 3, "Bien", nil)
	if err != nil || plain.ClaimID != nil {
		t.Errorf("reseña sin canje: err = %v, ClaimID = %v", err, plain.ClaimID)
	}
}

// La distribución cuenta cada calificación y cuántas reseñas están verificadas.
func TestReview_BreakdownCountsStarsAndVerified(t *testing.T) {
	db := openTestDB(t, true)
	reviews := newReviewService(db)
	f := newFixture(t, db, 10, models.OfferHours, models.RepeatOnce)
	entityID := f.offer.EntityID

	for _, rating := range []int{5, 5, 4, 1} {
		if _, err := reviews.Upsert(entityID, newUser(t, db), rating, "", nil); err != nil {
			t.Fatalf("reseña de %d: %v", rating, err)
		}
	}
	verifiedUser := newUser(t, db)
	claim := f.redeemAndConfirm(t, verifiedUser)
	if _, err := reviews.Upsert(entityID, verifiedUser, 5, "", &claim.ID); err != nil {
		t.Fatalf("reseña verificada: %v", err)
	}

	distribution, verified, err := reviews.Breakdown(entityID)
	if err != nil {
		t.Fatalf("Breakdown: %v", err)
	}
	if want := [5]int64{1, 0, 0, 1, 3}; distribution != want {
		t.Errorf("distribución = %v, se esperaba %v (índice 0 = 1 estrella)", distribution, want)
	}
	if verified != 1 {
		t.Errorf("verificadas = %d, se esperaba 1", verified)
	}

	// Otro negocio no mezcla sus reseñas.
	other := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	if d, v, _ := reviews.Breakdown(other.offer.EntityID); d != [5]int64{} || v != 0 {
		t.Errorf("un negocio sin reseñas devolvió %v / %d", d, v)
	}
}

// El listado público trae solo las ofertas vigentes y con cupos de ese negocio,
// las que terminan antes primero.
func TestOffers_ActiveListIsPublicSortedAndFiltered(t *testing.T) {
	db := openTestDB(t, true)
	f := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce) // termina en 2 h
	offers := NewOfferService(repository.NewOfferRepository(db), repository.NewEntityRepository(db))
	now := utils.NowInCartagena()

	add := func(entityID uuid.UUID, title string, ends time.Time, slots, used int) *models.Offer {
		o := &models.Offer{EntityID: entityID, Type: models.OfferDay, Title: title, Price: "1000", StartsAt: now.Add(-time.Hour),
			EndsAt: ends, TotalSlots: slots, UsedSlots: used, RadiusMeters: 500, RepeatPolicy: models.RepeatOnce}
		if err := db.Omit(clause.Associations).Create(o).Error; err != nil {
			t.Fatalf("crear oferta %q: %v", title, err)
		}
		return o
	}
	soon := add(f.offer.EntityID, "Termina pronto", now.Add(30*time.Minute), 3, 0)
	add(f.offer.EntityID, "Ya terminó", now.Add(-time.Hour), 3, 0)
	add(f.offer.EntityID, "Agotada", now.Add(3*time.Hour), 3, 3)
	other := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)

	got, err := offers.ActiveListForEntity(f.offer.EntityID)
	if err != nil {
		t.Fatalf("ActiveListForEntity: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ofertas = %d, se esperaban 2 (la terminada, la agotada y la de otro negocio no cuentan)", len(got))
	}
	if got[0].ID != soon.ID || got[1].ID != f.offer.ID {
		t.Errorf("orden incorrecto: debía ir primero la que termina antes")
	}
	for _, o := range got {
		if o.EntityID == other.offer.EntityID {
			t.Error("apareció una oferta de otro negocio")
		}
	}
	if empty, err := offers.ActiveListForEntity(uuid.New()); err != nil || len(empty) != 0 {
		t.Errorf("negocio sin ofertas: %v, %d resultados", err, len(empty))
	}
}

// La prueba social cuenta solo canjes confirmados por el cliente y dentro del periodo.
func TestClaimRepo_CountVerifiedRedemptionsSince(t *testing.T) {
	db := openTestDB(t, true)
	f := newFixture(t, db, 10, models.OfferHours, models.RepeatOnce)
	repo := repository.NewClaimRepository(db)
	now := utils.NowInCartagena()

	recent := f.redeemAndConfirm(t, newUser(t, db))
	old := f.redeemAndConfirm(t, newUser(t, db))
	db.Model(&models.OfferClaim{}).Where("id = ?", old.ID).Update("redeemed_at", now.Add(-10*24*time.Hour))

	// Canjeado pero sin confirmar: no cuenta como prueba social.
	unconfirmed, err := f.svc.Claim(f.offer.ID, newUser(t, db), 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	if _, err := f.svc.RedeemByToken(unconfirmed.Claim.Token, f.ownerID, "s"); err != nil {
		t.Fatalf("canjear: %v", err)
	}

	week, err := repo.CountVerifiedRedemptionsSince(f.offer.EntityID, now.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("conteo: %v", err)
	}
	if week != 1 {
		t.Errorf("esta semana = %d, se esperaba 1 (el de hace 10 días y el sin confirmar no cuentan)", week)
	}
	month, _ := repo.CountVerifiedRedemptionsSince(f.offer.EntityID, now.Add(-30*24*time.Hour))
	if month != 2 {
		t.Errorf("este mes = %d, se esperaban 2", month)
	}
	if none, _ := repo.CountVerifiedRedemptionsSince(uuid.New(), now.Add(-7*24*time.Hour)); none != 0 {
		t.Errorf("otro negocio = %d", none)
	}
	_ = recent
}
