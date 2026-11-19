package services

import (
	"encoding/base64"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"empre_backend/internal/ledger"
)

// Inventario por páginas (2026-11-16, tablero «Inventario»): la pantalla del dueño ya no baja todo el catálogo de una
// vez. Pide una página de una pestaña (productos, servicios o insumos) con su búsqueda, su filtro y su orden, y los
// conteos de las pestañas y del filtro aparte. Todo sale de la misma vista de inventario que ya calcula los estados
// (ledger.Status), así el servidor nunca dice algo distinto de la pantalla. Filtrar, ordenar, plegar las variantes,
// paginar y contar son funciones puras: se prueban sin base de datos.

const (
	InventoryPageDefault = 50
	InventoryPageMax     = 100
	inventoryQueryMax    = 80
)

// Pestañas del inventario.
const (
	TabProduct = "product"
	TabService = "service"
	TabSupply  = "supply"
)

// Filtros de estado y órdenes.
const (
	StateLow     = "low"
	StateOut     = "out"
	StateNoPhoto = "nophoto"
	SortAZ       = "az"
	SortStock    = "stock"
	SortSales    = "sales"
)

var ErrInventoryQuery = errors.New("consulta de inventario inválida")

// InventoryPageQuery es lo que pide la pantalla: una pestaña, qué buscar, una sección, un estado, un orden y desde dónde.
type InventoryPageQuery struct {
	Tab     string
	Q       string
	Section string
	State   string
	Sort    string
	Offset  int
	Limit   int
}

