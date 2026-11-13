package services

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
)

// Atributos de una variante: lo que la distingue de sus hermanas (talla, color, sabor, tamaño…), cada uno con su
// valor. Con ellos el catálogo filtra («talla 40», «vinotinto») y la búsqueda encuentra el producto por lo que tiene.
const (
	MaxItemAttrs      = 4
	maxAttrNameRunes  = 20
	maxAttrValueRunes = 24
)

// CleanItemAttrs limpia y valida los atributos: nombre y valor sin espacios de más, el nombre con mayúscula inicial,
// sin repetir nombres (sin importar tildes ni mayúsculas) y hasta MaxItemAttrs. Un atributo vacío se descarta.
func CleanItemAttrs(in []models.ItemAttr) ([]models.ItemAttr, error) {
	out := make([]models.ItemAttr, 0, len(in))
	seen := map[string]bool{}
	for _, a := range in {
		name := strings.Join(strings.Fields(a.Name), " ")
		value := strings.Join(strings.Fields(a.Value), " ")
		if name == "" && value == "" {
			continue
		}
		if name == "" || value == "" {
			return nil, invalidService("cada atributo lleva nombre y valor, por ejemplo Talla: 40")
		}
		if utf8.RuneCountInString(name) > maxAttrNameRunes || utf8.RuneCountInString(value) > maxAttrValueRunes {
			return nil, invalidService("el nombre de un atributo va hasta 20 letras y su valor hasta 24")
		}
		key := ledger.Normalize(name)
		if seen[key] {
			return nil, invalidService("un atributo se repite: " + name)
		}
		seen[key] = true
		out = append(out, models.ItemAttr{Name: capitalize(name), Value: value})
	}
	if len(out) > MaxItemAttrs {
		return nil, invalidService("una variante lleva hasta 4 atributos")
	}
	return out, nil
}

// AttrLabel arma la parte corta del nombre de una variante con los valores, en orden: «40 · Vinotinto».
func AttrLabel(attrs []models.ItemAttr) string {
	vals := make([]string, 0, len(attrs))
	for _, a := range attrs {
		vals = append(vals, a.Value)
	}
	return strings.Join(vals, VariantSep)
}

// AttrOption es un atributo con todos los valores que tienen las variantes de un producto: lo que el catálogo
// ofrece para filtrar. Available dice, por valor, si queda alguna variante con ese valor sin agotar.
type AttrOption struct {
	Name      string          `json:"name"`
	Values    []string        `json:"values"`
	Available map[string]bool `json:"available"`
}

// VariantOptions junta los atributos de las variantes de un producto, en el orden en que aparecen. Un nombre que
// solo cambia en tildes o mayúsculas cuenta como el mismo.
func VariantOptions(variants []models.ServiceItem) []AttrOption {
	var out []AttrOption
	idx := map[string]int{}
	for _, v := range variants {
		for _, a := range v.Attrs {
			key := ledger.Normalize(a.Name)
			i, ok := idx[key]
			if !ok {
				i = len(out)
				idx[key] = i
				out = append(out, AttrOption{Name: a.Name, Available: map[string]bool{}})
			}
			o := &out[i]
			if _, had := o.Available[a.Value]; !had {
				o.Values = append(o.Values, a.Value)
				o.Available[a.Value] = false
			}
			if !v.SoldOut {
				o.Available[a.Value] = true
			}
		}
	}
	return out
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
