package repository

import (
	"time"

	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EntityRepository struct {
	DB *gorm.DB
}

func NewEntityRepository(db *gorm.DB) *EntityRepository {
	return &EntityRepository{DB: db}
}

func (r *EntityRepository) Create(entity *models.Entity) error {
	// Simple create, Lat/Long are now regular float columns
	return r.DB.Create(entity).Error
}

func (r *EntityRepository) Update(entity *models.Entity) error {
	return r.DB.Save(entity).Error
}

func (r *EntityRepository) FindByID(id uuid.UUID) (*models.Entity, error) {
	var entity models.Entity
	err := r.DB.Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Photos", func(db *gorm.DB) *gorm.DB {
		return db.Joins("Media")
	}).Preload("Subcategories").Preload("Hours").First(&entity, "entities.id = ?", id).Error
	return &entity, err
}

func (r *EntityRepository) FindAll(lat, long, radius float64, categoryID, subcategoryID, query string, openNow bool, page, pageSize int) ([]models.Entity, int64, error) {
	var entities []models.Entity
	var total int64

	db := r.DB.Model(&models.Entity{}).Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Photos", func(db *gorm.DB) *gorm.DB {
		return db.Joins("Media")
	}).Preload("Subcategories").Preload("Hours")

	// Filter by Category
	db = WhereCategories(db, "category_id", categoryID)

	// Filter by Subcategory (many-to-many via entity_subcategories)
	if subcategoryID != "" {
		db = db.Where(
			"EXISTS (SELECT 1 FROM entity_subcategories es WHERE es.entity_id = entities.id AND es.subcategory_id = ?)",
			subcategoryID,
		)
	}

	// Filter by free-text search: name, description, address, category name or subcategory name.
	if query != "" {
		like := "%" + query + "%"
		db = db.Where(
			"entities.name ILIKE ? OR entities.description ILIKE ? OR entities.address ILIKE ? OR "+
				"EXISTS (SELECT 1 FROM categories WHERE categories.id = entities.category_id AND categories.name ILIKE ?) OR "+
				"EXISTS (SELECT 1 FROM entity_subcategories es JOIN subcategories s ON s.id = es.subcategory_id WHERE es.entity_id = entities.id AND s.name ILIKE ?)",
			like, like, like, like, like,
		)
	}

	// Filter by Location - NAIVE IMPLEMENTATION (Bounding Box)
	if lat != 0 && long != 0 {
		if radius == 0 {
			radius = 5000 // 5km
		}

		// Approximate degrees: 1 degree ~= 111km
		degRadius := radius / 111000.0

		minLat := lat - degRadius
		maxLat := lat + degRadius
		minLong := long - degRadius
		maxLong := long + degRadius

		db = db.Where("latitude BETWEEN ? AND ?", minLat, maxLat).
			Where("longitude BETWEEN ? AND ?", minLong, maxLong)
	}

	// Filter by "open now": comparamos contra el horario de HOY y de AYER (para
	// negocios cuyo horario de ayer cruza la medianoche y sigue "abierto" ya
	// entrada la madrugada de hoy).
	if openNow {
		now := utils.NowInCartagena()
		today := int(now.Weekday())
		yesterday := (today + 6) % 7
		nowTime := now.Format("15:04")

		db = db.Where(
			`EXISTS (
				SELECT 1 FROM business_hours bh
				WHERE bh.entity_id = entities.id AND (
					(bh.weekday = ? AND bh.is24h = true)
					OR (bh.weekday = ? AND bh.closed = false AND bh.is24h = false AND bh.close_time > bh.open_time AND ? >= bh.open_time AND ? < bh.close_time)
					OR (bh.weekday = ? AND bh.closed = false AND bh.is24h = false AND bh.close_time <= bh.open_time AND ? >= bh.open_time)
					OR (bh.weekday = ? AND bh.closed = false AND bh.is24h = false AND bh.close_time <= bh.open_time AND ? < bh.close_time)
				)
			)`,
			today, today, nowTime, nowTime, today, nowTime, yesterday, nowTime,
		)
	}

	// Ocultar del mapa/búsqueda (descubrimiento) los negocios que superaron el
	// umbral gratis de clientes distintos y no tienen un plan activo, PERO ya
	// pasó el margen de gracia desde que se les avisó (7 días — mismo valor
	// que services.TrialGracePeriodDays, se mantiene igual a mano). Un
	// negocio recién avisado sigue apareciendo durante el margen; uno que ya
	// pagó nunca entra aquí porque su suscripción queda 'active'. Esto NO
	// afecta FindByID: un cliente que ya tiene una conversación con el
	// negocio lo sigue viendo y le puede seguir escribiendo.
	db = db.Where(
		`NOT EXISTS (
			SELECT 1 FROM subscriptions sub
			WHERE sub.entity_id = entities.id
				AND sub.status != 'active'
				AND sub.trial_threshold_notified_at IS NOT NULL
				AND sub.trial_threshold_notified_at < NOW() - INTERVAL '7 days'
		)`,
	)

	// Count total records before applying pagination
	db.Count(&total)

	// Apply Pagination
	offset := (page - 1) * pageSize
	err := db.Limit(pageSize).Offset(offset).Find(&entities).Error

	return entities, total, err
}

