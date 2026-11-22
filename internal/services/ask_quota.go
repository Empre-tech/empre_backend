package services

import (
	"errors"
	"fmt"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cupos de «Pregúntale a Empre» al mes (2026-11-22, decisión de Giovanni): 10 gratis; con la suscripción de usuario
// (9.900 COP), 300.
const (
	AskFreeMonthly = 10
	AskPlusMonthly = 300
)

// ErrAskQuota: el usuario ya usó sus preguntas del mes.
var ErrAskQuota = errors.New("ask quota reached")

// AskQuota es lo que la app muestra: cuántas lleva, de cuántas, con qué plan y cuándo se renueva.
type AskQuota struct {
	Used     int       `json:"used"`
	Limit    int       `json:"limit"`
	Plan     string    `json:"plan"` // free | plus
	ResetsAt time.Time `json:"resets_at"`
}

var bogota = func() *time.Location {
	if l, err := time.LoadLocation("America/Bogota"); err == nil {
		return l
	}
	return time.FixedZone("COT", -5*60*60)
}()

func askMonth(now time.Time) (string, time.Time) {
	t := now.In(bogota)
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, bogota)
	return start.Format("2006-01"), start.AddDate(0, 1, 0)
}

// userPlanActive dice si el usuario tiene la suscripción de usuario vigente.
func userPlanActive(db *gorm.DB, userID uuid.UUID, now time.Time) (bool, error) {
	var n int64
	err := db.Model(&models.UserPlan{}).Where("user_id = ? AND current_period_end > ?", userID, now).Count(&n).Error
	return n > 0, err
}

// Quota devuelve el cupo del mes sin gastar nada.
func (s *AskService) Quota(userID uuid.UUID, now time.Time) (AskQuota, error) {
	month, resets := askMonth(now)
	q := AskQuota{Limit: AskFreeMonthly, Plan: "free", ResetsAt: resets}
	plus, err := userPlanActive(s.DB, userID, now)
	if err != nil {
		return q, err
	}
	if plus {
		q.Limit, q.Plan = AskPlusMonthly, "plus"
	}
	var used int64
	if err := s.DB.Model(&models.AskQuestion{}).Where("user_id = ? AND month = ?", userID, month).Count(&used).Error; err != nil {
		return q, err
	}
	q.Used = int(used)
	return q, nil
}

// UseQuestion cuenta la pregunta en el cupo del mes. La misma pregunta (normalizada) ya hecha este mes no cuenta
// otra vez: cambiar un filtro o volver a buscar es gratis. Con el cupo lleno devuelve ErrAskQuota.
func (s *AskService) UseQuestion(userID uuid.UUID, question string, now time.Time) (AskQuota, error) {
	q, err := s.Quota(userID, now)
	if err != nil {
		return q, err
	}
	month, _ := askMonth(now)
	key := askCacheKey(question)
	if len([]rune(key)) > 300 {
		key = string([]rune(key)[:300])
	}
	var seen int64
	if err := s.DB.Model(&models.AskQuestion{}).Where("user_id = ? AND month = ? AND key = ?", userID, month, key).Count(&seen).Error; err != nil {
		return q, err
	}
	if seen > 0 {
		return q, nil
	}
	if q.Used >= q.Limit {
		return q, ErrAskQuota
	}
	row := models.AskQuestion{ID: uuid.New(), UserID: userID, Month: month, Key: key, CreatedAt: now}
	if err := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return q, fmt.Errorf("contar pregunta: %w", err)
	}
	q.Used++
	return q, nil
}

// GrantPlus activa (o extiende) la suscripción de usuario hasta `until`. La usan el admin (pruebas, cortesías) y,
// más adelante, el aviso de compra de las tiendas.
func (s *AskService) GrantPlus(userID uuid.UUID, until time.Time, source string) error {
	plan := models.UserPlan{UserID: userID, Plan: "plus", CurrentPeriodEnd: until, Source: source}
	return s.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"plan", "current_period_end", "source", "updated_at"}),
	}).Create(&plan).Error
}
