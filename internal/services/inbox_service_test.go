package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

func TestInboxSummary(t *testing.T) {
	db := testDB(t)
	mk := func(name string) models.User {
		u := models.User{Name: name, Email: name + "@x.co", PasswordHash: "x"}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		return u
	}
	owner, ana, beto, rival := mk("duena"), mk("ana"), mk("beto"), mk("rival")
	cat := models.Category{Name: "Belleza"}
	db.Create(&cat)
	ent := func(o models.User, name string) models.Entity {
		e := models.Entity{OwnerID: o.ID, Name: name, CategoryID: cat.ID}
		if err := db.Omit("Owner", "Category").Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e
	}
	faro, sazon, ajeno := ent(owner, "Barbería El Faro"), ent(owner, "La Sazón"), ent(rival, "Ajeno")
	msg := func(e models.Entity, customer uuid.UUID, byEntity, read bool) {
		sender := customer
		if byEntity {
			sender = e.OwnerID
		}
		m := models.Message{SenderID: sender, EntityID: e.ID, UserID: customer, SentByEntity: byEntity, Content: "hola", IsRead: read}
		if err := db.Omit("Entity", "User").Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	// El Faro: Ana escribió 2 sin leer (1 conversación), Beto 1 leído. La Sazón: Beto 1 sin leer.
	msg(faro, ana.ID, false, false)
	msg(faro, ana.ID, false, false)
	msg(faro, beto.ID, false, true)
	msg(sazon, beto.ID, false, false)
	// La dueña como clienta de un negocio ajeno: el negocio le respondió y no lo ha leído (personal).
	msg(ajeno, owner.ID, true, false)
	// Lo que la dueña escribió no cuenta como «sin leer» para ella.
	msg(ajeno, owner.ID, false, false)

	now := time.Now()
	appt := func(e models.Entity, customer uuid.UUID, status models.AppointmentStatus, expires *time.Time, offset time.Duration) {
		s := now.Add(48*time.Hour + offset)
		a := models.Appointment{EntityID: e.ID, Kind: models.AppointmentBooking, CustomerID: &customer, StartsAt: s, EndsAt: s.Add(30 * time.Minute), BlocksUntil: s.Add(30 * time.Minute), Status: status, ExpiresAt: expires}
		if err := db.Omit("Entity", "Customer", "Service").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	later, past := now.Add(time.Hour), now.Add(-time.Minute)
	appt(faro, ana.ID, models.ApptRequested, &later, 0)
	appt(faro, beto.ID, models.ApptRequested, &later, time.Hour)
	appt(faro, beto.ID, models.ApptRequested, &past, 2*time.Hour) // ya venció: no cuenta
	appt(faro, ana.ID, models.ApptConfirmed, nil, 3*time.Hour)    // confirmada: no está por revisar
	appt(ajeno, ana.ID, models.ApptRequested, &later, 0)          // de otro dueño

	got, err := NewInboxService(db).Summary(owner.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PersonalUnread != 1 {
		t.Errorf("personal sin leer = %d, quería 1", got.PersonalUnread)
	}
	if len(got.Businesses) != 2 {
		t.Fatalf("negocios = %+v", got.Businesses)
	}
	f, z := got.Businesses[0], got.Businesses[1] // orden por nombre: Barbería, La Sazón
	if f.EntityID != faro.ID || f.UnreadConversations != 1 || f.PendingRequests != 2 {
		t.Errorf("El Faro = %+v, quería 1 conversación sin leer y 2 por revisar", f)
	}
	if z.EntityID != sazon.ID || z.UnreadConversations != 1 || z.PendingRequests != 0 {
		t.Errorf("La Sazón = %+v", z)
	}
	// Un cliente sin negocios: solo lo personal, sin negocios.
	rv, _ := NewInboxService(db).Summary(beto.ID, now)
	if rv.PersonalUnread != 0 || len(rv.Businesses) != 0 {
		t.Errorf("Beto = %+v", rv)
	}
}
