package services

import (
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"empre_backend/internal/database"
	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Secciones del catálogo (lo que vendes e insumos). El catálogo de secciones es global y lo administra el equipo de
// Empre; un negocio arranca con las más comunes de sus subcategorías, puede agregar cualquier otra del catálogo y,
// si no encuentra la suya, pedirla. El dueño nunca crea secciones.

var (
	ErrSectionNotFound = errors.New("sección no encontrada")
)

// maxFallbackSections: cuántas secciones toma un negocio sin subcategoría, de las plantillas de su categoría.
const maxFallbackSections = 8

// SectionView es una sección tal como la ve el negocio: con su posición y cuántos ítems activos tiene.
type SectionView struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	Icon  string    `json:"icon"`
	Count int       `json:"count"`
	// Template: viene de la plantilla de su subcategoría (false = la agregó el negocio, o es «Otros»).
	Template bool `json:"template"`
}

type SectionService struct {
	db *gorm.DB
}

func NewSectionService(db *gorm.DB) *SectionService { return &SectionService{db: db} }

// DB da la conexión del servicio (para consultas pequeñas de los handlers, como AudienceEnabled).
func (s *SectionService) DB() *gorm.DB { return s.db }

// entitySections arma las secciones de un negocio, en orden: las de la plantilla de sus subcategorías (sin repetir),
// las que agregó y, al final, «Otros». Sin conteos.
func entitySections(db *gorm.DB, entityID uuid.UUID, kind string) ([]SectionView, error) {
	var subIDs []uuid.UUID
	if err := db.Table("entity_subcategories").Where("entity_id = ?", entityID).Pluck("subcategory_id", &subIDs).Error; err != nil {
		return nil, err
	}
	type row struct {
		ID            uuid.UUID
		Name          string
		Icon          string
		SubcategoryID uuid.UUID
		Position      int
	}
	var rows []row
	q := db.Table("subcategory_sections ss").
		Select("s.id, s.name, s.icon, ss.subcategory_id, ss.position").
		Joins("JOIN sections s ON s.id = ss.section_id").
		Where("s.kind = ?", kind)
	fallback := len(subIDs) == 0
	if fallback {
		// Sin subcategoría: las plantillas de todas las subcategorías de su categoría, las más repetidas primero.
		q = q.Joins("JOIN subcategories sc ON sc.id = ss.subcategory_id AND sc.deleted_at IS NULL").
			Joins("JOIN entities e ON e.category_id = sc.category_id").
			Where("e.id = ?", entityID)
	} else {
		q = q.Where("ss.subcategory_id IN ?", subIDs)
	}
	if err := q.Order("ss.position ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	var out []SectionView
	seen := map[uuid.UUID]bool{}
	if fallback {
		freq := map[uuid.UUID]int{}
		best := map[uuid.UUID]int{}
		names := map[uuid.UUID]string{}
		icons := map[uuid.UUID]string{}
		for _, r := range rows {
			freq[r.ID]++
			if p, ok := best[r.ID]; !ok || r.Position < p {
				best[r.ID] = r.Position
			}
			names[r.ID] = r.Name
			icons[r.ID] = r.Icon
		}
		ids := make([]uuid.UUID, 0, len(freq))
		for id := range freq {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := ids[i], ids[j]
			if freq[a] != freq[b] {
				return freq[a] > freq[b]
			}
			if best[a] != best[b] {
				return best[a] < best[b]
			}
			return names[a] < names[b]
		})
		for _, id := range ids {
			if len(out) >= maxFallbackSections {
				break
			}
			if names[id] == models.SectionOther {
				continue
			}
			seen[id] = true
			out = append(out, SectionView{ID: id, Name: names[id], Kind: kind, Icon: icons[id], Template: true})
		}
	} else {
		// Varias subcategorías: se intercalan por posición, así la primera de cada una queda arriba.
		for _, r := range rows {
			if seen[r.ID] || r.Name == models.SectionOther {
				continue
			}
			seen[r.ID] = true
			out = append(out, SectionView{ID: r.ID, Name: r.Name, Kind: kind, Icon: r.Icon, Template: true})
		}
	}
	var added []models.Section
	if err := db.Table("entity_sections es").Select("s.*").
		Joins("JOIN sections s ON s.id = es.section_id").
		Where("es.entity_id = ? AND s.kind = ?", entityID, kind).
		Order("es.created_at ASC").Scan(&added).Error; err != nil {
		return nil, err
	}
	for _, s := range added {
		if seen[s.ID] || s.Name == models.SectionOther {
			continue
		}
		seen[s.ID] = true
		out = append(out, SectionView{ID: s.ID, Name: s.Name, Kind: kind, Icon: s.Icon})
	}
	other, err := otherSection(db, kind)
	if err != nil {
		return nil, err
	}
	out = append(out, SectionView{ID: other.ID, Name: other.Name, Kind: kind, Icon: other.Icon})
	return out, nil
}

