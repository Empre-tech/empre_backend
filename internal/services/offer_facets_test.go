package services

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestOfferBenefit(t *testing.T) {
	zero, price := 0, 25000
	cases := []struct {
		name, title, price string
		special            *int
		items              int
		want               string
	}{
		{"2x1 en el precio", "Jugo natural", "2x1", nil, 1, BenefitMulti},
		{"3 x 2 en el título", "Empanadas 3 x 2", "", nil, 1, BenefitMulti},
		{"gratis", "Postre gratis con tu almuerzo", "", nil, 1, BenefitFree},
		{"precio especial en cero", "Diagnóstico", "", &zero, 1, BenefitFree},
		{"porcentaje", "Corte", "15%", nil, 1, BenefitPercent},
		{"descuento en palabras", "Descuento en tenis", "", nil, 1, BenefitPercent},
		{"varios servicios", "Corte y barba", "$30.000", &price, 2, BenefitCombo},
		{"precio especial", "Corte clásico", "", &price, 1, BenefitPrice},
		{"precio escrito", "Almuerzo del día", "$14.000", nil, 0, BenefitPrice},
		{"nada claro", "Ven hoy", "", nil, 0, BenefitOther},
	}
	for _, c := range cases {
		if got := OfferBenefit(c.title, c.price, c.special, c.items); got != c.want {
			t.Errorf("%s: %q, quería %q", c.name, got, c.want)
		}
	}
}

func TestOfferTags(t *testing.T) {
	got := OfferTags("¡Oferta! Corte + barba 2x1 solo hoy", []string{"Corte clásico", "Arreglo de barba"})
	want := []string{"corte", "barba", "clasico", "arreglo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("etiquetas %v, quería %v", got, want)
	}
	if n := len(OfferTags("uno dos tres cuatro cinco seis siete ocho nueve diez once", nil)); n != OfferTagsMax {
		t.Errorf("tope: %d etiquetas, quería %d", n, OfferTagsMax)
	}
	if got := OfferTags("2x1 hoy", nil); len(got) != 0 {
		t.Errorf("sin palabras propias no hay etiquetas: %v", got)
	}
}

func TestOfferAudience(t *testing.T) {
	got := OfferAudience([][]string{{"kids"}, nil, {"women", "kids"}})
	if want := []string{"women", "kids"}; !reflect.DeepEqual(got, want) {
		t.Errorf("público %v, quería %v", got, want)
	}
	if got := OfferAudience(nil); len(got) != 0 {
		t.Errorf("sin datos no hay público: %v", got)
	}
}

func TestPickOfferCategory(t *testing.T) {
	barber, beauty, food := uuid.New(), uuid.New(), uuid.New()
	cases := []struct {
		name    string
		perItem [][]uuid.UUID
		want    uuid.UUID
	}{
		{"sin servicios, la del negocio", nil, barber},
		{"la sección no dice nada", [][]uuid.UUID{nil}, barber},
		{"la del negocio está entre las posibles", [][]uuid.UUID{{beauty, barber}}, barber},
		{"todo apunta a otra", [][]uuid.UUID{{food}, {food}}, food},
		{"empate entre otras dos", [][]uuid.UUID{{food, beauty}}, barber},
		{"un servicio sin dato", [][]uuid.UUID{{food}, nil}, barber},
		{"no todos coinciden", [][]uuid.UUID{{food}, {beauty}}, barber},
	}
	for _, c := range cases {
		if got := PickOfferCategory(barber, c.perItem); got != c.want {
			t.Errorf("%s: categoría equivocada", c.name)
		}
	}
}
