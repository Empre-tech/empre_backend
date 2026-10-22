package services

// Integration tests for slot reservation, redemption and claim expiry. They
// need a real PostgreSQL (the guarantees come from conditional UPDATEs and a
// partial unique index), so they run only when TEST_DATABASE_URL is set, e.g.
//
//	TEST_DATABASE_URL="host=localhost port=5433 user=postgres password=postgres dbname=postgres sslmode=disable" go test ./internal/services/
//
// Each run works in its own throwaway schema, dropped at the end. Never point
// it at a database whose data matters.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"empre_backend/internal/database"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// openTestDB connects to TEST_DATABASE_URL inside a fresh schema with the
// full application schema migrated. The one-open-claim index is created
// unless the test needs to insert data that violates it first.
func openTestDB(t *testing.T, withClaimIndex bool) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no está definida: se omiten los tests de integración con PostgreSQL")
	}

	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("crear schema: %v", err)
	}

	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("conectar al schema: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(20)

	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if a, err := admin.DB(); err == nil {
			_ = a.Close()
		}
	})

	if err := db.AutoMigrate(
		&models.User{}, &models.Media{}, &models.Category{}, &models.Subcategory{},
		&models.Place{}, &models.Entity{}, &models.Post{}, &models.EntityPhoto{},
		&models.BusinessHour{}, &models.Offer{}, &models.OfferClaim{}, &models.Signal{}, &models.Review{}, &models.Favorite{}, &models.Subscription{}, &models.Zone{}, &models.Event{}, &models.ServiceItem{}, &models.OfferItem{}, &models.Promotion{}, &models.PromotionItem{},
	); err != nil {
		t.Fatalf("migrar: %v", err)
	}
	if withClaimIndex {
		if err := database.EnsureClaimIndexes(db); err != nil {
			t.Fatalf("índice de reclamos: %v", err)
		}
	}
	return db
}

type fixture struct {
	db      *gorm.DB
	svc     *ClaimService
	ownerID uuid.UUID
	offer   *models.Offer
}

func newFixture(t *testing.T, db *gorm.DB, slots int, typ models.OfferType, repeat models.RepeatPolicy) *fixture {
	t.Helper()
	owner := newUser(t, db)

	category := models.Category{Name: "Barberías"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("crear categoría: %v", err)
	}
	entity := models.Entity{OwnerID: owner, Name: "Barbería de prueba", CategoryID: category.ID, Latitude: 10.4, Longitude: -75.5}
	if err := db.Omit(clause.Associations).Create(&entity).Error; err != nil {
		t.Fatalf("crear negocio: %v", err)
	}

	now := utils.NowInCartagena()
	offer := &models.Offer{
		EntityID:     entity.ID,
		Type:         typ,
		Title:        "Corte + barba",
		Price:        "30000",
		StartsAt:     now,
		EndsAt:       now.Add(2 * time.Hour),
		TotalSlots:   slots,
		RadiusMeters: 500,
		RepeatPolicy: repeat,
	}
	if err := db.Omit(clause.Associations).Create(offer).Error; err != nil {
		t.Fatalf("crear oferta: %v", err)
	}

	svc := NewClaimService(
		repository.NewClaimRepository(db),
		repository.NewOfferRepository(db),
		repository.NewEntityRepository(db),
		repository.NewUserRepository(db),
		NewSignalService(repository.NewSignalRepository(db)),
	)
	return &fixture{db: db, svc: svc, ownerID: owner, offer: offer}
}

func newUser(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	u := models.User{Name: "Cliente", Email: uuid.NewString() + "@test.local", PasswordHash: "x"}
	if err := db.Omit(clause.Associations).Create(&u).Error; err != nil {
		t.Fatalf("crear usuario: %v", err)
	}
	return u.ID
}

func (f *fixture) usedSlots(t *testing.T) int {
	t.Helper()
	var o models.Offer
	if err := f.db.First(&o, "id = ?", f.offer.ID).Error; err != nil {
		t.Fatalf("leer oferta: %v", err)
	}
	return o.UsedSlots
}

func (f *fixture) countClaims(t *testing.T, status models.ClaimStatus) int64 {
	t.Helper()
	var n int64
	f.db.Model(&models.OfferClaim{}).Where("offer_id = ? AND status = ?", f.offer.ID, status).Count(&n)
	return n
}