// otherSection devuelve (y crea si falta) la sección «Otros» del tipo.
func otherSection(db *gorm.DB, kind string) (models.Section, error) {
	var s models.Section
	err := db.Where("kind = ? AND name = ?", kind, models.SectionOther).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s = models.Section{Kind: kind, Name: models.SectionOther, Icon: database.SectionIcon(kind, models.SectionOther)}
		err = db.Where("kind = ? AND name = ?", kind, models.SectionOther).FirstOrCreate(&s).Error
	}
	return s, err
}

// resolveSection valida la sección que eligió el dueño para un ítem: existe y es del tipo correcto. Si no estaba
// entre las del negocio, la agrega (el dueño puede usar cualquier sección del catálogo).
func resolveSection(db *gorm.DB, entityID, sectionID uuid.UUID, kind string) (models.Section, error) {
	var s models.Section
	if err := db.First(&s, "id = ?", sectionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s, ErrSectionNotFound
		}
		return s, err
	}
	if s.Kind != kind {
		return s, ErrSectionNotFound
	}
	mine, err := entitySections(db, entityID, kind)
	if err != nil {
		return s, err
	}
	for _, m := range mine {
		if m.ID == s.ID {
			return s, nil
		}
	}
	return s, db.Where("entity_id = ? AND section_id = ?", entityID, s.ID).
		FirstOrCreate(&models.EntitySection{EntityID: entityID, SectionID: s.ID}).Error
}

// AudienceEnabled: el negocio dice para quién es cada producto (ropa, calzado, deporte), según sus subcategorías.
func AudienceEnabled(db *gorm.DB, entityID uuid.UUID) bool {
	var n int64
	db.Table("entity_subcategories es").Joins("JOIN subcategories s ON s.id = es.subcategory_id").
		Where("es.entity_id = ? AND s.name IN ?", entityID, models.AudienceSubcategories).Count(&n)
	return n > 0
}

// defaultSection: la primera sección del negocio (la más común de su subcategoría). Para los atajos que crean un
// ítem sin preguntar la sección, como agregar un servicio desde la agenda.
func defaultSection(db *gorm.DB, entityID uuid.UUID, kind string) (models.Section, error) {
	mine, err := entitySections(db, entityID, kind)
	if err != nil {
		return models.Section{}, err
	}
	first := mine[0] // siempre está «Otros»
	return models.Section{ID: first.ID, Name: first.Name, Kind: kind}, nil
}

func (s *SectionService) owner(userID, entityID uuid.UUID) error {
	var e models.Entity
	if err := s.db.Select("id", "owner_id").First(&e, "id = ?", entityID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCatalogNotFound
		}
		return err
	}
	if e.OwnerID != userID {
		return ErrCatalogNotOwner
	}
	return nil
}

// ForEntity devuelve las secciones del negocio con cuántos ítems activos tiene cada una (solo el dueño).
func (s *SectionService) ForEntity(userID, entityID uuid.UUID, kind string) ([]SectionView, error) {
	if !models.ValidSectionKind(kind) {
		return nil, &InvalidError{"tipo de sección inválido"}
	}
	if err := s.owner(userID, entityID); err != nil {
		return nil, err
	}
	out, err := entitySections(s.db, entityID, kind)
	if err != nil {
		return nil, err
	}
	counts, err := sectionCounts(s.db, entityID, kind)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Count = counts[out[i].ID]
	}
	return out, nil
}

