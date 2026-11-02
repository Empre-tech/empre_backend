package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
)

func TestAskCleanTermsAndFilters(t *testing.T) {
	cases := map[string]string{
		"¿Dónde almuerzo barato cerca?":  "almuerzo",
		"quiero cortarme el pelo":        "pelo",
		"una droguería abierta ahorita":  "droguería",
		"almorzar":                       "almuerzo",
		"arreglo de motos con domicilio": "arreglo motos",
	}
	for in, want := range cases {
		if got := cleanTerms(in); got != want {
			t.Errorf("cleanTerms(%q) = %q, quería %q", in, got, want)
		}
	}
	f := keywordFilters("algo barato con domicilio abierto ahora y en oferta")
	if !f.Cheapest || !f.Delivery || !f.OpenNow || !f.WithOffer {
		t.Fatalf("filtros: %+v", f)
	}
	if pesos(12000) != "$12.000" || pesos(1500000) != "$1.500.000" || pesos(900) != "$900" {
		t.Fatal("pesos")
	}
}

// Sin IA: con las palabras de la pregunta encuentra el negocio por lo que vende y arma el texto con sus datos.
func TestAskFindsByWhatTheySell(t *testing.T) {
	db, owner, _, ent, _ := sectionsFixture(t)
	db.Model(&ent).Updates(map[string]any{"latitude": 10.4236, "longitude": -75.5478})
	cat := NewCatalogService(db)
	p := 12000
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Almuerzo ejecutivo", Kind: "product", Price: &p}, time.Now()); err != nil {
		t.Fatal(err)
	}
	q := 18000
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Almuerzo especial", Kind: "product", Price: &q}, time.Now()); err != nil {
		t.Fatal(err)
	}
	search := NewSearchService(repository.NewSearchRepository(db), repository.NewOfferRepository(db))
	ask := NewAskService(db, search, nil)
	// La búsqueda usa unaccent (en producción la crea la migración).
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS unaccent").Error; err != nil {
		t.Skip("sin la extensión unaccent:", err)
	}
	res, err := ask.Ask(AskInput{Question: "¿Dónde almuerzo barato cerca?", Lat: 10.4240, Lng: -75.5480})
	if err != nil {
		t.Fatal(err)
	}
	if res.UsedAI || res.Terms != "almuerzo" || !res.Filters.Cheapest {
		t.Fatalf("intención: %+v", res)
	}
	if len(res.Businesses) != 1 || res.Businesses[0].ID != ent.ID || res.Businesses[0].Item == nil || res.Businesses[0].Item.Name != "Almuerzo ejecutivo" {
		t.Fatalf("negocios: %+v", res.Businesses)
	}
	if len(res.Items) != 2 || res.Items[0].Name != "Almuerzo ejecutivo" {
		t.Fatalf("productos: %+v", res.Items)
	}
	if res.Reply == "" || res.Businesses[0].Minutes < 1 {
		t.Fatalf("respuesta: %q", res.Reply)
	}
	// Muy lejos: no aparece.
	far, _ := ask.Ask(AskInput{Question: "almuerzo", Lat: 4.6, Lng: -74.08})
	if far.Businesses == nil || far.Items == nil {
		t.Fatal("sin resultados debe devolver listas vacías, no null")
	}
	if len(far.Businesses) != 0 || far.Suggestions[0] != "Ver categorías" {
		t.Fatalf("lejos: %+v", far)
	}
	// Con «a domicilio» y el negocio sin domicilio: no aparece.
	d, _ := ask.Ask(AskInput{Question: "almuerzo", Lat: 10.4240, Lng: -75.5480, Filters: AskFilters{Delivery: true}})
	if len(d.Businesses) != 0 {
		t.Fatalf("domicilio: %+v", d.Businesses)
	}
	db.Model(&models.Entity{}).Where("id = ?", ent.ID).Update("service_mode", models.ServiceModeBoth)
	d, _ = ask.Ask(AskInput{Question: "almuerzo", Lat: 10.4240, Lng: -75.5480, Filters: AskFilters{Delivery: true}})
	if len(d.Businesses) != 1 || !d.Businesses[0].Delivery {
		t.Fatalf("domicilio sí: %+v", d.Businesses)
	}
}
