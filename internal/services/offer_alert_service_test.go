package services

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAlertDue(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	if !AlertDue(nil, now) {
		t.Fatal("un aviso que nunca sonó debe sonar")
	}
	recent := now.Add(-2 * time.Hour)
	if AlertDue(&recent, now) {
		t.Fatal("dos horas después no vuelve a sonar")
	}
	old := now.Add(-AlertCooldown)
	if !AlertDue(&old, now) {
		t.Fatal("pasado el descanso vuelve a sonar")
	}
}

func TestAlertMatches(t *testing.T) {
	comida, bebidas, ropa := uuid.New(), uuid.New(), uuid.New()
	offer := map[uuid.UUID]bool{bebidas: true, comida: true} // la oferta es de bebidas y su madre es comida
	if !AlertMatches(nil, offer) {
		t.Fatal("sin categoría cubre todas")
	}
	if !AlertMatches(&comida, offer) {
		t.Fatal("la categoría madre cubre a sus hijas")
	}
	if AlertMatches(&ropa, offer) {
		t.Fatal("otra categoría no suena")
	}
}

func TestAlertText(t *testing.T) {
	title, body := AlertText("Taganga", "Casa Canoas", "2x1 en jugos")
	if title != "Nueva oferta en Taganga" || body != "Casa Canoas: 2x1 en jugos" {
		t.Fatalf("texto inesperado: %q / %q", title, body)
	}
}
