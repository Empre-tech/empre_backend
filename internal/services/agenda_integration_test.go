package services

import (
	"empre_backend/internal/agenda"
	"os"
	"testing"
	"time"

	"empre_backend/internal/database"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Integration test against a real Postgres. Skipped unless AGENDA_TEST_DSN is
// set, e.g. "host=/tmp port=5544 user=pgtest dbname=empre_test sslmode=disable".
func testDB(t *testing.T) *gorm.DB {
	// La misma base de pruebas que el resto de la suite (TEST_DATABASE_URL); AGENDA_TEST_DSN sigue valiendo.
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("AGENDA_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no definido")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public")
	db.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto")
	if err := db.AutoMigrate(&models.User{}, &models.Entity{}, &models.Message{}, &models.Category{}, &models.Media{},
		&models.BusinessHour{}, &models.Subscription{}, &models.AgendaSettings{}, &models.ServiceItem{}, &models.Appointment{},
		&models.Party{}, &models.Sale{}, &models.SaleLine{}, &models.Expense{}, &models.DebtPayment{}, &models.VoiceDraft{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgendaConstraints(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAgendaFlow(t *testing.T) {
	db := testDB(t)
	svc := NewAgendaService(db, nil)

	owner := models.User{Name: "Sara", Email: "sara@x.co", PasswordHash: "x"}
	c1 := models.User{Name: "Andrés", Email: "a@x.co", PasswordHash: "x"}
	c2 := models.User{Name: "Luisa", Email: "l@x.co", PasswordHash: "x"}
	for _, u := range []*models.User{&owner, &c1, &c2} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Belleza"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	ent := models.Entity{OwnerID: owner.ID, Name: "Peluquería Sara", CategoryID: cat.ID}
	if err := db.Omit("Owner", "Category").Create(&ent).Error; err != nil {
		t.Fatal(err)
	}
	for wd := 0; wd <= 6; wd++ {
		db.Create(&models.BusinessHour{EntityID: ent.ID, Weekday: wd, OpenTime: "00:00", CloseTime: "23:59"})
	}
	// Todos los días 00:00–23:59 evita depender de la hora a la que corre la prueba.

	// Sin activar: la agenda no existe para el cliente.
	if _, err := svc.Availability(c1.ID, ent.ID, nowCO(), nil); err != ErrAgendaDisabled {
		t.Fatalf("esperaba agenda_disabled, got %v", err)
	}
	if _, err := svc.UpdateSettings(c1.ID, ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14}); err != ErrApptForbidden {
		t.Fatalf("un cliente no puede configurar: %v", err)
	}
	if _, err := svc.UpdateSettings(owner.ID, ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, BufferMin: 10, HorizonDays: 14, IntakeMode: strp(agenda.IntakeManual)}); err != nil {
		t.Fatal(err)
	}
	tall, err := svc.CreateService(owner.ID, ent.ID, AgendaServiceInput{Name: "Tinte", DurationMin: 60})
	if err != nil {
		t.Fatal(err)
	}

	// Con servicios definidos hay que elegir uno.
	day := nowCO().AddDate(0, 0, 2)
	if _, err := svc.Availability(c1.ID, ent.ID, day, nil); err == nil {
		t.Fatal("debería pedir servicio")
	}
	slots, err := svc.Availability(c1.ID, ent.ID, day, &tall.ID)
	if err != nil || len(slots) < 4 {
		t.Fatalf("slots=%d err=%v", len(slots), err)
	}
	start := slots[10] // alrededor de 5:00 a.m. con horario 00:00–23:59

	a1, err := svc.CreateAppointment(c1.ID, ent.ID, &tall.ID, start, "nota", "")
	if err != nil {
		t.Fatal(err)
	}
	if a1.Status != models.ApptRequested || a1.ExpiresAt == nil || !a1.ExpiresAt.Before(a1.StartsAt.Add(time.Second)) {
		t.Fatalf("estado/expiración raros: %+v", a1)
	}
	if !a1.EndsAt.Equal(a1.StartsAt.Add(time.Hour)) {
		t.Fatalf("el Tinte dura 60 min: %v-%v", a1.StartsAt, a1.EndsAt)
	}
	var cards int64
	db.Model(&models.Message{}).Where("appointment_id = ? AND kind = 'appointment'", a1.ID).Count(&cards)
	if cards != 1 {
		t.Fatalf("falta la tarjeta del chat: %d", cards)
	}

	// Mismo bloque: slot_taken con alternativas.
	_, err = svc.CreateAppointment(c2.ID, ent.ID, &tall.ID, start, "", "")
	taken, ok := err.(*SlotTakenError)
	if !ok || len(taken.Alternatives) == 0 {
		t.Fatalf("esperaba slot_taken con alternativas, got %v", err)
	}
	// Solape parcial (30 min después) también choca (cita de 60 + margen).
	if _, err = svc.CreateAppointment(c2.ID, ent.ID, &tall.ID, start.Add(30*time.Minute), "", ""); err == nil {
		t.Fatal("el solape parcial debería fallar")
	}
	// La base de datos misma lo impide aunque se salte el código.
	dup := models.Appointment{EntityID: ent.ID, Kind: models.AppointmentBooking, CustomerID: &c2.ID, StartsAt: start, EndsAt: start.Add(time.Hour),
		BlocksUntil: start.Add(70 * time.Minute), Status: models.ApptRequested}
	if err := db.Create(&dup).Error; !isOverlapViolation(err) {
		t.Fatalf("la restricción de exclusión no actuó: %v", err)
	}

	// El cliente no puede aceptar su propia cita; el dueño sí.
	if _, err := svc.Accept(c1.ID, a1.ID); err != ErrApptForbidden {
		t.Fatalf("cliente aceptando: %v", err)
	}
	a1, err = svc.Accept(owner.ID, a1.ID)
	if err != nil || a1.Status != models.ApptConfirmed || a1.ClosingDeadline == nil {
		t.Fatalf("accept: %v %+v", err, a1)
	}
	if _, err := svc.Accept(owner.ID, a1.ID); err != ErrBadState {
		t.Fatalf("aceptar dos veces: %v", err)
	}

	// Segunda cita: rechazar con alternativa.
	other := slots[20]
	a2, err := svc.CreateAppointment(c2.ID, ent.ID, &tall.ID, other, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decline(owner.ID, a2.ID, DeclineInput{ReasonCode: "xx"}); err == nil {
		t.Fatal("motivo inválido")
	}
	a2, err = svc.Decline(owner.ID, a2.ID, DeclineInput{ReasonCode: "no_space", Propose: []time.Time{slots[25], start /* ocupado */, slots[26]}})
	if err != nil || a2.Status != models.ApptDeclined || len(a2.Proposed) != 2 {
		t.Fatalf("decline: %v proposed=%v", err, a2.Proposed)
	}
	// El espacio rechazado vuelve a estar libre.
	if _, err := svc.CreateAppointment(c1.ID, ent.ID, &tall.ID, other, "", ""); err != nil {
		t.Fatalf("debería liberarse: %v", err)
	}

	// Bloqueo sobre cita viva: 409 salvo force; con force se cancela la cita.
	_, err = svc.CreateBlock(owner.ID, ent.ID, start, start.Add(time.Hour), false)
	if ov, ok := err.(*OverlapsBookingError); !ok || len(ov.Appointments) != 1 {
		t.Fatalf("overlaps_booking: %v", err)
	}
	if _, err = svc.CreateBlock(owner.ID, ent.ID, start, start.Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetAppointment(c1.ID, a1.ID)
	if got.Status != models.ApptCancelled || got.CancelledBy != "business" {
		t.Fatalf("la cita debía cancelarse: %+v", got.Status)
	}
	// El bloqueo ocupa el espacio.
	if _, err := svc.CreateAppointment(c2.ID, ent.ID, &tall.ID, start, "", ""); err == nil {
		t.Fatal("el bloqueo debería ocupar el espacio")
	}

	// Cancelación tardía: cita confirmada que empieza en < 2 horas.
	soon := models.Appointment{EntityID: ent.ID, Kind: models.AppointmentBooking, CustomerID: &c2.ID, StartsAt: time.Now().Add(90 * time.Minute),
		EndsAt: time.Now().Add(150 * time.Minute), BlocksUntil: time.Now().Add(160 * time.Minute), Status: models.ApptConfirmed}
	db.Create(&soon)
	c, err := svc.Cancel(c2.ID, soon.ID)
	if err != nil || !c.CancelledLate || c.CancelledBy != "customer" {
		t.Fatalf("cancelación tardía: %v %+v", err, c)
	}

	// Límite de solicitudes pendientes por cliente y negocio.
	var last error
	for i := 0; i < 4; i++ {
		_, last = svc.CreateAppointment(c2.ID, ent.ID, &tall.ID, slots[30+i*3], "", "")
	}
	if last != ErrTooManyPending {
		t.Fatalf("esperaba too_many_pending, got %v", last)
	}
}

func TestAgendaJobs(t *testing.T) {
	db := testDB(t)
	svc := NewAgendaService(db, nil)
	owner := models.User{Name: "Sara", Email: "sara@x.co", PasswordHash: "x"}
	cust := models.User{Name: "Andrés", Email: "a@x.co", PasswordHash: "x"}
	db.Create(&owner)
	db.Create(&cust)
	cat := models.Category{Name: "Belleza"}
	db.Create(&cat)
	ent := models.Entity{OwnerID: owner.ID, Name: "Peluquería", CategoryID: cat.ID}
	db.Omit("Owner", "Category").Create(&ent)

	now := time.Now()
	mk := func(status models.AppointmentStatus, start time.Time, mutate func(*models.Appointment)) *models.Appointment {
		a := &models.Appointment{EntityID: ent.ID, Kind: models.AppointmentBooking, CustomerID: &cust.ID,
			StartsAt: start, EndsAt: start.Add(30 * time.Minute), BlocksUntil: start.Add(30 * time.Minute), Status: status}
		if mutate != nil {
			mutate(a)
		}
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
		return a
	}
	past := now.Add(-time.Minute)
	expiring := mk(models.ApptRequested, now.Add(5*time.Hour), func(a *models.Appointment) { a.ExpiresAt = &past })
	fresh := mk(models.ApptRequested, now.Add(30*time.Hour), func(a *models.Appointment) { e := now.Add(time.Hour); a.ExpiresAt = &e })
	// Cita terminada hace 3 días; nadie respondió y venció el plazo de cierre.
	dl := now.Add(-time.Hour)
	ghost := mk(models.ApptConfirmed, now.Add(-72*time.Hour), func(a *models.Appointment) { a.ClosingDeadline = &dl })
	yes, no := true, false
	split := mk(models.ApptConfirmed, now.Add(-96*time.Hour), func(a *models.Appointment) {
		a.CustomerAttended, a.BusinessAttended = &yes, &no
		a.ClosingDeadline = &dl
	})

	svc.Tick(now)

	status := func(id uuid.UUID) models.AppointmentStatus {
		var a models.Appointment
		db.First(&a, "id = ?", id)
		return a.Status
	}
	if status(expiring.ID) != models.ApptExpired {
		t.Errorf("debía expirar: %s", status(expiring.ID))
	}
	if status(fresh.ID) != models.ApptRequested {
		t.Errorf("no debía expirar: %s", status(fresh.ID))
	}
	if status(ghost.ID) != models.ApptCompleted {
		t.Errorf("sin respuestas cierra completada: %s", status(ghost.ID))
	}
	if status(split.ID) != models.ApptDisputed {
		t.Errorf("respuestas distintas = disputed: %s", status(split.ID))
	}
	// Idempotencia: otro tick no cambia nada ni falla.
	svc.Tick(now)
	if status(expiring.ID) != models.ApptExpired {
		t.Error("segundo tick cambió el estado")
	}
}

func strp(s string) *string { return &s }