// FindByBBox returns the businesses inside a visible rectangle plus the total
// count (for the counter). Same filters as FindAll (category, subcategory,
// search, subscription-hide) but no pagination and no open-now filter: it
// returns up to `cap` entities for ranking, and `total` is the real count.
func (r *EntityRepository) FindByBBox(minLat, maxLat, minLng, maxLng float64, categoryID, subcategoryID, query string, cap int) ([]models.Entity, int64, error) {
	var entities []models.Entity
	var total int64

	// Las fotos NO se precargan todas: se agregan después solo las más recientes de cada negocio (attachRecentPhotos).
	db := r.DB.Model(&models.Entity{}).Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Subcategories").Preload("Hours")

	db = WhereCategories(db, "category_id", categoryID)
	if subcategoryID != "" {
		db = db.Where(
			"EXISTS (SELECT 1 FROM entity_subcategories es WHERE es.entity_id = entities.id AND es.subcategory_id = ?)",
			subcategoryID,
		)
	}
	if query != "" {
		like := "%" + query + "%"
		db = db.Where(
			"entities.name ILIKE ? OR entities.description ILIKE ? OR entities.address ILIKE ? OR "+
				"EXISTS (SELECT 1 FROM categories WHERE categories.id = entities.category_id AND categories.name ILIKE ?) OR "+
				"EXISTS (SELECT 1 FROM entity_subcategories es JOIN subcategories s ON s.id = es.subcategory_id WHERE es.entity_id = entities.id AND s.name ILIKE ?)",
			like, like, like, like, like,
		)
	}

	if minLat != 0 || maxLat != 0 || minLng != 0 || maxLng != 0 {
		db = db.Where("latitude BETWEEN ? AND ?", minLat, maxLat).
			Where("longitude BETWEEN ? AND ?", minLng, maxLng)
	}

	// Ocultar negocios que superaron el umbral gratis (igual que FindAll).
	db = db.Where(
		`NOT EXISTS (
			SELECT 1 FROM subscriptions sub
			WHERE sub.entity_id = entities.id
				AND sub.status != 'active'
				AND sub.trial_threshold_notified_at IS NOT NULL
				AND sub.trial_threshold_notified_at < NOW() - INTERVAL '7 days'
		)`,
	)

	db.Count(&total)

	if err := db.Limit(cap).Find(&entities).Error; err != nil {
		return entities, total, err
	}
	return entities, total, r.attachRecentPhotos(entities, RecentPhotosMax)
}

// RecentPhotosMax es cuántas fotos de cada negocio trae el listado del mapa (Descubre dibuja 3 y la tira 2).
const RecentPhotosMax = 4

// ImagePath es la ruta pública y estable de un medio (/api/images/{id} redirige a un enlace firmado nuevo). Al ser
// siempre la misma, el teléfono guarda la foto y no la vuelve a bajar en cada respuesta, y el servidor no firma nada.
func ImagePath(mediaID uuid.UUID) string { return "/api/images/" + mediaID.String() }

