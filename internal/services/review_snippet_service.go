package services

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// La frase de reseña de los destacados: una sola, la más reciente que sirva para decidir ir.
// Sirve la reseña verificada (respaldada por un canje confirmado), con comentario de verdad y de los últimos
// meses. No se filtra por nota: mostrar solo las buenas sería escoger a conveniencia.
const (
	SnippetMinChars = 20
	SnippetMaxChars = 90
	SnippetMaxAge   = 90 * 24 * time.Hour
	SnippetMaxIDs   = 12
)

// ReviewSnippet es la frase elegida de un negocio; AuthorName llega completo y quien responde lo acorta.
type ReviewSnippet struct {
	EntityID   uuid.UUID
	Rating     int
	Comment    string
	CreatedAt  time.Time
	AuthorName string
}

type ReviewSnippetService struct {
	DB *gorm.DB
}

func NewReviewSnippetService(db *gorm.DB) *ReviewSnippetService {
	return &ReviewSnippetService{DB: db}
}

// Latest devuelve, por negocio, su frase elegida. Los negocios sin reseña que sirva no aparecen en el mapa.
func (s *ReviewSnippetService) Latest(ids []uuid.UUID, now time.Time) (map[uuid.UUID]ReviewSnippet, error) {
	out := map[uuid.UUID]ReviewSnippet{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > SnippetMaxIDs {
		ids = ids[:SnippetMaxIDs]
	}
	var rows []ReviewSnippet
	err := s.DB.Raw(`
		SELECT DISTINCT ON (r.entity_id) r.entity_id, r.rating, r.comment, r.created_at, COALESCE(u.name, '') AS author_name
		FROM reviews r
		LEFT JOIN users u ON u.id = r.user_id
		WHERE r.entity_id IN ?
		  AND r.deleted_at IS NULL
		  AND r.claim_id IS NOT NULL
		  AND r.created_at >= ? AND r.created_at <= ?
		  AND char_length(btrim(r.comment)) >= ?
		ORDER BY r.entity_id, r.created_at DESC`,
		ids, now.Add(-SnippetMaxAge), now, SnippetMinChars).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		r.Comment = TrimSnippet(r.Comment)
		out[r.EntityID] = r
	}
	return out, nil
}

// TrimSnippet deja el comentario en una línea y de SnippetMaxChars como mucho, cortando en una palabra.
func TrimSnippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= SnippetMaxChars {
		return text
	}
	cut := runes[:SnippetMaxChars]
	if i := strings.LastIndex(string(cut), " "); i > SnippetMaxChars/2 {
		return strings.TrimRight(string(cut)[:i], " ,.;:") + "…"
	}
	return strings.TrimRight(string(cut), " ,.;:") + "…"
}
