package services

import (
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

// Lo que el dueño le enseña a Empre AI y lo que Empre AI no supo responder (3.62, F3).

const (
	MaxBusinessAnswers   = 30
	MaxAnswerChars       = 500
	MaxQuestionChars     = 200
	UnansweredWindow     = 30 * 24 * time.Hour
	ReplyTimeWindow      = 30 * 24 * time.Hour
	MinReplyConversation = 5
)

var (
	ErrAnswerInput   = errors.New("falta la respuesta o la pregunta")
	ErrTooManyAnswer = errors.New("ya hay 30 respuestas: borra una antes de agregar otra")
	ErrAIChatOff     = errors.New("el negocio apagó las respuestas de Empre AI")
)

// NormalizeQuestion: minúsculas, sin tildes ni signos y con espacios simples. Sirve para no repetir la misma pregunta.
func NormalizeQuestion(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var stopwords = map[string]bool{
	"hola": true, "para": true, "como": true, "cual": true, "cuales": true, "cuanto": true, "tienen": true, "tiene": true,
	"hacen": true, "hace": true, "puedo": true, "pueden": true, "ustedes": true, "usted": true, "esta": true, "este": true,
	"estan": true, "quiero": true, "buenas": true, "buenos": true, "tardes": true, "noches": true, "dias": true, "favor": true,
	"gracias": true, "donde": true, "cuando": true, "algun": true, "alguna": true, "tambien": true, "sobre": true, "porque": true,
}

// KeywordsFrom saca las palabras con que se reconoce una pregunta propia: las de 4 letras o más que no son de relleno.
func KeywordsFrom(question string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range strings.Fields(NormalizeQuestion(question)) {
		if len([]rune(w)) < 4 || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == 6 {
			break
		}
	}
	return out
}

// MedianReplyMinutes es la mediana de los minutos hasta la primera respuesta del dueño. Con menos de
// MinReplyConversation conversaciones no se dice nada: un promedio de dos casos engaña.
func MedianReplyMinutes(minutes []float64) (int, bool) {
	if len(minutes) < MinReplyConversation {
		return 0, false
	}
	m := append([]float64(nil), minutes...)
	sort.Float64s(m)
	mid := len(m) / 2
	v := m[mid]
	if len(m)%2 == 0 {
		v = (m[mid-1] + m[mid]) / 2
	}
	if v < 1 {
		v = 1
	}
	return int(v + 0.5), true
}

type BusinessAnswerService struct {
	db        *gorm.DB
	replyTime *TTLCache[uuid.UUID, replyTime]
}

type replyTime struct {
	minutes int
	ok      bool
}

func NewBusinessAnswerService(db *gorm.DB) *BusinessAnswerService {
	return &BusinessAnswerService{db: db, replyTime: NewTTLCache[uuid.UUID, replyTime](30 * time.Minute)}
}

func (s *BusinessAnswerService) requireOwner(entityID, userID uuid.UUID) (*models.Entity, error) {
	var e models.Entity
	if err := s.db.Select("id", "owner_id", "ai_chat_off").First(&e, "id = ?", entityID).Error; err != nil {
		return nil, err
	}
	if e.OwnerID != userID {
		return nil, ErrNotOwner
	}
	return &e, nil
}

// List devuelve lo que el negocio enseñó y si las respuestas de Empre AI están apagadas. Es público: el chat y
// «Pregúntale» lo usan para responder.
func (s *BusinessAnswerService) List(entityID uuid.UUID) ([]models.BusinessAnswer, bool, error) {
	var e models.Entity
	if err := s.db.Select("id", "ai_chat_off").First(&e, "id = ?", entityID).Error; err != nil {
		return nil, false, err
	}
	var list []models.BusinessAnswer
	err := s.db.Where("entity_id = ?", entityID).Order("updated_at DESC").Find(&list).Error
	return list, e.AIChatOff, err
}

type BusinessAnswerInput struct {
	ID       *uuid.UUID `json:"id"`
	Intent   string     `json:"intent"`
	Question string     `json:"question"`
	Answer   string     `json:"answer"`
}

// Upsert guarda una respuesta del dueño. Con Intent hay una sola por intención (la nueva reemplaza a la anterior); sin
// Intent es una pregunta propia y sus palabras clave salen de la pregunta.
func (s *BusinessAnswerService) Upsert(entityID, userID uuid.UUID, in BusinessAnswerInput) (*models.BusinessAnswer, error) {
	if _, err := s.requireOwner(entityID, userID); err != nil {
		return nil, err
	}
	in.Answer = strings.TrimSpace(in.Answer)
	in.Question = strings.TrimSpace(in.Question)
	if in.Intent != "" {
		if _, ok := AIIntents[in.Intent]; !ok {
			return nil, ErrAIIntent
		}
	}
	if in.Answer == "" || len([]rune(in.Answer)) > MaxAnswerChars || len([]rune(in.Question)) > MaxQuestionChars {
		return nil, ErrAnswerInput
	}
	keywords := []string{}
	if in.Intent == "" {
		keywords = KeywordsFrom(in.Question)
		if len(keywords) == 0 {
			return nil, ErrAnswerInput
		}
	}

	var row models.BusinessAnswer
	q := s.db.Where("entity_id = ?", entityID)
	switch {
	case in.ID != nil:
		q = q.Where("id = ?", *in.ID)
	case in.Intent != "":
		q = q.Where("intent = ?", in.Intent)
	default:
		q = q.Where("1 = 0")
	}
	err := q.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var n int64
		if err := s.db.Model(&models.BusinessAnswer{}).Where("entity_id = ?", entityID).Count(&n).Error; err != nil {
			return nil, err
		}
		if n >= MaxBusinessAnswers {
			return nil, ErrTooManyAnswer
		}
		row = models.BusinessAnswer{EntityID: entityID}
	} else if err != nil {
		return nil, err
	}
	row.Intent = in.Intent
	row.Question = in.Question
	row.Keywords = keywords
	row.Answer = in.Answer
	if err := s.db.Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *BusinessAnswerService) Delete(entityID, userID, answerID uuid.UUID) error {
	if _, err := s.requireOwner(entityID, userID); err != nil {
		return err
	}
	return s.db.Where("id = ? AND entity_id = ?", answerID, entityID).Delete(&models.BusinessAnswer{}).Error
}

// SetChatOff prende o apaga las respuestas de Empre AI en el chat del negocio.
func (s *BusinessAnswerService) SetChatOff(entityID, userID uuid.UUID, off bool) error {
	if _, err := s.requireOwner(entityID, userID); err != nil {
		return err
	}
	return s.db.Model(&models.Entity{}).Where("id = ?", entityID).Update("ai_chat_off", off).Error
}

type UnansweredQuestion struct {
	MessageID uuid.UUID `json:"message_id"`
	UserID    uuid.UUID `json:"user_id"`
	Text      string    `json:"text"`
	At        time.Time `json:"at"`
}

// Unanswered: las preguntas que Empre AI no supo responder en los últimos 30 días, una sola vez cada una (la más
// reciente), hasta 20. Solo el dueño.
func (s *BusinessAnswerService) Unanswered(entityID, userID uuid.UUID, now time.Time) ([]UnansweredQuestion, error) {
	if _, err := s.requireOwner(entityID, userID); err != nil {
		return nil, err
	}
	var msgs []models.Message
	if err := s.db.Select("id", "user_id", "content", "created_at").
		Where("entity_id = ? AND ai_unanswered = true AND created_at >= ?", entityID, now.Add(-UnansweredWindow)).
		Order("created_at DESC").Limit(200).Find(&msgs).Error; err != nil {
		return nil, err
	}
	return dedupeUnanswered(msgs, 20), nil
}

func dedupeUnanswered(msgs []models.Message, max int) []UnansweredQuestion {
	seen := map[string]bool{}
	out := []UnansweredQuestion{}
	for _, m := range msgs {
		k := NormalizeQuestion(m.Content)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, UnansweredQuestion{MessageID: m.ID, UserID: m.UserID, Text: m.Content, At: m.CreatedAt})
		if len(out) == max {
			break
		}
	}
	return out
}

