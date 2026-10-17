package repository

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseCategoryIDs(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	if ids, asked := ParseCategoryIDs(""); asked || ids != nil {
		t.Fatalf("sin parámetro no hay filtro: %v %v", ids, asked)
	}
	if _, asked := ParseCategoryIDs("   "); asked {
		t.Fatal("solo espacios tampoco es un filtro")
	}
	if ids, asked := ParseCategoryIDs(a.String()); !asked || len(ids) != 1 || ids[0] != a {
		t.Fatalf("una categoría: %v", ids)
	}
	if ids, _ := ParseCategoryIDs(a.String() + ", " + b.String() + "," + a.String()); len(ids) != 2 || ids[0] != a || ids[1] != b {
		t.Fatalf("varias, con espacios y repetidas: %v", ids)
	}
	if ids, asked := ParseCategoryIDs("basura," + a.String() + ",,x"); !asked || len(ids) != 1 || ids[0] != a {
		t.Fatalf("se descartan los valores que no son id: %v", ids)
	}
	if ids, asked := ParseCategoryIDs("basura"); !asked || len(ids) != 0 {
		t.Fatalf("pedido pero sin ningún id válido: %v %v", ids, asked)
	}
	many := ""
	for i := 0; i < MaxCategoryFilter+10; i++ {
		many += uuid.NewString() + ","
	}
	if ids, _ := ParseCategoryIDs(many); len(ids) != MaxCategoryFilter {
		t.Fatalf("tope de categorías: %d", len(ids))
	}
}
