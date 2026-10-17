package services

import (
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func TestTrimSnippet(t *testing.T) {
	if got := TrimSnippet("  Muy buena\n atención   y rápido "); got != "Muy buena atención y rápido" {
		t.Fatalf("una línea y sin espacios de más: %q", got)
	}
	long := strings.Repeat("palabra ", 30)
	got := TrimSnippet(long)
	if n := len([]rune(got)); n > SnippetMaxChars+1 || !strings.HasSuffix(got, "…") || strings.Contains(got, "palabr…") {
		t.Fatalf("se corta en una palabra y termina en …: %q (%d)", got, n)
	}
	if got := TrimSnippet(strings.Repeat("x", 200)); len([]rune(got)) != SnippetMaxChars+1 {
		t.Fatalf("sin espacios corta duro: %d", len([]rune(got)))
	}
	if got := strings.Repeat("ñ", SnippetMaxChars); TrimSnippet(got) != got {
		t.Fatal("justo en el tope no se corta (cuenta letras, no bytes)")
	}
}

func TestReviewSnippets_PicksLatestVerifiedWithComment(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	cat := models.Category{Name: "Cafeterías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(name string) uuid.UUID {
		e := models.Entity{OwnerID: owner, Name: name, CategoryID: cat.ID, Latitude: 11.2, Longitude: -74.2}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour
	rev := func(entity uuid.UUID, rating int, comment string, verified bool, at time.Time) {
		r := models.Review{EntityID: entity, UserID: newUser(t, db), Rating: rating, Comment: comment, CreatedAt: at, UpdatedAt: at}
		if verified {
			c := uuid.New()
			r.ClaimID = &c
		}
		if err := db.Omit(clause.Associations).Create(&r).Error; err != nil {
			t.Fatalf("crear reseña: %v", err)
		}
	}
	good := "Excelente café y muy buena atención, volveré pronto"

	a := mk("A: elige la más reciente que sirve")
	rev(a, 5, good+" (vieja)", true, ago(30*day))
	rev(a, 2, "Tardaron demasiado en atenderme esta vez", true, ago(2*day))
	rev(a, 5, "Increíble lugar, lo mejor de la zona sin duda", false, ago(1*day)) // más nueva pero no verificada
	rev(a, 4, "Ok", true, ago(time.Hour))                                         // verificada y nueva, pero sin comentario real

	b := mk("B: solo reseñas que no sirven")
	rev(b, 5, good, false, ago(day))         // sin verificar
	rev(b, 5, good, true, ago(120*day))      // muy vieja
	rev(b, 5, "   corto   ", true, ago(day)) // comentario corto

	c := mk("C: sin reseñas")
	d := mk("D: comentario largo")
	rev(d, 5, strings.Repeat("excelente servicio ", 20), true, ago(day))
	e := mk("E: reseña del futuro no cuenta")
	rev(e, 5, good, true, now.Add(48*time.Hour))

	svc := NewReviewSnippetService(db)
	got, err := svc.Latest([]uuid.UUID{a, b, c, d, e}, now)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := got[a]; !ok || s.Rating != 2 || !strings.HasPrefix(s.Comment, "Tardaron") || s.AuthorName != "Cliente" {
		t.Fatalf("A debe elegir la verificada más reciente con comentario, sin filtrar por nota: %+v", s)
	}
	if _, ok := got[b]; ok {
		t.Fatalf("B no tiene ninguna que sirva: %+v", got[b])
	}
	if _, ok := got[c]; ok {
		t.Fatal("C no tiene reseñas")
	}
	if s := got[d]; len([]rune(s.Comment)) > SnippetMaxChars+1 || !strings.HasSuffix(s.Comment, "…") {
		t.Fatalf("D se recorta: %q", s.Comment)
	}
	if _, ok := got[e]; ok {
		t.Fatal("una reseña con fecha futura no debe contar")
	}
	if m, err := svc.Latest(nil, now); err != nil || len(m) != 0 {
		t.Fatalf("sin ids: %v %v", m, err)
	}
}
