package services

// Integración con PostgreSQL de «Seguir y Novedades»: solo cuenta lo publicado
// después de empezar a seguir, lo visto se descuenta por negocio y el dueño
// solo ve cuántos seguidores tiene. Se omiten sin TEST_DATABASE_URL.

import (
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func follow(t *testing.T, db *gorm.DB, userID, entityID uuid.UUID, at time.Time) {
	t.Helper()
	if err := db.Create(&models.Favorite{UserID: userID, EntityID: entityID, CreatedAt: at}).Error; err != nil {
		t.Fatalf("seguir: %v", err)
	}
}

func post(t *testing.T, db *gorm.DB, entityID uuid.UUID, caption string, at time.Time) {
	t.Helper()
	p := &models.Post{EntityID: entityID, Caption: caption, CreatedAt: at}
	if err := db.Omit(clause.Associations).Create(p).Error; err != nil {
		t.Fatalf("crear publicación: %v", err)
	}
}

func setOfferCreatedAt(t *testing.T, db *gorm.DB, offerID uuid.UUID, at time.Time) {
	t.Helper()
	if err := db.Model(&models.Offer{}).Where("id = ?", offerID).UpdateColumn("created_at", at).Error; err != nil {
		t.Fatalf("fecha de la oferta: %v", err)
	}
}

func TestNovedades_OnlyWhatWasPublishedAfterFollowing(t *testing.T) {
	db := openTestDB(t, true)
	favs := NewFavoriteService(repository.NewFavoriteRepository(db))
	f := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	entityID := f.offer.EntityID
	user := newUser(t, db)
	now := time.Now()

	// Antes de seguir: no cuenta.
	setOfferCreatedAt(t, db, f.offer.ID, now.Add(-3*time.Hour))
	post(t, db, entityID, "Antes de seguir", now.Add(-2*time.Hour))
	follow(t, db, user, entityID, now.Add(-time.Hour))
	// Después de seguir: cuenta.
	post(t, db, entityID, "Nueva carta", now.Add(-30*time.Minute))
	// Fuera de la ventana de 30 días: no cuenta aunque sea posterior al seguir (seguía desde antes).
	old := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	follow(t, db, user, old.offer.EntityID, now.Add(-90*24*time.Hour))
	setOfferCreatedAt(t, db, old.offer.ID, now.Add(-60*24*time.Hour))

	// Un negocio que no sigue no aparece.
	stranger := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	setOfferCreatedAt(t, db, stranger.offer.ID, now)

	items, err := favs.Novedades(user, now)
	if err != nil {
		t.Fatalf("Novedades: %v", err)
	}
	if len(items) != 1 || items[0].Kind != "post" || items[0].Title != "Nueva carta" || !items[0].Unseen {
		t.Fatalf("novedades = %+v, se esperaba solo la publicación nueva, sin ver", items)
	}

	// Una oferta nueva del negocio seguido llega primero (es la más reciente).
	offer := &models.Offer{EntityID: entityID, Type: models.OfferDay, Title: "2x1", Price: "1", StartsAt: now, EndsAt: now.Add(time.Hour), TotalSlots: 3, RadiusMeters: 500, RepeatPolicy: models.RepeatOnce}
	if err := db.Omit(clause.Associations).Create(offer).Error; err != nil {
		t.Fatalf("oferta: %v", err)
	}
	items, _ = favs.Novedades(user, time.Now())
	if len(items) != 2 || items[0].Kind != "offer" || items[0].Title != "2x1" {
		t.Fatalf("novedades = %+v, se esperaba la oferta primero y luego la publicación", items)
	}
}

func TestNovedades_SeenIsPerBusinessAndPerUser(t *testing.T) {
	db := openTestDB(t, true)
	favs := NewFavoriteService(repository.NewFavoriteRepository(db))
	a := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	b := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	user, other := newUser(t, db), newUser(t, db)
	now := time.Now()

	for _, u := range []uuid.UUID{user, other} {
		follow(t, db, u, a.offer.EntityID, now.Add(-2*time.Hour))
		follow(t, db, u, b.offer.EntityID, now.Add(-2*time.Hour))
	}
	post(t, db, a.offer.EntityID, "de A", now.Add(-time.Hour))
	post(t, db, b.offer.EntityID, "de B", now.Add(-time.Hour))

	// Ver solo A descuenta A, no B, y no afecta a la otra persona.
	if err := favs.MarkNovedadesSeen(user, &a.offer.EntityID, now); err != nil {
		t.Fatalf("marcar visto: %v", err)
	}
	unseen := func(u uuid.UUID) map[string]bool {
		items, err := favs.Novedades(u, time.Now())
		if err != nil {
			t.Fatalf("Novedades: %v", err)
		}
		m := map[string]bool{}
		for _, it := range items {
			m[it.Title] = it.Unseen
		}
		return m
	}
	if got := unseen(user); got["de A"] || !got["de B"] {
		t.Errorf("tras ver A: %v, se esperaba A visto y B sin ver", got)
	}
	if got := unseen(other); !got["de A"] || !got["de B"] {
		t.Errorf("la otra persona: %v, no debía cambiar", got)
	}

	// Lo que se publica después de ver vuelve a salir sin ver.
	post(t, db, a.offer.EntityID, "otra de A", time.Now().Add(time.Minute))
	if got := unseen(user); !got["otra de A"] {
		t.Errorf("una publicación posterior a lo visto debía salir sin ver: %v", got)
	}

	// Ver todo.
	if err := favs.MarkNovedadesSeen(user, nil, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("ver todo: %v", err)
	}
	for title, u := range unseen(user) {
		if u {
			t.Errorf("%q sigue sin ver tras marcar todo", title)
		}
	}
}

func TestNovedades_FollowAgainOnlyShowsWhatComesAfter(t *testing.T) {
	db := openTestDB(t, true)
	repo := repository.NewFavoriteRepository(db)
	favs := NewFavoriteService(repo)
	f := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	user := newUser(t, db)
	entityID := f.offer.EntityID

	if err := repo.Add(user, entityID); err != nil {
		t.Fatalf("seguir: %v", err)
	}
	post(t, db, entityID, "mientras seguía", time.Now())
	if err := repo.Remove(user, entityID); err != nil {
		t.Fatalf("dejar de seguir: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := repo.Add(user, entityID); err != nil {
		t.Fatalf("volver a seguir: %v", err)
	}
	items, _ := favs.Novedades(user, time.Now().Add(time.Minute))
	if len(items) != 0 {
		t.Errorf("al volver a seguir solo cuenta lo posterior, hubo %+v", items)
	}
}

func TestFollowers_CountsPeopleWithoutExposingThem(t *testing.T) {
	db := openTestDB(t, true)
	favs := NewFavoriteService(repository.NewFavoriteRepository(db))
	f := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	for i := 0; i < 3; i++ {
		follow(t, db, newUser(t, db), f.offer.EntityID, time.Now())
	}
	other := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	follow(t, db, newUser(t, db), other.offer.EntityID, time.Now())

	count, err := favs.CountFollowers(f.offer.EntityID)
	if err != nil || count != 3 {
		t.Errorf("seguidores = %d, %v; se esperaban 3", count, err)
	}
	if none, _ := favs.CountFollowers(uuid.New()); none != 0 {
		t.Errorf("negocio sin seguidores = %d", none)
	}
}