// expireNow pushes the user's claims' window into the past, as if they lapsed.
func (f *fixture) expireNow(t *testing.T, claimIDs ...uuid.UUID) {
	t.Helper()
	if err := f.db.Model(&models.OfferClaim{}).Where("id IN ?", claimIDs).
		Update("expires_at", utils.NowInCartagena().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("vencer reclamos: %v", err)
	}
}

// runConcurrently starts n goroutines at once and returns how many succeeded.
func runConcurrently(n int, fn func(i int) error) (ok int, errs []error) {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		start = make(chan struct{})
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			err := fn(i)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else {
				errs = append(errs, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	return ok, errs
}

// Reserva: muchos usuarios a la vez nunca superan los cupos.
func TestClaim_ConcurrentUsersNeverOversell(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 5, models.OfferHours, models.RepeatOnce)

	users := make([]uuid.UUID, 40)
	for i := range users {
		users[i] = newUser(t, f.db)
	}

	ok, errs := runConcurrently(len(users), func(i int) error {
		_, err := f.svc.Claim(f.offer.ID, users[i], 0, 0)
		return err
	})

	if ok != 5 {
		t.Fatalf("reclamos exitosos = %d, se esperaban 5 (errores: %v)", ok, errs)
	}
	for _, err := range errs {
		if err.Error() != "oferta agotada" {
			t.Errorf("error inesperado: %v", err)
		}
	}
	if got := f.usedSlots(t); got != 5 {
		t.Errorf("used_slots = %d, se esperaba 5", got)
	}
	if got := f.countClaims(t, models.ClaimClaimed); got != 5 {
		t.Errorf("reclamos RECLAMADO = %d, se esperaban 5", got)
	}
}

// Reserva (D2 + carrera): el mismo usuario reclamando a la vez obtiene un
// solo reclamo y un solo cupo, con ambas políticas de repetición.
func TestClaim_SameUserConcurrentGetsOneSlot(t *testing.T) {
	for _, repeat := range []models.RepeatPolicy{models.RepeatOnce, models.RepeatDaily} {
		t.Run(string(repeat), func(t *testing.T) {
			f := newFixture(t, openTestDB(t, true), 10, models.OfferWeek, repeat)
			user := newUser(t, f.db)

			ok, errs := runConcurrently(20, func(int) error {
				_, err := f.svc.Claim(f.offer.ID, user, 0, 0)
				return err
			})

			if ok != 1 {
				t.Fatalf("reclamos exitosos = %d, se esperaba 1 (errores: %v)", ok, errs)
			}
			if got := f.usedSlots(t); got != 1 {
				t.Errorf("used_slots = %d, se esperaba 1: los intentos rechazados no devolvieron su cupo", got)
			}
		})
	}
}

// D2: con la política diaria, un segundo reclamo mientras el primero sigue
// abierto se rechaza. Sin el índice único, para probar la regla del servicio
// por sí sola (el índice es la segunda barrera, ante reclamos simultáneos).
func TestClaim_DailyRejectsSecondOpenClaim(t *testing.T) {
	f := newFixture(t, openTestDB(t, false), 10, models.OfferWeek, models.RepeatDaily)
	user := newUser(t, f.db)

	if _, err := f.svc.Claim(f.offer.ID, user, 0, 0); err != nil {
		t.Fatalf("primer reclamo: %v", err)
	}
	if _, err := f.svc.Claim(f.offer.ID, user, 0, 0); err == nil || err.Error() != "ya reclamaste esta oferta" {
		t.Fatalf("segundo reclamo: err = %v, se esperaba «ya reclamaste esta oferta»", err)
	}
	if got := f.usedSlots(t); got != 1 {
		t.Errorf("used_slots = %d, se esperaba 1", got)
	}
}

// D1: «una vez por usuario» se mantiene aunque haya pasado la ventana del
// reclamo canjeado.
func TestClaim_OnceStillBlockedAfterRedeemedWindowPasses(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 10, models.OfferHours, models.RepeatOnce)
	user := newUser(t, f.db)

	res, err := f.svc.Claim(f.offer.ID, user, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	if _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "test"); err != nil {
		t.Fatalf("canjear: %v", err)
	}
	f.expireNow(t, res.Claim.ID)

	if _, err := f.svc.Claim(f.offer.ID, user, 0, 0); err == nil || err.Error() != "ya reclamaste esta oferta" {
		t.Fatalf("reclamar de nuevo: err = %v, se esperaba «ya reclamaste esta oferta»", err)
	}
}

