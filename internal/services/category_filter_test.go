package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// El filtro de categoría acepta un grupo entero (varias categorías separadas por coma) en el listado por
// rectángulo y en las casillas; un valor que no sirve da cero negocios, no todos ni un error.
func TestCategoryFilter_Groups(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	cats := map[string]models.Category{}
	for _, n := range []string{"Restaurantes", "Cafeterías", "Barberías"} {
		c := models.Category{Name: n}
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		cats[n] = c
	}
	for i, n := range []string{"Restaurantes", "Restaurantes", "Cafeterías", "Barberías", "Barberías", "Barberías"} {
		e := models.Entity{OwnerID: owner, Name: n, CategoryID: cats[n].ID, Latitude: 11.2 + float64(i)*0.0003, Longitude: -74.2}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
	}

	ents := NewEntityService(repository.NewEntityRepository(db), nil)
	cells := NewCellService(db)
	box := func(cat string) (int, int) {
		list, _, err := ents.FindByBBox(11.1, 11.3, -74.3, -74.1, cat, "", "", 100)
		if err != nil {
			t.Fatalf("FindByBBox(%q): %v", cat, err)
		}
		got, _, _, err := cells.Cells(CellQuery{MinLat: 11.1, MaxLat: 11.3, MinLng: -74.3, MaxLng: -74.1, LatDelta: 0.2, CategoryID: cat, Now: time.Now()})
		if err != nil {
			t.Fatalf("Cells(%q): %v", cat, err)
		}
		n := 0
		for _, c := range got {
			n += c.Count
		}
		return len(list), n
	}
	id := func(n string) string { return cats[n].ID.String() }

	cases := []struct {
		name string
		cat  string
		want int
	}{
		{"sin filtro", "", 6},
		{"una categoría", id("Barberías"), 3},
		{"un grupo de dos", id("Restaurantes") + "," + id("Cafeterías"), 3},
		{"grupo con espacios y repetidos", id("Restaurantes") + ", " + id("Restaurantes") + "," + id("Barberías"), 5},
		{"un valor basura entre ids válidos se ignora", "basura," + id("Cafeterías"), 1},
		{"solo basura: cero, sin error", "basura", 0},
		{"categoría que no existe: cero", uuid.NewString(), 0},
	}
	for _, c := range cases {
		list, count := box(c.cat)
		if list != c.want || count != c.want {
			t.Errorf("%s: listado=%d casillas=%d, quería %d", c.name, list, count, c.want)
		}
	}
}
