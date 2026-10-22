package services

import (
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

const anon = "inst-1234567890abcdef"

func TestNormalizeQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Barbería   La  22 ", "barbería la 22"},
		{"", ""},
		{"   ", ""},
		{"juan@correo.com", omittedQuery},
		{"llamar 300 123 4567", omittedQuery},
		{"3001234567", omittedQuery},
		{"300-123-4567", omittedQuery},
		{"calle 22 con 5", "calle 22 con 5"}, // cifras cortas no son un teléfono
		{strings.Repeat("a", 100), strings.Repeat("a", maxQueryLen)},
	}
	for _, c := range cases {
		if got := NormalizeQuery(c.in); got != c.want {
			t.Errorf("NormalizeQuery(%q) = %q; quería %q", c.in, got, c.want)
		}
	}
}

func TestValidateEvent(t *testing.T) {
	eid := uuid.NewString()
	ok := func(name string, in EventInput) models.Event {
		t.Helper()
		ev, err := ValidateEvent(anon, in)
		if err != nil {
			t.Fatalf("%s: no debía rechazarse: %v", name, err)
		}
		return ev
	}
	bad := func(name, anonID string, in EventInput) {
		t.Helper()
		if _, err := ValidateEvent(anonID, in); err == nil {
			t.Errorf("%s: debía rechazarse", name)
		}
	}

	ev := ok("búsqueda", EventInput{Name: "search_submitted", Props: map[string]any{"query": "  Barbería ", "results": float64(7), "scope": "near", "intruso": "x"}})
	if ev.Props["query"] != "barbería" || ev.Props["results"] != float64(7) || ev.Props["scope"] != "near" {
		t.Errorf("props de búsqueda: %+v", ev.Props)
	}
	if _, has := ev.Props["intruso"]; has {
		t.Error("un dato no permitido debe descartarse")
	}
	if ev.EntityID != nil {
		t.Error("la búsqueda no lleva negocio")
	}

	ev = ok("perfil", EventInput{Name: "profile_opened", EntityID: eid, Props: map[string]any{"origin": "map"}})
	if ev.EntityID == nil || ev.EntityID.String() != eid || ev.Props["origin"] != "map" {
		t.Errorf("perfil: %+v", ev)
	}
	ev = ok("seguir", EventInput{Name: "follow_toggled", EntityID: eid, Props: map[string]any{"following": true}})
	if ev.Props["following"] != true {
		t.Errorf("seguir: %+v", ev.Props)
	}
	ev = ok("contacto sin datos", EventInput{Name: "contact_started", EntityID: eid})
	if len(ev.Props) != 0 {
		t.Errorf("contacto no lleva datos: %+v", ev.Props)
	}
	ev = ok("texto largo recortado", EventInput{Name: "profile_opened", EntityID: eid, Props: map[string]any{"origin": strings.Repeat("x", 200)}})
	if len(ev.Props["origin"].(string)) != maxPropString {
		t.Errorf("el texto debe recortarse a %d", maxPropString)
	}

	bad("evento desconocido", anon, EventInput{Name: "robar_datos"})
	bad("sin negocio", anon, EventInput{Name: "profile_opened"})
	bad("negocio inválido", anon, EventInput{Name: "profile_opened", EntityID: "no-es-uuid"})
	bad("anon_id corto", "abc", EventInput{Name: "search_submitted"})
	bad("anon_id con caracteres raros", "inst-1234567890abc<script>", EventInput{Name: "search_submitted"})
	bad("anon_id vacío", "", EventInput{Name: "search_submitted"})
	bad("tipo equivocado (texto)", anon, EventInput{Name: "search_submitted", Props: map[string]any{"query": 5.0}})
	bad("tipo equivocado (número)", anon, EventInput{Name: "search_submitted", Props: map[string]any{"results": "siete"}})
	bad("número negativo", anon, EventInput{Name: "search_submitted", Props: map[string]any{"results": float64(-1)}})
	bad("número enorme", anon, EventInput{Name: "search_submitted", Props: map[string]any{"results": float64(1e9)}})
	bad("tipo equivocado (bool)", anon, EventInput{Name: "follow_toggled", EntityID: eid, Props: map[string]any{"following": "sí"}})

	// La consulta con un correo no se guarda tal cual.
	ev = ok("privacidad", EventInput{Name: "search_no_results", Props: map[string]any{"query": "ana@correo.com"}})
	if ev.Props["query"] != omittedQuery {
		t.Errorf("la consulta con correo debe omitirse: %+v", ev.Props)
	}

	// Cada nombre permitido tiene su especificación y ninguno pide datos personales.
	for name, spec := range EventSpecs {
		for key := range spec.Props {
			if key == "email" || key == "phone" || key == "name" || key == "location" {
				t.Errorf("%s no debe admitir %s", name, key)
			}
		}
	}
}