// Con «una vez», un reclamo que venció sin canjearse sí permite volver a
// reclamar, y su cupo no queda contado dos veces.
func TestClaim_OnceAllowsReclaimAfterUnusedClaimLapses(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 10, models.OfferHours, models.RepeatOnce)
	user := newUser(t, f.db)

	res, err := f.svc.Claim(f.offer.ID, user, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	f.expireNow(t, res.Claim.ID)

	if _, err := f.svc.Claim(f.offer.ID, user, 0, 0); err != nil {
		t.Fatalf("reclamar tras vencer: %v", err)
	}
	if got := f.usedSlots(t); got != 1 {
		t.Errorf("used_slots = %d, se esperaba 1 (el cupo vencido debía liberarse)", got)
	}
	if got := f.countClaims(t, models.ClaimExpired); got != 1 {
		t.Errorf("reclamos EXPIRADO = %d, se esperaba 1", got)
	}
}

// Canje: escaneos simultáneos del mismo reclamo (QR, código corto e id)
// producen un solo canje válido.
func TestRedeem_ConcurrentScansOnlyOneWins(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 5, models.OfferHours, models.RepeatOnce)
	user := newUser(t, f.db)

	res, err := f.svc.Claim(f.offer.ID, user, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	claim := res.Claim

	ok, errs := runConcurrently(30, func(i int) error {
		var err error
		switch i % 3 {
		case 0:
			_, err = f.svc.RedeemByToken(claim.Token, f.ownerID, "scanner")
		case 1:
			_, err = f.svc.RedeemByShortCode(claim.ShortCode, f.ownerID, "manual")
		default:
			_, err = f.svc.RedeemByID(claim.ID.String(), f.ownerID, "scanner")
		}
		return err
	})

	if ok != 1 {
		t.Fatalf("canjes exitosos = %d, se esperaba 1 (errores: %v)", ok, errs)
	}
	// Quien pierde la carrera recibe el motivo real, no un mensaje genérico.
	for _, err := range errs {
		var rej *RedeemRejection
		if !errors.As(err, &rej) || rej.Code != ReasonAlreadyRedeemed {
			t.Errorf("el escaneo perdedor devolvió %v, se esperaba ALREADY_REDEEMED", err)
		}
	}
	if got := f.countClaims(t, models.ClaimRedeemed); got != 1 {
		t.Errorf("reclamos CANJEADO = %d, se esperaba 1", got)
	}
	if got := f.usedSlots(t); got != 1 {
		t.Errorf("used_slots = %d, se esperaba 1 (el canje no libera ni duplica cupos)", got)
	}
}

// Canje: un reclamo vencido no se puede canjear y su cupo vuelve.
func TestRedeem_ExpiredClaimIsRejectedAndReleased(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 5, models.OfferHours, models.RepeatOnce)
	user := newUser(t, f.db)

	res, err := f.svc.Claim(f.offer.ID, user, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	f.expireNow(t, res.Claim.ID)

	if _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "scanner"); err == nil || err.Error() != "reclamo expirado" {
		t.Fatalf("canjear vencido: err = %v, se esperaba «reclamo expirado»", err)
	}
	if got := f.usedSlots(t); got != 0 {
		t.Errorf("used_slots = %d, se esperaba 0", got)
	}
}

// D3: el barrido libera los cupos de reclamos vencidos exactamente una vez,
// aunque se ejecute en paralelo.
func TestExpireOverdue_ReleasesEachSlotOnce(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 3, models.OfferHours, models.RepeatOnce)
	claimRepo := repository.NewClaimRepository(f.db)

	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		res, err := f.svc.Claim(f.offer.ID, newUser(t, f.db), 0, 0)
		if err != nil {
			t.Fatalf("reclamar %d: %v", i, err)
		}
		ids = append(ids, res.Claim.ID)
	}
	if _, err := f.svc.Claim(f.offer.ID, newUser(t, f.db), 0, 0); err == nil {
		t.Fatal("la oferta debía estar agotada")
	}

	f.expireNow(t, ids[0], ids[1])

	var (
		mu    sync.Mutex
		total int64
	)
	_, errs := runConcurrently(10, func(int) error {
		n, err := claimRepo.ExpireOverdue(utils.NowInCartagena())
		mu.Lock()
		total += n
		mu.Unlock()
		return err
	})
	if len(errs) > 0 {
		t.Fatalf("errores del barrido: %v", errs)
	}

	if total != 2 {
		t.Errorf("reclamos expirados en total = %d, se esperaban 2", total)
	}
	if got := f.usedSlots(t); got != 1 {
		t.Errorf("used_slots = %d, se esperaba 1", got)
	}
	if _, err := f.svc.Claim(f.offer.ID, newUser(t, f.db), 0, 0); err != nil {
		t.Errorf("tras liberar cupos, un usuario nuevo debía poder reclamar: %v", err)
	}
}