// attachRecentPhotos pone en cada negocio sus `max` fotos más recientes (de la publicación más nueva a la más vieja y
// en su orden dentro de cada una; sin videos), con la ruta estable como URL, y cuántas fotos tiene en total. Antes se
// precargaban TODAS las fotos y se firmaba un enlace por cada una: con 150 negocios de 12 fotos eran 1.800 filas y
// 1.800 firmas por petición.
func (r *EntityRepository) attachRecentPhotos(entities []models.Entity, max int) error {
	if len(entities) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(entities))
	for i := range entities {
		ids[i] = entities[i].ID
	}
	type photoRow struct {
		ID          uuid.UUID
		PostID      uuid.UUID
		EntityID    uuid.UUID
		MediaID     uuid.UUID
		Order       int `gorm:"column:order"`
		ContentType string
	}
	var rows []photoRow
	err := r.DB.Raw(`SELECT x.id, x.post_id, x.entity_id, x.media_id, x."order", x.content_type FROM (
			SELECT ep.id, ep.post_id, ep.entity_id, ep.media_id, ep."order", m.content_type,
				ROW_NUMBER() OVER (PARTITION BY ep.entity_id ORDER BY p.created_at DESC, ep."order" ASC) AS rn
			FROM entity_photos ep
			JOIN posts p ON p.id = ep.post_id
			JOIN media m ON m.id = ep.media_id
			WHERE ep.entity_id IN ? AND m.content_type NOT LIKE 'video/%'
		) x WHERE x.rn <= ? ORDER BY x.entity_id, x.rn`, ids, max).Scan(&rows).Error
	if err != nil {
		return err
	}
	type countRow struct {
		EntityID uuid.UUID
		N        int
	}
	var counts []countRow
	err = r.DB.Raw(`SELECT ep.entity_id, COUNT(*) AS n FROM entity_photos ep JOIN media m ON m.id = ep.media_id
		WHERE ep.entity_id IN ? AND m.content_type NOT LIKE 'video/%' GROUP BY ep.entity_id`, ids).Scan(&counts).Error
	if err != nil {
		return err
	}
	byEntity := make(map[uuid.UUID][]models.EntityPhoto, len(entities))
	for _, p := range rows {
		byEntity[p.EntityID] = append(byEntity[p.EntityID], models.EntityPhoto{
			ID: p.ID, PostID: p.PostID, EntityID: p.EntityID, MediaID: p.MediaID, Order: p.Order,
			Media: models.Media{ID: p.MediaID, ContentType: p.ContentType, URL: ImagePath(p.MediaID)},
		})
	}
	total := make(map[uuid.UUID]int, len(counts))
	for _, c := range counts {
		total[c.EntityID] = c.N
	}
	for i := range entities {
		entities[i].Photos = byEntity[entities[i].ID]
		entities[i].PhotosCount = total[entities[i].ID]
	}
	return nil
}

func (r *EntityRepository) FindAllByOwner(ownerID uuid.UUID, page, pageSize int) ([]models.Entity, int64, error) {
	var entities []models.Entity
	var total int64

	db := r.DB.Model(&models.Entity{}).Where("entities.owner_id = ?", ownerID)
	db.Count(&total)

	offset := (page - 1) * pageSize
	err := db.Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Photos", func(db *gorm.DB) *gorm.DB {
		return db.Joins("Media")
	}).Preload("Photos.Post").Preload("Subcategories").Limit(pageSize).Offset(offset).Find(&entities).Error

	return entities, total, err
}

func (r *EntityRepository) FindAllByStatus(status models.VerificationStatus, page, pageSize int) ([]models.Entity, int64, error) {
	var entities []models.Entity
	var total int64

	db := r.DB.Model(&models.Entity{})
	if status != "" {
		db = db.Where("verification_status = ?", status)
	}
	db.Count(&total)

	offset := (page - 1) * pageSize
	err := db.Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").
		Order("entities.created_at ASC").
		Limit(pageSize).Offset(offset).Find(&entities).Error

	return entities, total, err
}

// SetSubcategories replaces an entity's subcategory associations. Subcategory
// rows are fetched by ID first (rather than synthesizing them from the IDs
// alone) so GORM's association Replace only touches the join table and never
// overwrites the subcategory's own Name/CategoryID.
func (r *EntityRepository) SetSubcategories(entity *models.Entity, subcategoryIDs []uuid.UUID) error {
	if len(subcategoryIDs) == 0 {
		return r.DB.Model(entity).Association("Subcategories").Clear()
	}

	var subs []models.Subcategory
	if err := r.DB.Where("id IN ?", subcategoryIDs).Find(&subs).Error; err != nil {
		return err
	}

	return r.DB.Model(entity).Association("Subcategories").Replace(subs)
}

// SetHours replaces an entity's weekly schedule (one row per weekday). An
// empty slice clears it back to "sin horario especificado".
func (r *EntityRepository) SetHours(entity *models.Entity, hours []models.BusinessHour) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("entity_id = ?", entity.ID).Delete(&models.BusinessHour{}).Error; err != nil {
			return err
		}
		if len(hours) == 0 {
			return nil
		}
		for i := range hours {
			hours[i].ID = uuid.UUID{}
			hours[i].EntityID = entity.ID
		}
		return tx.Create(&hours).Error
	})
}

func (r *EntityRepository) Delete(entity *models.Entity) error {
	return r.DB.Delete(entity).Error
}

// LatestPostTimes devuelve, por negocio, cuándo hizo su última publicación.
// Los negocios que nunca publicaron no aparecen en el mapa devuelto.
func (r *EntityRepository) LatestPostTimes(entityIDs []uuid.UUID) (map[uuid.UUID]time.Time, error) {
	out := make(map[uuid.UUID]time.Time, len(entityIDs))
	if len(entityIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		EntityID uuid.UUID
		Latest   time.Time
	}
	err := r.DB.Table("posts").Select("entity_id, MAX(created_at) AS latest").
		Where("entity_id IN ?", entityIDs).Group("entity_id").Scan(&rows).Error
	for _, row := range rows {
		out[row.EntityID] = row.Latest
	}
	return out, err
}