// InventoryPage es una página: las filas de arriba (productos, servicios o insumos), las variantes de esos productos
// para mostrarlas plegadas, cuántas filas hay con ese filtro y el cursor de la siguiente página (vacío si no hay más).
type InventoryPage struct {
	Items      []InventoryItem `json:"items"`
	Variants   []InventoryItem `json:"variants"`
	Total      int             `json:"total"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// SectionCount es una sección de una pestaña con cuántas filas tiene y cuántas se acaban o se acabaron (el punto rojo de
// la fila de secciones: ir directo a donde algo falta).
type SectionCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Low   int    `json:"low"`
	Out   int    `json:"out"`
	// CountedAt: la última vez que se contó esta sección (vacío si nunca).
	CountedAt *time.Time `json:"counted_at,omitempty"`
}

// TabCounts son los números de una pestaña: el total, los de cada estado y sus secciones.
type TabCounts struct {
	Total    int            `json:"total"`
	Low      int            `json:"low"`
	Out      int            `json:"out"`
	NoPhoto  int            `json:"no_photo"`
	Sections []SectionCount `json:"sections"`
}

// InventoryCounts son los números de las tres pestañas, para las pestañas y el filtro sin bajar las filas.
type InventoryCounts struct {
	Products TabCounts `json:"products"`
	Services TabCounts `json:"services"`
	Supplies TabCounts `json:"supplies"`
}

// EncodePageCursor / DecodePageCursor: el cursor es opaco para la app (hoy, un desplazamiento).
func EncodePageCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func DecodePageCursor(c string) (int, error) {
	if c == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || !strings.HasPrefix(string(raw), "o:") {
		return 0, ErrInventoryQuery
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(raw), "o:"))
	if err != nil || n < 0 {
		return 0, ErrInventoryQuery
	}
	return n, nil
}

// ParseInventoryPageQuery valida lo que llega por la URL.
func ParseInventoryPageQuery(tab, q, section, state, sortBy, cursor, limit string) (InventoryPageQuery, error) {
	out := InventoryPageQuery{Tab: tab, Q: strings.TrimSpace(q), Section: strings.TrimSpace(section), State: state, Sort: sortBy, Limit: InventoryPageDefault}
	switch tab {
	case TabProduct, TabService, TabSupply:
	default:
		return out, ErrInventoryQuery
	}
	switch state {
	case "", StateLow, StateOut, StateNoPhoto:
	default:
		return out, ErrInventoryQuery
	}
	switch sortBy {
	case "", SortAZ, SortStock, SortSales:
	default:
		return out, ErrInventoryQuery
	}
	if len([]rune(out.Q)) > inventoryQueryMax {
		out.Q = string([]rune(out.Q)[:inventoryQueryMax])
	}
	if limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 {
			return out, ErrInventoryQuery
		}
		if n > InventoryPageMax {
			n = InventoryPageMax
		}
		out.Limit = n
	}
	off, err := DecodePageCursor(cursor)
	if err != nil {
		return out, err
	}
	out.Offset = off
	return out, nil
}

// fold: minúsculas y sin tildes, para que «cafe» encuentre «Café».
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if r >= 0x300 && r <= 0x36f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// tabOf: en qué pestaña va una fila (como la app: un producto que también se usa como insumo sale en las dos).
func inTab(it InventoryItem, tab string) bool {
	switch tab {
	case TabService:
		return it.Kind == "product" && it.IsService
	case TabProduct:
		return it.Kind == "product" && !it.IsService
	case TabSupply:
		return it.Kind == "supply" || it.AlsoSupply
	}
	return false
}

func sectionOf(it InventoryItem, tab string) string {
	if tab == TabSupply && it.Group != "" {
		return it.Group
	}
	return it.Section
}

// splitVariants separa las filas de arriba de sus variantes (por producto).
func splitVariants(rows []InventoryItem) ([]InventoryItem, map[uuid.UUID][]InventoryItem) {
	top := make([]InventoryItem, 0, len(rows))
	kids := map[uuid.UUID][]InventoryItem{}
	for _, it := range rows {
		if it.ParentID != nil {
			kids[*it.ParentID] = append(kids[*it.ParentID], it)
			continue
		}
		top = append(top, it)
	}
	return top, kids
}

// hasStatus: la fila (o alguna de sus variantes) está en ese estado.
func hasStatus(it InventoryItem, kids []InventoryItem, status string) bool {
	if it.Status == status {
		return true
	}
	for _, k := range kids {
		if k.Status == status {
			return true
		}
	}
	return false
}

func matchesQuery(it InventoryItem, kids []InventoryItem, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(fold(it.Name), q) || strings.Contains(fold(it.Section), q) || strings.Contains(fold(it.Group), q) {
		return true
	}
	for _, k := range kids {
		if strings.Contains(fold(k.Name), q) || strings.Contains(fold(k.VariantLabel), q) {
			return true
		}
	}
	return false
}

// stockRank: para «menos existencias» primero lo agotado, luego lo que se acaba, lo que no se ha contado y lo demás.
func stockRank(it InventoryItem, kids []InventoryItem) int {
	switch {
	case hasStatus(it, kids, ledger.StatusOut):
		return 0
	case hasStatus(it, kids, ledger.StatusLow):
		return 1
	case it.Status == ledger.StatusUnknown:
		return 2
	case it.Status == ledger.StatusOK:
		return 3
	}
	return 4
}

// PageInventory filtra, ordena y corta una página de una pestaña. Las variantes de los productos de la página van
// aparte (plegadas en la app); la búsqueda y los estados miran también las variantes.
func PageInventory(rows []InventoryItem, q InventoryPageQuery) InventoryPage {
	top, kids := splitVariants(rows)
	query := fold(q.Q)
	sec := fold(q.Section)
	matched := make([]InventoryItem, 0, len(top))
	for _, it := range top {
		if !inTab(it, q.Tab) {
			continue
		}
		k := kids[it.Ref]
		if sec != "" && fold(sectionOf(it, q.Tab)) != sec {
			continue
		}
		if !matchesQuery(it, k, query) {
			continue
		}
		switch q.State {
		case StateLow:
			if !hasStatus(it, k, ledger.StatusLow) {
				continue
			}
		case StateOut:
			if !hasStatus(it, k, ledger.StatusOut) {
				continue
			}
		case StateNoPhoto:
			if it.Photo != "" {
				continue
			}
		}
		matched = append(matched, it)
	}
	switch q.Sort {
	case SortAZ:
		sort.SliceStable(matched, func(i, j int) bool { return fold(matched[i].Name) < fold(matched[j].Name) })
	case SortStock:
		sort.SliceStable(matched, func(i, j int) bool {
			return stockRank(matched[i], kids[matched[i].Ref]) < stockRank(matched[j], kids[matched[j].Ref])
		})
	case SortSales:
		sort.SliceStable(matched, func(i, j int) bool { return matched[i].Sold28 > matched[j].Sold28 })
	}
	page := InventoryPage{Items: []InventoryItem{}, Variants: []InventoryItem{}, Total: len(matched)}
	limit := q.Limit
	if limit <= 0 {
		limit = InventoryPageDefault
	}
	if q.Offset >= len(matched) {
		return page
	}
	end := q.Offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	page.Items = append(page.Items, matched[q.Offset:end]...)
	for _, it := range page.Items {
		page.Variants = append(page.Variants, kids[it.Ref]...)
	}
	if end < len(matched) {
		page.NextCursor = EncodePageCursor(end)
	}
	return page
}

// CountInventory cuenta las tres pestañas: el total, los de cada estado (mirando las variantes) y sus secciones, en el
// orden en que aparecen.
func CountInventory(rows []InventoryItem) InventoryCounts {
	top, kids := splitVariants(rows)
	count := func(tab string) TabCounts {
		tc := TabCounts{Sections: []SectionCount{}}
		idx := map[string]int{}
		for _, it := range top {
			if !inTab(it, tab) {
				continue
			}
			k := kids[it.Ref]
			tc.Total++
			out, low := hasStatus(it, k, ledger.StatusOut), false
			if out {
				tc.Out++
			} else if low = hasStatus(it, k, ledger.StatusLow); low {
				tc.Low++
			}
			if it.Photo == "" && tab != TabSupply {
				tc.NoPhoto++
			}
			if name := sectionOf(it, tab); name != "" {
				i, ok := idx[name]
				if !ok {
					i = len(tc.Sections)
					idx[name] = i
					tc.Sections = append(tc.Sections, SectionCount{Name: name})
				}
				tc.Sections[i].Count++
				if out {
					tc.Sections[i].Out++
				} else if low {
					tc.Sections[i].Low++
				}
			}
		}
		return tc
	}
	return InventoryCounts{Products: count(TabProduct), Services: count(TabService), Supplies: count(TabSupply)}
}

// InventoryPage: una página de una pestaña del inventario (solo el dueño).
func (s *LedgerService) InventoryPage(ownerID, entityID uuid.UUID, q InventoryPageQuery) (InventoryPage, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryPage{}, err
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return InventoryPage{}, err
	}
	return PageInventory(ov.Items, q), nil
}

// InventoryCountsFor: los números de las pestañas y del filtro (solo el dueño).
func (s *LedgerService) InventoryCountsFor(ownerID, entityID uuid.UUID) (InventoryCounts, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return InventoryCounts{}, err
	}
	ov, err := s.inventory(entityID)
	if err != nil {
		return InventoryCounts{}, err
	}
	c := CountInventory(ov.Items)
	last, err := s.lastCounts(entityID)
	if err != nil {
		return InventoryCounts{}, err
	}
	AttachCountDates(&c, last)
	return c, nil
}