// D3: cancelar un reclamo que ya venció devuelve error, pero libera su cupo.
func TestCancel_LapsedClaimStillReleasesSlot(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 5, models.OfferHours, models.RepeatOnce)
	user := newUser(t, f.db)

	res, err := f.svc.Claim(f.offer.ID, user, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	f.expireNow(t, res.Claim.ID)

	if err := f.svc.Cancel(res.Claim.ID, user); err == nil {
		t.Fatal("cancelar un reclamo vencido debía devolver error")
	}
	if got := f.usedSlots(t); got != 0 {
		t.Errorf("used_slots = %d, se esperaba 0", got)
	}
}

// La migración limpia los reclamos abiertos duplicados que dejó el defecto
// D2 antes de crear el índice único, y libera sus cupos.
func TestEnsureClaimIndexes_CleansDuplicateOpenClaims(t *testing.T) {
	f := newFixture(t, openTestDB(t, false), 10, models.OfferWeek, models.RepeatDaily)
	user := newUser(t, f.db)

	now := utils.NowInCartagena()
	for i := 0; i < 3; i++ {
		c := models.OfferClaim{
			OfferID: f.offer.ID, UserID: user, EntityID: f.offer.EntityID,
			Status: models.ClaimClaimed, Token: uuid.NewString(), ShortCode: fmt.Sprintf("DUP%03d", i),
			ClaimedAt: now.Add(time.Duration(i) * time.Second), ExpiresAt: now.Add(time.Hour),
		}
		if err := f.db.Create(&c).Error; err != nil {
			t.Fatalf("insertar duplicado: %v", err)
		}
	}
	f.db.Model(&models.Offer{}).Where("id = ?", f.offer.ID).Update("used_slots", 3)

	if err := database.EnsureClaimIndexes(f.db); err != nil {
		t.Fatalf("EnsureClaimIndexes: %v", err)
	}
	// Idempotente.
	if err := database.EnsureClaimIndexes(f.db); err != nil {
		t.Fatalf("EnsureClaimIndexes (segunda vez): %v", err)
	}

	if got := f.countClaims(t, models.ClaimClaimed); got != 1 {
		t.Errorf("reclamos RECLAMADO = %d, se esperaba 1", got)
	}
	if got := f.countClaims(t, models.ClaimCanceled); got != 2 {
		t.Errorf("reclamos CANCELADO = %d, se esperaban 2", got)
	}
	if got := f.usedSlots(t); got != 1 {
		t.Errorf("used_slots = %d, se esperaba 1", got)
	}
}

func asRejection(t *testing.T, err error) *RedeemRejection {
	t.Helper()
	var rej *RedeemRejection
	if !errors.As(err, &rej) {
		t.Fatalf("se esperaba *RedeemRejection y llegó %T: %v", err, err)
	}
	return rej
}