// ReplyMinutes: «suele responder en ~N min». Para cada cliente de los últimos 30 días, cuánto tardó la primera
// respuesta del negocio a su primer mensaje; la mediana, con mínimo 5 conversaciones. Se guarda 30 minutos.
func (s *BusinessAnswerService) ReplyMinutes(entityID uuid.UUID, now time.Time) (int, bool, error) {
	if v, ok := s.replyTime.Get(entityID); ok {
		return v.minutes, v.ok, nil
	}
	var mins []float64
	err := s.db.Raw(`
		WITH firsts AS (
		  SELECT user_id, MIN(created_at) AS asked
		  FROM messages
		  WHERE entity_id = ? AND sent_by_entity = false AND sender_id = user_id
		    AND coalesce(kind, 'text') <> 'ai_answer' AND created_at >= ? AND deleted_at IS NULL
		  GROUP BY user_id
		)
		SELECT EXTRACT(EPOCH FROM (r.replied - f.asked)) / 60.0
		FROM firsts f
		JOIN LATERAL (
		  SELECT MIN(m.created_at) AS replied FROM messages m
		  WHERE m.entity_id = ? AND m.user_id = f.user_id AND m.sent_by_entity = true AND m.created_at > f.asked AND m.deleted_at IS NULL
		) r ON r.replied IS NOT NULL
	`, entityID, now.Add(-ReplyTimeWindow), entityID).Scan(&mins).Error
	if err != nil {
		return 0, false, err
	}
	m, ok := MedianReplyMinutes(mins)
	s.replyTime.Set(entityID, replyTime{minutes: m, ok: ok})
	return m, ok, nil
}
