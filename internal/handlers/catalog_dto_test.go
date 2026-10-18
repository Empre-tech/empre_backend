package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"
)

// La respuesta pública del catálogo lleva la duración y si se puede reservar (antes se perdían al armar el DTO y
// «Pregúntale» no ofrecía reservar).
func TestServiceDTOCarriesSchedule(t *testing.T) {
	d := 30
	raw, err := json.Marshal(toServiceDTO(models.ServiceItem{Name: "Corte", DurationMin: &d, Bookable: true}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"duration_min":30`) || !strings.Contains(string(raw), `"bookable":true`) {
		t.Fatalf("faltan duración o reservable: %s", raw)
	}
	raw, _ = json.Marshal(toServiceDTO(models.ServiceItem{Name: "Barba"}, time.Now()))
	if !strings.Contains(string(raw), `"duration_min":null`) || !strings.Contains(string(raw), `"bookable":false`) {
		t.Fatalf("sin duración debe ir null y no reservable: %s", raw)
	}
}