// Cada motivo de rechazo llega con su código, y los de su propio negocio con
// el contexto que el escáner muestra.
func TestRedeem_RejectionsCarryReasonCodeAndContext(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 10, models.OfferHours, models.RepeatOnce)
	f.db.Model(&models.Offer{}).Where("id = ?", f.offer.ID).Updates(map[string]any{"conditions": "Solo con cita previa"})

	t.Run("ya canjeado trae cuándo, qué y a quién", func(t *testing.T) {
		client := newUser(t, f.db)
		f.db.Model(&models.User{}).Where("id = ?", client).Update("name", "María Fernanda Pérez")
		res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
		if err != nil {
			t.Fatalf("reclamar: %v", err)
		}
		if _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "scanner"); err != nil {
			t.Fatalf("primer canje: %v", err)
		}

		rej := asRejection(t, func() error { _, err := f.svc.RedeemByID(res.Claim.ID.String(), f.ownerID, "scanner"); return err }())
		if rej.Code != ReasonAlreadyRedeemed {
			t.Fatalf("code = %s, se esperaba ALREADY_REDEEMED", rej.Code)
		}
		if rej.Claim == nil || rej.Claim.RedeemedAt == nil {
			t.Error("falta cuándo se canjeó (Claim.RedeemedAt)")
		}
		if rej.Offer == nil || rej.Offer.Title != "Corte + barba" || rej.Offer.Price != "30000" || rej.Offer.Conditions != "Solo con cita previa" {
			t.Errorf("falta la oferta o está incompleta: %+v", rej.Offer)
		}
		if rej.ClientName != "María Fernanda Pérez" {
			t.Errorf("ClientName = %q", rej.ClientName)
		}
	})

	t.Run("cancelado", func(t *testing.T) {
		client := newUser(t, f.db)
		res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
		if err != nil {
			t.Fatalf("reclamar: %v", err)
		}
		if err := f.svc.Cancel(res.Claim.ID, client); err != nil {
			t.Fatalf("cancelar: %v", err)
		}
		rej := asRejection(t, func() error { _, err := f.svc.RedeemByShortCode(res.Claim.ShortCode, f.ownerID, "m"); return err }())
		if rej.Code != ReasonCanceled || rej.Offer == nil {
			t.Errorf("code = %s, offer = %v, se esperaba CANCELED con oferta", rej.Code, rej.Offer)
		}
	})

	t.Run("vencido trae cuándo venció", func(t *testing.T) {
		client := newUser(t, f.db)
		res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
		if err != nil {
			t.Fatalf("reclamar: %v", err)
		}
		f.expireNow(t, res.Claim.ID)
		rej := asRejection(t, func() error { _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "s"); return err }())
		if rej.Code != ReasonExpired {
			t.Fatalf("code = %s, se esperaba EXPIRED", rej.Code)
		}
		if rej.Claim == nil || !rej.Claim.ExpiresAt.Before(utils.NowInCartagena()) {
			t.Error("falta cuándo venció (Claim.ExpiresAt)")
		}
		// El estado ya quedó expirado en la base: un segundo escaneo da lo mismo.
		if again := asRejection(t, func() error { _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "s"); return err }()); again.Code != ReasonExpired {
			t.Errorf("segundo escaneo: code = %s, se esperaba EXPIRED", again.Code)
		}
	})

	t.Run("otro negocio no revela nada", func(t *testing.T) {
		client := newUser(t, f.db)
		res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
		if err != nil {
			t.Fatalf("reclamar: %v", err)
		}
		stranger := newUser(t, f.db)
		rej := asRejection(t, func() error { _, err := f.svc.RedeemByToken(res.Claim.Token, stranger, "s"); return err }())
		if rej.Code != ReasonWrongBusiness {
			t.Fatalf("code = %s, se esperaba WRONG_BUSINESS", rej.Code)
		}
		if rej.Claim != nil || rej.Offer != nil || rej.ClientName != "" {
			t.Errorf("filtra datos de otro negocio: claim=%v offer=%v cliente=%q", rej.Claim, rej.Offer, rej.ClientName)
		}
		// Y el reclamo sigue intacto para su verdadero dueño.
		if _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "s"); err != nil {
			t.Errorf("el canje legítimo falló tras el intento ajeno: %v", err)
		}
	})

	t.Run("códigos que no existen", func(t *testing.T) {
		checks := map[string]func() error{
			"uuid inválido":  func() error { _, err := f.svc.RedeemByID("no-es-uuid", f.ownerID, "s"); return err },
			"id desconocido": func() error { _, err := f.svc.RedeemByID(uuid.NewString(), f.ownerID, "s"); return err },
			"token":          func() error { _, err := f.svc.RedeemByToken("nope", f.ownerID, "s"); return err },
			"código corto":   func() error { _, err := f.svc.RedeemByShortCode("ZZZZZZ", f.ownerID, "m"); return err },
		}
		for name, run := range checks {
			if rej := asRejection(t, run()); rej.Code != ReasonNotFound || rej.Claim != nil {
				t.Errorf("%s: code = %s, claim = %v, se esperaba NOT_FOUND sin reclamo", name, rej.Code, rej.Claim)
			}
		}
	})

	t.Run("el código corto admite minúsculas y espacios", func(t *testing.T) {
		client := newUser(t, f.db)
		res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
		if err != nil {
			t.Fatalf("reclamar: %v", err)
		}
		typed := " " + strings.ToLower(res.Claim.ShortCode) + " "
		if _, err := f.svc.RedeemByShortCode(typed, f.ownerID, "m"); err != nil {
			t.Errorf("RedeemByShortCode(%q): %v", typed, err)
		}
	})
}

