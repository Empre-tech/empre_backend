package services

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"empre_backend/internal/models"
)

// Tipos de publicación que se pueden pedir en «Todas las fotos».
const (
	PostKindAll   = "all"
	PostKindPhoto = "photo"
	PostKindVideo = "video"
)

const (
	// PostPageDefault y PostPageMax: publicaciones por página (la app pide 30 al bajar).
	PostPageDefault = 30
	PostPageMax     = 50
)

// PostFeedService lista las publicaciones (fotos y videos) de un negocio por páginas.
type PostFeedService struct {
	DB    *gorm.DB
	Media *MediaService
}

func NewPostFeedService(db *gorm.DB, media *MediaService) *PostFeedService {
	return &PostFeedService{DB: db, Media: media}
}

// Page devuelve una página de publicaciones del negocio, las más recientes primero, y cuántas hay en total
// para ese tipo. «video»: las que traen al menos un video; «photo»: las que traen al menos una foto. Una
// publicación sin ningún archivo no se lista.
func (s *PostFeedService) Page(entityID uuid.UUID, page, pageSize int, kind string) ([]models.Post, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = PostPageDefault
	}
	if pageSize > PostPageMax {
		pageSize = PostPageMax
	}
	const withFile = "EXISTS (SELECT 1 FROM entity_photos ep JOIN media m ON m.id = ep.media_id WHERE ep.post_id = posts.id%s)"
	filter := ""
	switch kind {
	case PostKindVideo:
		filter = " AND m.content_type LIKE 'video/%'"
	case PostKindPhoto:
		filter = " AND m.content_type NOT LIKE 'video/%'"
	}
	cond := fmt.Sprintf(withFile, filter)

	db := s.DB.Model(&models.Post{}).Where("posts.entity_id = ?", entityID).Where(cond)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var posts []models.Post
	err := db.
		Preload("Photos", func(d *gorm.DB) *gorm.DB { return d.Order(`entity_photos."order" ASC`) }).
		Preload("Photos.Media").
		Order("posts.created_at DESC, posts.id DESC").
		Limit(pageSize).Offset((page - 1) * pageSize).Find(&posts).Error
	if err != nil {
		return nil, 0, err
	}
	if s.Media != nil {
		for i := range posts {
			for j := range posts[i].Photos {
				s.Media.PopulateURL(&posts[i].Photos[j].Media)
			}
		}
	}
	return posts, total, nil
}
