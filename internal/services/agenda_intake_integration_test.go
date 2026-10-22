package services

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"empre_backend/internal/agenda"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Recepción automática, avisos al dueño y reservas simultáneas (CONVERSACIONES_Y_AGENDA.md §9b), con PostgreSQL.

type intakeFixture struct {
	db      *gorm.DB
	svc     *AgendaService
	owner   models.User
	ent     models.Entity
	service BookableService
	slots   []time.Time
}

func newIntakeFixture(t *testing.T, customers int) (*intakeFixture, []models.User) {
	db := testDB(t)
	f := &intakeFixture{db: db, svc: NewAgendaService(db, nil)}
	f.owner = models.User{Name: "Dueña", Email: "duena@x.co", PasswordHash: "x"}
	if err := db.Create(&f.owner).Error; err != nil {
		t.Fatal(err)
	}
	cs := make([]models.User, customers)
	for i := range cs {
		cs[i] = models.User{Name: fmt.Sprintf("Cliente %d", i), Email: fmt.Sprintf("c%d@x.co", i), PasswordHash: "x"}
		if err := db.Create(&cs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Belleza"}
	db.Create(&cat)
	f.ent = models.Entity{OwnerID: f.owner.ID, Name: "Barbería", CategoryID: cat.ID}
	if err := db.Omit("Owner", "Category").Create(&f.ent).Error; err != nil {
		t.Fatal(err)
	}
	for wd := 0; wd <= 6; wd++ {
		db.Create(&models.BusinessHour{EntityID: f.ent.ID, Weekday: wd, OpenTime: "00:00", CloseTime: "23:59"})
	}
	if _, err := f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14}); err != nil {
		t.Fatal(err)
	}
	var err error
	if f.service, err = f.svc.CreateService(f.owner.ID, f.ent.ID, AgendaServiceInput{Name: "Corte", DurationMin: 30}); err != nil {
		t.Fatal(err)
	}
	if f.slots, err = f.svc.Availability(cs[0].ID, f.ent.ID, nowCO().AddDate(0, 0, 3), &f.service.ID); err != nil || len(f.slots) < 10 {
		t.Fatalf("slots=%d err=%v", len(f.slots), err)
	}
	return f, cs
}

