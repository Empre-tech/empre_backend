package services

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"empre_backend/internal/models"
)

func TestAskQuota(t *testing.T) {
	db := testDB(t)
	u := models.User{Name: "Ana", Email: "ana@x.co", PasswordHash: "x"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	s := &AskService{DB: db}
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, bogota)

	for i := 1; i <= AskFreeMonthly; i++ {
		q, err := s.UseQuestion(u.ID, fmt.Sprintf("pregunta %d", i), now)
		if err != nil || q.Used != i || q.Limit != AskFreeMonthly || q.Plan != "free" {
			t.Fatalf("pregunta %d: q=%+v err=%v", i, q, err)
		}
	}
	// La misma pregunta (otra forma de escribirla, o cambiando un filtro) no cuenta otra vez.
	if q, err := s.UseQuestion(u.ID, "  ¿Pregunta 3? ", now); err != nil || q.Used != AskFreeMonthly {
		t.Fatalf("repetida: q=%+v err=%v", q, err)
	}
	// Una nueva con el cupo lleno: no.
	q, err := s.UseQuestion(u.ID, "una más", now)
	if !errors.Is(err, ErrAskQuota) {
		t.Fatalf("esperaba ErrAskQuota, q=%+v err=%v", q, err)
	}
	if q.ResetsAt.Format("2006-01-02") != "2026-12-01" {
		t.Fatalf("se renueva %v", q.ResetsAt)
	}
	// El 1 de diciembre (hora de Colombia) arranca de cero.
	dec := time.Date(2026, 12, 1, 0, 30, 0, 0, bogota)
	if q, err := s.UseQuestion(u.ID, "una más", dec); err != nil || q.Used != 1 {
		t.Fatalf("mes nuevo: q=%+v err=%v", q, err)
	}
	// Con la suscripción: 300.
	if err := s.GrantPlus(u.ID, now.AddDate(0, 1, 0), "manual"); err != nil {
		t.Fatal(err)
	}
	if q, err := s.UseQuestion(u.ID, "con plus", now); err != nil || q.Limit != AskPlusMonthly || q.Plan != "plus" || q.Used != AskFreeMonthly+1 {
		t.Fatalf("plus: q=%+v err=%v", q, err)
	}
	// Vencida: vuelve a 10.
	if q, err := s.Quota(u.ID, now.AddDate(0, 2, 0)); err != nil || q.Plan != "free" {
		t.Fatalf("vencida: q=%+v err=%v", q, err)
	}
}