// Un canje válido devuelve el reclamo con su hora y la oferta completa, y el
// cupo restante refleja lo reservado.
func TestRedeem_ValidResultHasEverythingTheScannerShows(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 10, models.OfferHours, models.RepeatOnce)
	client := newUser(t, f.db)
	res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	out, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "scanner")
	if err != nil {
		t.Fatalf("canjear: %v", err)
	}
	if out.Claim.RedeemedAt == nil || out.Claim.ClaimedAt.IsZero() || out.Claim.ExpiresAt.IsZero() {
		t.Errorf("faltan horas en el reclamo: %+v", out.Claim)
	}
	if out.Offer.Title == "" || out.Offer.Price != "30000" || out.EntityName == "" {
		t.Errorf("faltan datos de la oferta o del negocio: %+v / %q", out.Offer, out.EntityName)
	}
	if left := out.Offer.TotalSlots - out.Offer.UsedSlots; left != 9 {
		t.Errorf("cupos libres = %d, se esperaban 9", left)
	}
}

// El vencimiento del reclamo sale de la ventana del tipo de oferta y nunca pasa
// del final de la oferta.
func TestClaim_ExpiryFollowsOfferTypeWindow(t *testing.T) {
	db := openTestDB(t, true)

	hours := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	res, err := hours.svc.Claim(hours.offer.ID, newUser(t, db), 0, 0)
	if err != nil {
		t.Fatalf("reclamar (horas): %v", err)
	}
	if got := res.Claim.ExpiresAt.Sub(res.Claim.ClaimedAt); got != 30*time.Minute {
		t.Errorf("ventana por horas = %v, se esperaban 30 min", got)
	}

	// La oferta de la semana da 24 h, pero esta termina en 2 h: manda el final de la oferta.
	week := newFixture(t, db, 5, models.OfferWeek, models.RepeatOnce)
	res, err = week.svc.Claim(week.offer.ID, newUser(t, db), 0, 0)
	if err != nil {
		t.Fatalf("reclamar (semana): %v", err)
	}
	if diff := res.Claim.ExpiresAt.Sub(week.offer.EndsAt); diff > time.Millisecond || diff < -time.Millisecond {
		t.Errorf("el vencimiento debía recortarse al fin de la oferta: diferencia %v", diff)
	}
	if res.EntityName == "" {
		t.Error("el resultado del reclamo debe traer el nombre del negocio")
	}
}

