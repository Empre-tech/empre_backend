package services

import (
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Nombres de las funciones que usan IA, tal como quedan en ai_usage.feature.
const (
	AIFeatureDictado       = "dictado"
	AIFeatureRedactar      = "redactar"
	AIFeatureAsistente     = "asistente_negocio"
	AIFeaturePregunta      = "pregunta"
	AIFeatureChatClasifica = "chat_clasificar"
	AIFeatureChatFicha     = "chat_ficha"
)

// AIPrices son los precios del modelo en USD por millón de tokens, más el recargo del proveedor (0.046 = 4,6 %).
// Se leen de AI_PRICE_INPUT_PER_M, AI_PRICE_OUTPUT_PER_M, AI_PRICE_CACHE_READ_PER_M, AI_PRICE_CACHE_WRITE_PER_M
// y AI_PRICE_MARKUP; por defecto, los de Claude Haiku en OpenCode Zen.
type AIPrices struct {
	InputPerM      float64 `json:"input_per_m"`
	OutputPerM     float64 `json:"output_per_m"`
	CacheReadPerM  float64 `json:"cache_read_per_m"`
	CacheWritePerM float64 `json:"cache_write_per_m"`
	Markup         float64 `json:"markup"`
}

func AIPricesFromEnv() AIPrices {
	return AIPrices{
		InputPerM:      envFloat("AI_PRICE_INPUT_PER_M", 0.10),
		OutputPerM:     envFloat("AI_PRICE_OUTPUT_PER_M", 0.50),
		CacheReadPerM:  envFloat("AI_PRICE_CACHE_READ_PER_M", 0.01),
		CacheWritePerM: envFloat("AI_PRICE_CACHE_WRITE_PER_M", 0.125),
		Markup:         envFloat("AI_PRICE_MARKUP", 0.046),
	}
}

func envFloat(name string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(name), 64); err == nil && v >= 0 {
		return v
	}
	return def
}

// Cost es el costo estimado en USD de esos tokens.
func (p AIPrices) Cost(in, out, cacheRead, cacheWrite int64) float64 {
	usd := (float64(in)*p.InputPerM + float64(out)*p.OutputPerM +
		float64(cacheRead)*p.CacheReadPerM + float64(cacheWrite)*p.CacheWritePerM) / 1e6
	return math.Round(usd*(1+p.Markup)*1e6) / 1e6
}

// AIUsageService guarda los tokens de cada llamada a la IA y los resume para el panel de administración.
type AIUsageService struct {
	db     *gorm.DB
	prices AIPrices
	queue  chan models.AIUsage
}

func NewAIUsageService(db *gorm.DB, prices AIPrices) *AIUsageService {
	return &AIUsageService{db: db, prices: prices}
}

func usageRow(r AIUsageRecord, at time.Time) models.AIUsage {
	feature := r.Meta.Feature
	if feature == "" {
		feature = "otro"
	}
	return models.AIUsage{
		ID: uuid.New(), Feature: feature, EntityID: r.Meta.EntityID, UserID: r.Meta.UserID, Model: r.Model,
		InputTokens: r.Usage.InputTokens, OutputTokens: r.Usage.OutputTokens,
		CacheReadTokens: r.Usage.CacheReadTokens, CacheWriteTokens: r.Usage.CacheWriteTokens,
		LatencyMS: r.LatencyMS, CreatedAt: at,
	}
}

// Record guarda una llamada ya mismo.
func (s *AIUsageService) Record(r AIUsageRecord) error {
	row := usageRow(r, time.Now())
	return s.db.Create(&row).Error
}

