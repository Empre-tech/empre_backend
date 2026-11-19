package services

import (
	"reflect"
	"testing"

	"empre_backend/internal/models"
)

func TestCleanItemAttrs(t *testing.T) {
	got, err := CleanItemAttrs([]models.ItemAttr{{Name: "  talla ", Value: " 40 "}, {Name: "color", Value: "Vino  tinto"}, {}})
	if err != nil {
		t.Fatal(err)
	}
	want := []models.ItemAttr{{Name: "Talla", Value: "40"}, {Name: "Color", Value: "Vino tinto"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("limpios %v, quería %v", got, want)
	}
	bad := map[string][]models.ItemAttr{
		"sin valor":          {{Name: "Talla"}},
		"sin nombre":         {{Value: "40"}},
		"repetido":           {{Name: "Talla", Value: "40"}, {Name: "talla", Value: "41"}},
		"repetido con tilde": {{Name: "Tamaño", Value: "S"}, {Name: "tamano", Value: "M"}},
		"demasiados":         {{Name: "A", Value: "1"}, {Name: "B", Value: "2"}, {Name: "C", Value: "3"}, {Name: "D", Value: "4"}, {Name: "E", Value: "5"}},
		"nombre largo":       {{Name: "Un nombre de atributo demasiado largo", Value: "1"}},
	}
	for name, in := range bad {
		if _, err := CleanItemAttrs(in); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
}

func TestAttrLabel(t *testing.T) {
	if got := AttrLabel([]models.ItemAttr{{Name: "Talla", Value: "40"}, {Name: "Color", Value: "Vinotinto"}}); got != "40"+VariantSep+"Vinotinto" {
		t.Errorf("etiqueta %q", got)
	}
	if got := AttrLabel(nil); got != "" {
		t.Errorf("sin atributos, etiqueta vacía: %q", got)
	}
}

func TestVariantOptions(t *testing.T) {
	v := func(talla, color string, agotado bool) models.ServiceItem {
		return models.ServiceItem{SoldOut: agotado, Attrs: []models.ItemAttr{{Name: "Talla", Value: talla}, {Name: "Color", Value: color}}}
	}
	opts := VariantOptions([]models.ServiceItem{v("40", "Vinotinto", false), v("41", "Vinotinto", true), v("40", "Gris", false), {Attrs: []models.ItemAttr{{Name: "talla", Value: "42"}}}})
	if len(opts) != 2 || opts[0].Name != "Talla" || opts[1].Name != "Color" {
		t.Fatalf("atributos %+v", opts)
	}
	if !reflect.DeepEqual(opts[0].Values, []string{"40", "41", "42"}) {
		t.Errorf("tallas %v", opts[0].Values)
	}
	if opts[0].Available["41"] || !opts[0].Available["40"] {
		t.Errorf("disponibilidad %v: la 41 está agotada y la 40 no", opts[0].Available)
	}
	if !reflect.DeepEqual(opts[1].Values, []string{"Vinotinto", "Gris"}) {
		t.Errorf("colores %v", opts[1].Values)
	}
}