func (f *intakeFixture) addHistory(t *testing.T, customer uuid.UUID, status models.AppointmentStatus, n int) {
	base := nowCO().AddDate(0, 0, -20)
	for i := 0; i < n; i++ {
		s := base.Add(time.Duration(i) * 24 * time.Hour)
		a := models.Appointment{EntityID: f.ent.ID, Kind: models.AppointmentBooking, CustomerID: &customer, StartsAt: s, EndsAt: s.Add(30 * time.Minute), BlocksUntil: s.Add(30 * time.Minute), Status: status}
		if err := f.db.Omit("Entity", "Customer", "Service").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func strpp(s string) *string { return &s }
func intp2(i int) *int        { return &i }

func TestAgendaIntakeModes(t *testing.T) {
	f, cs := newIntakeFixture(t, 4)
	// Por defecto, con reglas: faltas >= 2 a revisión; un cliente sin faltas se confirma solo.
	st, _ := f.svc.settings(f.ent.ID)
	if st.IntakeMode != agenda.IntakeRules || st.ReviewNoShows != 2 || st.ReviewNoticeMin != 120 || st.NotifyEvery != 60 || st.QuietMode != agenda.QuietHours {
		t.Fatalf("valores por defecto: %+v", st)
	}
	f.addHistory(t, cs[1].ID, models.ApptNoShow, 2)
	ok, err := f.svc.CreateAppointment(cs[0].ID, f.ent.ID, &f.service.ID, f.slots[0], "", "")
	if err != nil || ok.Status != models.ApptConfirmed || ok.ExpiresAt != nil || ok.ClosingDeadline == nil {
		t.Fatalf("cliente sin faltas debía confirmarse solo: %+v %v", ok, err)
	}
	risky, err := f.svc.CreateAppointment(cs[1].ID, f.ent.ID, &f.service.ID, f.slots[2], "", "")
	if err != nil || risky.Status != models.ApptRequested || risky.ExpiresAt == nil {
		t.Fatalf("cliente con 2 faltas debía quedar por revisar: %+v %v", risky, err)
	}
	// La tarjeta del chat dice lo que pasó.
	var cards []models.Message
	f.db.Where("kind = ?", "appointment").Order("created_at").Find(&cards)
	if len(cards) != 2 || !strings.HasPrefix(cards[0].Content, "Reservó") || !strings.HasPrefix(cards[1].Content, "Pidió") {
		t.Fatalf("tarjetas: %+v", cards)
	}
	// Manual: todo a revisión. Automático: todo confirmado, aunque tenga faltas.
	if _, err := f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14, IntakeMode: strpp(agenda.IntakeManual)}); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.CreateAppointment(cs[2].ID, f.ent.ID, &f.service.ID, f.slots[4], "", ""); a == nil || a.Status != models.ApptRequested {
		t.Fatalf("manual debía dejarla por revisar: %+v", a)
	}
	if _, err := f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14, IntakeMode: strpp(agenda.IntakeAuto)}); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.CreateAppointment(cs[1].ID, f.ent.ID, &f.service.ID, f.slots[6], "", ""); a == nil || a.Status != models.ApptConfirmed {
		t.Fatalf("automático debía confirmar aunque tenga faltas: %+v", a)
	}
	// Ajustes inválidos se rechazan; los que no llegan no se tocan.
	if _, err := f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14, IntakeMode: strpp("magia")}); err == nil {
		t.Fatal("intake_mode inválido aceptado")
	}
	if _, err := f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14, QuietMode: strpp(agenda.QuietCustom)}); err == nil {
		t.Fatal("silencio elegido sin horas aceptado")
	}
	if _, err := f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14, NotifyEvery: intp2(45)}); err == nil {
		t.Fatal("notify_every 45 aceptado")
	}
	st, _ = f.svc.settings(f.ent.ID)
	if st.IntakeMode != agenda.IntakeAuto || st.NotifyEvery != 60 {
		t.Fatalf("un ajuste que no llegó no debía cambiar: %+v", st)
	}
}

func TestAgendaIdempotentAndConcurrent(t *testing.T) {
	f, cs := newIntakeFixture(t, 41)
	// El mismo intento dos veces (y a la vez): una sola cita, el mismo id.
	ids := make([]string, 6)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, err := f.svc.CreateAppointment(cs[40].ID, f.ent.ID, &f.service.ID, f.slots[8], "", "intento-1")
			if err != nil {
				t.Errorf("reintento %d: %v", i, err)
				return
			}
			ids[i] = a.ID.String()
		}(i)
	}
	wg.Wait()
	var n int64
	f.db.Model(&models.Appointment{}).Where("customer_id = ?", cs[40].ID).Count(&n)
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("reintentos con ids distintos: %v", ids)
		}
	}
	if n != 1 {
		t.Fatalf("reintentos crearon %d citas", n)
	}
	// 40 clientes piden la misma hora al mismo tiempo: una gana, el resto recibe «ocupada» con alternativas;
	// ninguno recibe un error del servidor (antes, interbloqueos 40P01 sin manejar).
	start := make(chan struct{})
	var mu sync.Mutex
	won, taken, other := 0, 0, 0
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := f.svc.CreateAppointment(cs[i].ID, f.ent.ID, &f.service.ID, f.slots[0], "", "")
			var st *SlotTakenError
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.As(err, &st):
				taken++
			default:
				other++
				t.Errorf("cliente %d: error inesperado %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	var overlaps int64
	f.db.Raw(`SELECT count(*) FROM appointments a JOIN appointments b ON a.id < b.id AND a.entity_id = b.entity_id
		AND tstzrange(a.starts_at, a.blocks_until) && tstzrange(b.starts_at, b.blocks_until)
		WHERE a.status IN ('requested','confirmed','blocked') AND b.status IN ('requested','confirmed','blocked')`).Scan(&overlaps)
	t.Logf("40 a la vez: ganó %d, ocupada %d, otros errores %d, solapamientos %d", won, taken, other, overlaps)
	if won != 1 || taken != 39 || other != 0 || overlaps != 0 {
		t.Fatalf("ganó %d, ocupada %d, otros %d, solapamientos %d", won, taken, other, overlaps)
	}
}