// PublicSections: las secciones de lo que vende el negocio que tienen al menos un ítem activo, en su orden. Es lo
// que agrupa el perfil público.
func (s *SectionService) PublicSections(entityID uuid.UUID) ([]SectionView, error) {
	all, err := entitySections(s.db, entityID, models.SectionSell)
	if err != nil {
		return nil, err
	}
	counts, err := sectionCounts(s.db, entityID, models.SectionSell)
	if err != nil {
		return nil, err
	}
	out := make([]SectionView, 0, len(all))
	listed := map[uuid.UUID]bool{}
	for _, v := range all {
		listed[v.ID] = true
		if c := counts[v.ID]; c > 0 {
			v.Count = c
			out = append(out, v)
		}
	}
	// Un ítem puede quedar en una sección que ya no está entre las del negocio (cambió de subcategoría): se muestra
	// igual, antes de «Otros».
	var stray []models.Section
	ids := make([]uuid.UUID, 0)
	for id, c := range counts {
		if c > 0 && !listed[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		if err := s.db.Where("id IN ?", ids).Order("name").Find(&stray).Error; err != nil {
			return nil, err
		}
		tail := out
		if n := len(out); n > 0 && out[n-1].Name == models.SectionOther {
			tail = out[:n-1]
		}
		extra := make([]SectionView, 0, len(stray))
		for _, st := range stray {
			extra = append(extra, SectionView{ID: st.ID, Name: st.Name, Kind: st.Kind, Icon: st.Icon, Count: counts[st.ID]})
		}
		merged := append(append(append([]SectionView{}, tail...), extra...), out[len(tail):]...)
		out = merged
	}
	return out, nil
}

// sectionCounts: cuántos ítems activos hay en cada sección (las variantes no cuentan: van con su producto).
func sectionCounts(db *gorm.DB, entityID uuid.UUID, kind string) (map[uuid.UUID]int, error) {
	type row struct {
		SectionID uuid.UUID
		N         int
	}
	var rows []row
	var err error
	if kind == models.SectionSell {
		err = db.Model(&models.ServiceItem{}).Select("section_id, COUNT(*) AS n").
			Where("entity_id = ? AND status = ? AND parent_id IS NULL AND section_id IS NOT NULL", entityID, models.ServiceActive).
			Group("section_id").Scan(&rows).Error
	} else {
		err = db.Model(&models.StockItem{}).Select("section_id, COUNT(*) AS n").
			Where("entity_id = ? AND service_item_id IS NULL AND archived = false AND section_id IS NOT NULL", entityID).
			Group("section_id").Scan(&rows).Error
	}
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		out[r.SectionID] = r.N
	}
	return out, nil
}

// Catalog busca en el catálogo global de secciones de un tipo (para «Más secciones»). q vacío = todas.
func (s *SectionService) Catalog(kind, q string) ([]models.Section, error) {
	if !models.ValidSectionKind(kind) {
		return nil, &InvalidError{"tipo de sección inválido"}
	}
	var all []models.Section
	if err := s.db.Where("kind = ?", kind).Order("name ASC").Find(&all).Error; err != nil {
		return nil, err
	}
	key := ledger.Normalize(q)
	if key == "" {
		return all, nil
	}
	out := make([]models.Section, 0, len(all))
	for _, sec := range all {
		if strings.Contains(ledger.Normalize(sec.Name), key) {
			out = append(out, sec)
		}
	}
	return out, nil
}

// Add agrega una sección del catálogo a las del negocio.
func (s *SectionService) Add(userID, entityID, sectionID uuid.UUID) (SectionView, error) {
	if err := s.owner(userID, entityID); err != nil {
		return SectionView{}, err
	}
	var sec models.Section
	if err := s.db.First(&sec, "id = ?", sectionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SectionView{}, ErrSectionNotFound
		}
		return SectionView{}, err
	}
	if _, err := resolveSection(s.db, entityID, sec.ID, sec.Kind); err != nil {
		return SectionView{}, err
	}
	return SectionView{ID: sec.ID, Name: sec.Name, Kind: sec.Kind, Icon: sec.Icon}, nil
}

