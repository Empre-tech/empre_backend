package services

import "testing"

// Sin negocio solo se redacta la descripción del negocio que se está creando; lo demás va desde un negocio (Pro).
func TestWritingNeedsBusiness(t *testing.T) {
	if writingNeedsBusiness("business_description") {
		t.Fatal("la descripción al crear el negocio va sin negocio")
	}
	for _, k := range []string{"offer_title", "post_caption", "otra"} {
		if !writingNeedsBusiness(k) {
			t.Fatalf("%s debe pedirse desde un negocio", k)
		}
	}
}