func TestAgendaOwnerDigest(t *testing.T) {
	f, cs := newIntakeFixture(t, 3)
	// Avisos cada hora (por defecto): nada sale al crear; el resumen sale en el siguiente pase y no se repite.
	for i := 0; i < 3; i++ {
		if _, err := f.svc.CreateAppointment(cs[i].ID, f.ent.ID, &f.service.ID, f.slots[i*2], "", ""); err != nil {
			t.Fatal(err)
		}
	}
	var pending int64
	f.db.Model(&models.Appointment{}).Where("owner_notified_at IS NULL").Count(&pending)
	if pending != 3 {
		t.Fatalf("con resumen cada hora no se avisa al crear: %d pendientes", pending)
	}
	now := time.Now()
	f.svc.digestOwners(now)
	f.db.Model(&models.Appointment{}).Where("owner_notified_at IS NULL").Count(&pending)
	st, _ := f.svc.settings(f.ent.ID)
	if pending != 0 || st.LastDigestAt == nil {
		t.Fatalf("el resumen debía marcar las 3: %d pendientes, último %v", pending, st.LastDigestAt)
	}
	// Llega otra antes de la hora: espera al siguiente resumen.
	extra := models.User{Name: "Otro", Email: "otro@x.co", PasswordHash: "x"}
	f.db.Create(&extra)
	if _, err := f.svc.CreateAppointment(extra.ID, f.ent.ID, &f.service.ID, f.slots[8], "", ""); err != nil {
		t.Fatal(err)
	}
	f.svc.digestOwners(now.Add(10 * time.Minute))
	f.db.Model(&models.Appointment{}).Where("owner_notified_at IS NULL").Count(&pending)
	if pending != 1 {
		t.Fatalf("antes de la hora no debía salir otro resumen: %d pendientes", pending)
	}
	// En silencio (elegido para cubrir todo el día salvo un minuto) tampoco sale, aunque pasó la hora.
	from := now.In(nowCO().Location()).Add(-time.Hour).Format("15:04")
	to := now.In(nowCO().Location()).Add(2 * time.Hour).Format("15:04")
	if _, err := f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14, QuietFrom: &from, QuietTo: &to, QuietMode: strpp(agenda.QuietCustom)}); err != nil {
		t.Fatal(err)
	}
	f.svc.digestOwners(now.Add(61 * time.Minute))
	f.db.Model(&models.Appointment{}).Where("owner_notified_at IS NULL").Count(&pending)
	if pending != 1 {
		t.Fatalf("en silencio no debía salir el resumen: %d pendientes", pending)
	}
	// Al salir del silencio, sale.
	f.svc.UpdateSettings(f.owner.ID, f.ent.ID, SettingsInput{Enabled: true, SlotMinutes: 30, HorizonDays: 14, QuietMode: strpp(agenda.QuietOff)})
	f.svc.digestOwners(now.Add(61 * time.Minute))
	f.db.Model(&models.Appointment{}).Where("owner_notified_at IS NULL").Count(&pending)
	if pending != 0 {
		t.Fatalf("fuera del silencio debía salir: %d pendientes", pending)
	}
	if DigestText(3, 0) != "3 solicitudes por revisar" || DigestText(1, 2) != "1 solicitud por revisar · 2 citas nuevas" || DigestText(0, 1) != "1 cita nueva" {
		t.Fatal("texto del resumen")
	}
}