// «Mis reclamos»: solo los del usuario, solo los recientes, los más nuevos
// primero, y con la oferta y el negocio ya resueltos.
func TestListMine_OnlyMyRecentClaimsNewestFirst(t *testing.T) {
	db := openTestDB(t, true)
	me := newUser(t, db)
	other := newUser(t, db)

	older := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	newer := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)
	ancient := newFixture(t, db, 5, models.OfferHours, models.RepeatOnce)

	first, err := older.svc.Claim(older.offer.ID, me, 0, 0)
	if err != nil {
		t.Fatalf("reclamar 1: %v", err)
	}
	second, err := newer.svc.Claim(newer.offer.ID, me, 0, 0)
	if err != nil {
		t.Fatalf("reclamar 2: %v", err)
	}
	third, err := ancient.svc.Claim(ancient.offer.ID, me, 0, 0)
	if err != nil {
		t.Fatalf("reclamar 3: %v", err)
	}
	if _, err := older.svc.Claim(older.offer.ID, other, 0, 0); err != nil {
		t.Fatalf("reclamo de otra persona: %v", err)
	}

	now := utils.NowInCartagena()
	db.Model(&models.OfferClaim{}).Where("id = ?", first.Claim.ID).Update("claimed_at", now.Add(-time.Hour))
	db.Model(&models.OfferClaim{}).Where("id = ?", third.Claim.ID).Update("claimed_at", now.Add(-72*time.Hour))

	got, err := older.svc.ListMine(me)
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("reclamos = %d, se esperaban 2 (el de hace 72 h y el ajeno no cuentan)", len(got))
	}
	if got[0].Claim.ID != second.Claim.ID || got[1].Claim.ID != first.Claim.ID {
		t.Errorf("el orden debía ser del más nuevo al más viejo")
	}
	for _, r := range got {
		if r.Claim.UserID != me {
			t.Errorf("apareció un reclamo de otra persona: %s", r.Claim.UserID)
		}
		if r.Offer == nil || r.Offer.Title != "Corte + barba" {
			t.Errorf("falta la oferta resuelta: %+v", r.Offer)
		}
		if r.EntityName != "Barbería de prueba" {
			t.Errorf("EntityName = %q", r.EntityName)
		}
	}

	// Quien no reclamó nada recibe una lista vacía, no un error.
	empty, err := older.svc.ListMine(newUser(t, db))
	if err != nil || len(empty) != 0 {
		t.Errorf("sin reclamos: %v, %d resultados", err, len(empty))
	}
}

// Describe devuelve la oferta y el negocio de un reclamo suelto.
func TestDescribe_ResolvesOfferAndBusiness(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 5, models.OfferHours, models.RepeatOnce)
	res, err := f.svc.Claim(f.offer.ID, newUser(t, f.db), 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}
	got, err := f.svc.Describe(res.Claim)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got.Offer.ID != f.offer.ID || got.EntityName != "Barbería de prueba" {
		t.Errorf("Describe devolvió %+v / %q", got.Offer, got.EntityName)
	}
}

func (f *fixture) redeemSignals(t *testing.T) int64 {
	t.Helper()
	var n int64
	f.db.Model(&models.Signal{}).Where("entity_id = ? AND type = ?", f.offer.EntityID, models.SignalRedeem).Count(&n)
	return n
}

// Solo cuentan los canjes verificados: canjear no suma popularidad, confirmar sí,
// una sola vez, aunque el cliente confirme muchas veces a la vez.
func TestConfirm_OnlyVerifiedRedemptionsCountAsSignal(t *testing.T) {
	f := newFixture(t, openTestDB(t, true), 5, models.OfferHours, models.RepeatOnce)
	client := newUser(t, f.db)

	res, err := f.svc.Claim(f.offer.ID, client, 0, 0)
	if err != nil {
		t.Fatalf("reclamar: %v", err)
	}

	// Confirmar antes de que el negocio canjee no es posible.
	if err := f.svc.Confirm(res.Claim.ID, client); err == nil {
		t.Fatal("confirmar un reclamo sin canjear debía fallar")
	}

	if _, err := f.svc.RedeemByToken(res.Claim.Token, f.ownerID, "scanner"); err != nil {
		t.Fatalf("canjear: %v", err)
	}
	if got := f.redeemSignals(t); got != 0 {
		t.Fatalf("canjear registró %d señales de popularidad; debía registrar 0 hasta que el cliente confirme", got)
	}

	// Otra persona no puede confirmar el canje ajeno.
	if err := f.svc.Confirm(res.Claim.ID, newUser(t, f.db)); err == nil {
		t.Fatal("otra persona pudo confirmar un canje que no era suyo")
	}
	if got := f.redeemSignals(t); got != 0 {
		t.Fatalf("una confirmación ajena registró %d señales", got)
	}

	// Veinte confirmaciones simultáneas del cliente: una sola señal.
	ok, errs := runConcurrently(20, func(int) error { return f.svc.Confirm(res.Claim.ID, client) })
	if ok != 20 {
		t.Fatalf("confirmaciones sin error = %d de 20 (%v)", ok, errs)
	}
	if got := f.redeemSignals(t); got != 1 {
		t.Errorf("señales de canje = %d, se esperaba exactamente 1", got)
	}
	var verified bool
	f.db.Model(&models.OfferClaim{}).Select("verified_by_client").Where("id = ?", res.Claim.ID).Scan(&verified)
	if !verified {
		t.Error("el reclamo debía quedar verificado")
	}
}