// Request: el negocio pide una sección que no está en el catálogo. Si ya existe con ese nombre, la agrega de una vez
// en vez de abrir la petición.
func (s *SectionService) Request(userID, entityID uuid.UUID, kind, name string) (models.SectionRequest, *SectionView, error) {
	if err := s.owner(userID, entityID); err != nil {
		return models.SectionRequest{}, nil, err
	}
	if !models.ValidSectionKind(kind) {
		return models.SectionRequest{}, nil, &InvalidError{"tipo de sección inválido"}
	}
	name = cleanSectionName(name)
	if n := utf8.RuneCountInString(name); n < 2 || n > 40 {
		return models.SectionRequest{}, nil, &InvalidError{"escribe el nombre de la sección (de 2 a 40 letras)"}
	}
	var all []models.Section
	if err := s.db.Where("kind = ?", kind).Find(&all).Error; err != nil {
		return models.SectionRequest{}, nil, err
	}
	for _, sec := range all {
		if ledger.Normalize(sec.Name) == ledger.Normalize(name) {
			v, err := s.Add(userID, entityID, sec.ID)
			return models.SectionRequest{}, &v, err
		}
	}
	var dup models.SectionRequest
	err := s.db.Where("entity_id = ? AND kind = ? AND status = ? AND lower(name) = lower(?)", entityID, kind, models.SectionRequestOpen, name).First(&dup).Error
	if err == nil {
		return dup, nil, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.SectionRequest{}, nil, err
	}
	req := models.SectionRequest{EntityID: entityID, UserID: userID, Kind: kind, Name: name, Status: models.SectionRequestOpen}
	return req, nil, s.db.Create(&req).Error
}

// cleanSectionName: espacios sencillos y mayúscula inicial, para que «bebidas » y «Bebidas» sean la misma.
func cleanSectionName(n string) string {
	n = strings.Join(strings.Fields(n), " ")
	if n == "" {
		return ""
	}
	r := []rune(n)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// SectionRequestView: una petición con el nombre del negocio, para el panel de admin.
type SectionRequestView struct {
	models.SectionRequest
	EntityName string `json:"entity_name"`
}

// ListRequests: las peticiones de secciones por estado (vacío = abiertas), las más nuevas primero. Solo admin.
func (s *SectionService) ListRequests(status string) ([]SectionRequestView, error) {
	if status == "" {
		status = models.SectionRequestOpen
	}
	var out []SectionRequestView
	err := s.db.Table("section_requests r").Select("r.*, e.name AS entity_name").
		Joins("LEFT JOIN entities e ON e.id = r.entity_id").
		Where("r.status = ?", status).Order("r.created_at DESC").Limit(200).Scan(&out).Error
	return out, err
}

// ResolveRequest: el admin agrega la sección pedida al catálogo (con el nombre que elija, por defecto el pedido) y se
// la deja puesta al negocio que la pidió, o descarta la petición.
func (s *SectionService) ResolveRequest(requestID uuid.UUID, add bool, name string) (models.SectionRequest, error) {
	var req models.SectionRequest
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&req, "id = ?", requestID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSectionNotFound
			}
			return err
		}
		if req.Status != models.SectionRequestOpen {
			return &InvalidError{"esta petición ya se resolvió"}
		}
		now := time.Now()
		req.ResolvedAt = &now
		if !add {
			req.Status = models.SectionRequestDismissed
			return tx.Model(&req).Select("status", "resolved_at").Updates(&req).Error
		}
		final := cleanSectionName(firstNonEmpty(name, req.Name))
		if n := utf8.RuneCountInString(final); n < 2 || n > 40 {
			return &InvalidError{"el nombre de la sección debe tener de 2 a 40 letras"}
		}
		sec := models.Section{Kind: req.Kind, Name: final, Icon: database.SectionIcon(req.Kind, final)}
		if err := tx.Where("kind = ? AND name = ?", req.Kind, final).FirstOrCreate(&sec).Error; err != nil {
			return err
		}
		if err := tx.Where("entity_id = ? AND section_id = ?", req.EntityID, sec.ID).
			FirstOrCreate(&models.EntitySection{EntityID: req.EntityID, SectionID: sec.ID}).Error; err != nil {
			return err
		}
		req.Status, req.SectionID = models.SectionRequestAdded, &sec.ID
		return tx.Model(&req).Select("status", "resolved_at", "section_id").Updates(&req).Error
	})
	return req, err
}
