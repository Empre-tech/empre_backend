package repository

import (
	"sort"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FavoriteRepository struct {
	DB *gorm.DB
}

func NewFavoriteRepository(db *gorm.DB) *FavoriteRepository {
	return &FavoriteRepository{DB: db}
}

// Add saves the (user, entity) pair if it isn't already there.
func (r *FavoriteRepository) Add(userID, entityID uuid.UUID) error {
	fav := models.Favorite{UserID: userID, EntityID: entityID}
	return r.DB.Where(models.Favorite{UserID: userID, EntityID: entityID}).FirstOrCreate(&fav).Error
}

func (r *FavoriteRepository) Remove(userID, entityID uuid.UUID) error {
	return r.DB.Where("user_id = ? AND entity_id = ?", userID, entityID).Delete(&models.Favorite{}).Error
}

func (r *FavoriteRepository) IsFavorite(userID, entityID uuid.UUID) (bool, error) {
	var count int64
	err := r.DB.Model(&models.Favorite{}).Where("user_id = ? AND entity_id = ?", userID, entityID).Count(&count).Error
	return count > 0, err
}

// FindEntitiesByUser returns the businesses a user has favorited, most
// recently favorited first.
func (r *FavoriteRepository) FindEntitiesByUser(userID uuid.UUID, page, pageSize int) ([]models.Entity, int64, error) {
	var favorites []models.Favorite
	var total int64

	db := r.DB.Model(&models.Favorite{}).Where("user_id = ?", userID)
	db.Count(&total)

	offset := (page - 1) * pageSize
	if err := db.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&favorites).Error; err != nil {
		return nil, 0, err
	}

	if len(favorites) == 0 {
		return []models.Entity{}, total, nil
	}

	ids := make([]uuid.UUID, 0, len(favorites))
	for _, f := range favorites {
		ids = append(ids, f.EntityID)
	}

	var entities []models.Entity
	err := r.DB.Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Subcategories").
		Where("entities.id IN ?", ids).Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}

	// The IN query doesn't preserve order: re-sort to match "most recently favorited first".
	order := make(map[uuid.UUID]int, len(ids))
	for i, id := range ids {
		order[id] = i
	}
	sort.Slice(entities, func(i, j int) bool { return order[entities[i].ID] < order[entities[j].ID] })

	return entities, total, nil
}

// Novedad es algo que un negocio seguido publicó después de que lo empezaste a
// seguir: una publicación o una oferta.
type Novedad struct {
	Kind     string    `json:"kind"` // post | offer
	ID       string    `json:"id"`
	EntityID uuid.UUID `json:"entity_id"`
	At       time.Time `json:"at"`
	Title    string    `json:"title"`
	Unseen   bool      `json:"unseen"`
}

// NovedadesLimit evita que una cuenta que sigue a muchos negocios reciba una lista sin fin.
const NovedadesLimit = 100

// Novedades devuelve, de más nueva a más vieja, lo publicado por los negocios
// que sigue el usuario desde el momento en que los siguió y no antes de `since`.
// `Unseen` es lo posterior a la última vez que vio las novedades de ese negocio.
func (r *FavoriteRepository) Novedades(userID uuid.UUID, since time.Time) ([]Novedad, error) {
	var out []Novedad
	err := r.DB.Raw(`
		SELECT * FROM (
			SELECT 'post' AS kind, p.id::text AS id, p.entity_id AS entity_id, p.created_at AS at, p.caption AS title,
			       p.created_at > COALESCE(f.novedades_seen_at, f.created_at) AS unseen
			FROM posts p
			JOIN favorites f ON f.entity_id = p.entity_id AND f.user_id = ?
			WHERE p.created_at > f.created_at AND p.created_at >= ?
			UNION ALL
			SELECT 'offer', o.id::text, o.entity_id, o.created_at, o.title,
			       o.created_at > COALESCE(f.novedades_seen_at, f.created_at)
			FROM offers o
			JOIN favorites f ON f.entity_id = o.entity_id AND f.user_id = ?
			WHERE o.deleted_at IS NULL AND o.created_at > f.created_at AND o.created_at >= ?
		) n
		ORDER BY at DESC
		LIMIT ?`, userID, since, userID, since, NovedadesLimit).Scan(&out).Error
	return out, err
}

// MarkNovedadesSeen marca como vistas las novedades de un negocio seguido, o de todos si entityID es nil.
func (r *FavoriteRepository) MarkNovedadesSeen(userID uuid.UUID, entityID *uuid.UUID, at time.Time) error {
	q := r.DB.Model(&models.Favorite{}).Where("user_id = ?", userID)
	if entityID != nil {
		q = q.Where("entity_id = ?", *entityID)
	}
	return q.Update("novedades_seen_at", at).Error
}

// CountFollowers es cuánta gente sigue al negocio. Nunca quiénes.
func (r *FavoriteRepository) CountFollowers(entityID uuid.UUID) (int64, error) {
	var count int64
	err := r.DB.Model(&models.Favorite{}).Where("entity_id = ?", entityID).Count(&count).Error
	return count, err
}