func TestEvents_FromDatabase(t *testing.T) {
	db := openTestDB(t, false)
	svc := NewEventService(db)
	eid := uuid.NewString()
	now := time.Now()

	res, err := svc.Ingest(anon, []EventInput{
		{Name: "search_submitted", Props: map[string]any{"query": "Barbería", "results": float64(3)}},
		{Name: "search_submitted", Props: map[string]any{"query": "tienda", "results": float64(0)}},
		{Name: "search_no_results", Props: map[string]any{"query": "tienda"}},
		{Name: "profile_opened", EntityID: eid, Props: map[string]any{"origin": "search"}},
		{Name: "inventado"},      // se rechaza
		{Name: "profile_opened"}, // sin negocio: se rechaza
	}, now)
	if err != nil || res.Accepted != 4 || res.Rejected != 2 {
		t.Fatalf("lote mixto: %v %+v", err, res)
	}

	// Otra instalación.
	if _, err := svc.Ingest("otra-instalacion-123456", []EventInput{{Name: "search_submitted"}}, now); err != nil {
		t.Fatal(err)
	}
	// Uno viejo.
	if _, err := svc.Ingest(anon, []EventInput{{Name: "search_submitted"}}, now.AddDate(0, 0, -40)); err != nil {
		t.Fatal(err)
	}

	// La hora la pone el servidor y las props quedan como se validaron.
	var saved models.Event
	if err := db.Where("name = ? AND entity_id IS NOT NULL", "profile_opened").First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if saved.AnonID != anon || saved.Props["origin"] != "search" || saved.CreatedAt.Before(now.Add(-time.Minute)) {
		t.Errorf("evento guardado: %+v", saved)
	}

	sum, err := svc.Summary(now.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]EventCount{}
	for _, r := range sum {
		got[r.Name] = r
	}
	if got["search_submitted"].Events != 3 || got["search_submitted"].Users != 2 {
		t.Errorf("search_submitted: %+v (3 eventos de 2 instalaciones, sin el de hace 40 días)", got["search_submitted"])
	}
	if got["search_no_results"].Events != 1 || got["profile_opened"].Events != 1 {
		t.Errorf("conteos: %+v", got)
	}
	if sum[0].Name != "search_submitted" {
		t.Errorf("orden por cantidad: %+v", sum)
	}

	// Retención: se borra solo lo anterior al corte.
	n, err := svc.PurgeOlderThan(now.AddDate(0, 0, -30))
	if err != nil || n != 1 {
		t.Fatalf("purga: %v n=%d", err, n)
	}
	var left int64
	db.Model(&models.Event{}).Count(&left)
	if left != 5 {
		t.Errorf("deben quedar 5 eventos: %d", left)
	}

	// Un lote sin eventos válidos no escribe nada y no falla.
	res, err = svc.Ingest(anon, []EventInput{{Name: "inventado"}}, now)
	if err != nil || res.Accepted != 0 || res.Rejected != 1 {
		t.Errorf("lote inválido: %v %+v", err, res)
	}
}