// Async devuelve el Recorder para AIService: encola la fila y la guarda un worker aparte, para no sumar la escritura
// a lo que espera el usuario. Si la cola se llena (la base está lenta) la fila se descarta: el registro es para medir,
// no puede tumbar la respuesta.
func (s *AIUsageService) Async() func(AIUsageRecord) {
	if s.queue == nil {
		s.queue = make(chan models.AIUsage, 512)
		go func() {
			for row := range s.queue {
				if err := s.db.Create(&row).Error; err != nil {
					log.Println("ai_usage: no se pudo guardar:", err)
				}
			}
		}()
	}
	return func(r AIUsageRecord) {
		select {
		case s.queue <- usageRow(r, time.Now()):
		default:
			log.Println("ai_usage: cola llena, se descarta un registro")
		}
	}
}

// AIUsageTotals son las sumas de un grupo de llamadas.
type AIUsageTotals struct {
	Calls            int64   `json:"calls"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	AvgLatencyMS     float64 `json:"avg_latency_ms"`
	CostUSD          float64 `json:"cost_usd"`
	CostPerCallUSD   float64 `json:"cost_per_call_usd"`
}

type AIUsageByFeature struct {
	Feature string `json:"feature"`
	AIUsageTotals
}

type AIUsageByEntity struct {
	EntityID uuid.UUID `json:"entity_id"`
	Name     string    `json:"name"`
	AIUsageTotals
}

type AIUsageSummary struct {
	Since       time.Time          `json:"since"`
	Days        int                `json:"days"`
	Prices      AIPrices           `json:"prices"`
	Total       AIUsageTotals      `json:"total"`
	ByFeature   []AIUsageByFeature `json:"by_feature"`
	TopEntities []AIUsageByEntity  `json:"top_entities"`
}

const usageSums = `COUNT(*) AS calls, COALESCE(SUM(u.input_tokens),0) AS input_tokens, COALESCE(SUM(u.output_tokens),0) AS output_tokens,
COALESCE(SUM(u.cache_read_tokens),0) AS cache_read_tokens, COALESCE(SUM(u.cache_write_tokens),0) AS cache_write_tokens,
COALESCE(AVG(u.latency_ms),0) AS avg_latency_ms`

func (s *AIUsageService) price(t *AIUsageTotals) {
	t.CostUSD = s.prices.Cost(t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.CacheWriteTokens)
	if t.Calls > 0 {
		t.CostPerCallUSD = math.Round(t.CostUSD/float64(t.Calls)*1e6) / 1e6
	}
	t.AvgLatencyMS = math.Round(t.AvgLatencyMS)
}

// Summary resume los últimos `days` días: el total, por función y los `top` negocios que más gastan.
func (s *AIUsageService) Summary(days, top int, now time.Time) (AIUsageSummary, error) {
	since := now.Add(-time.Duration(days) * 24 * time.Hour)
	out := AIUsageSummary{Since: since, Days: days, Prices: s.prices, ByFeature: []AIUsageByFeature{}, TopEntities: []AIUsageByEntity{}}
	if err := s.db.Table("ai_usage u").Select(usageSums).Where("u.created_at >= ?", since).Scan(&out.Total).Error; err != nil {
		return out, err
	}
	s.price(&out.Total)
	if err := s.db.Table("ai_usage u").Select("u.feature AS feature, "+usageSums).Where("u.created_at >= ?", since).
		Group("u.feature").Order("SUM(u.input_tokens + u.output_tokens) DESC").Scan(&out.ByFeature).Error; err != nil {
		return out, err
	}
	for i := range out.ByFeature {
		s.price(&out.ByFeature[i].AIUsageTotals)
	}
	if err := s.db.Table("ai_usage u").Select("u.entity_id AS entity_id, COALESCE(e.name,'') AS name, "+usageSums).
		Joins("LEFT JOIN entities e ON e.id = u.entity_id").
		Where("u.created_at >= ? AND u.entity_id IS NOT NULL", since).Group("u.entity_id, e.name").
		Order("SUM(u.input_tokens + u.output_tokens) DESC").Limit(top).Scan(&out.TopEntities).Error; err != nil {
		return out, err
	}
	for i := range out.TopEntities {
		s.price(&out.TopEntities[i].AIUsageTotals)
	}
	return out, nil
}
