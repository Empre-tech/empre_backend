package database

import "testing"

// TestDefaultCatalogIsComplete: cada categoría tiene subcategorías, cada subcategoría tiene su plantilla de secciones
// y ninguna subcategoría se repite (2026-11-26: el catálogo anterior tenía repetidos).
func TestDefaultCatalogIsComplete(t *testing.T) {
	seen := map[string]string{}
	for _, c := range defaultCategories {
		subs, ok := defaultSubcategories[c.Name]
		if !ok || len(subs) == 0 {
			t.Errorf("%s no tiene subcategorías", c.Name)
		}
		for _, s := range subs {
			if prev, dup := seen[s]; dup {
				t.Errorf("«%s» está en %s y en %s", s, prev, c.Name)
			}
			seen[s] = c.Name
			if _, ok := sectionTemplates[s]; !ok {
				t.Errorf("«%s» (%s) no tiene plantilla de secciones", s, c.Name)
			}
		}
	}
	if len(defaultSubcategories) != len(defaultCategories) {
		t.Errorf("hay subcategorías de una categoría que no existe: %d grupos, %d categorías", len(defaultSubcategories), len(defaultCategories))
	}
	for name := range sectionTemplates {
		if _, ok := seen[name]; !ok {
			t.Errorf("la plantilla «%s» no es de ninguna subcategoría", name)
		}
	}
}
