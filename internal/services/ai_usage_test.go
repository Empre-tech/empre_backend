package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// La respuesta de la IA trae "usage": el Recorder recibe esos tokens con la etiqueta de la llamada.
func TestAIServiceRecordsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content":[{"type":"text","text":"hola"}],"usage":{"input_tokens":1200,"output_tokens":80,"cache_read_input_tokens":300,"cache_creation_input_tokens":40}}`))
	}))
	defer srv.Close()

	ai := NewAIService("k", srv.URL, "haiku-test")
	var got []AIUsageRecord
	ai.Recorder = func(r AIUsageRecord) { got = append(got, r) }

	entity, user := uuid.New(), uuid.New()
	reply, err := ai.For(AIUsageMeta{Feature: AIFeatureDictado, EntityID: &entity, UserID: &user}).
		Chat("sys", []AIMessage{{Role: AIRoleUser, Content: "hola"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Usage.InputTokens != 1200 || reply.Usage.OutputTokens != 80 {
		t.Fatalf("usage en la respuesta = %+v", reply.Usage)
	}
	if len(got) != 1 {
		t.Fatalf("registros = %d, quería 1", len(got))
	}
	r := got[0]
	if r.Meta.Feature != AIFeatureDictado || *r.Meta.EntityID != entity || *r.Meta.UserID != user || r.Model != "haiku-test" {
		t.Fatalf("meta = %+v", r)
	}
	if r.Usage != (AIUsage{InputTokens: 1200, OutputTokens: 80, CacheReadTokens: 300, CacheWriteTokens: 40}) {
		t.Fatalf("usage = %+v", r.Usage)
	}

	// Sin etiqueta también se registra (como "otro" al guardarse); con error, no.
	if _, err := ai.Chat("sys", []AIMessage{{Role: AIRoleUser, Content: "x"}}, nil); err != nil || len(got) != 2 {
		t.Fatalf("err=%v registros=%d", err, len(got))
	}
}

func TestAIServiceNoRecordOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"type":"x","message":"caído"}}`))
	}))
	defer srv.Close()
	ai := NewAIService("k", srv.URL, "m")
	calls := 0
	ai.Recorder = func(AIUsageRecord) { calls++ }
	if _, err := ai.Chat("s", []AIMessage{{Role: AIRoleUser, Content: "x"}}, nil); err == nil {
		t.Fatal("quería error")
	}
	if calls != 0 {
		t.Fatalf("registró %d llamadas fallidas", calls)
	}
}

func TestAIPricesCost(t *testing.T) {
	p := AIPrices{InputPerM: 0.10, OutputPerM: 0.50, CacheReadPerM: 0.01, CacheWritePerM: 0.125, Markup: 0.046}
	// 1M de entrada + 1M de salida = 0,60 USD, más 4,6 %.
	if got := p.Cost(1_000_000, 1_000_000, 0, 0); got != 0.6276 {
		t.Fatalf("costo = %v", got)
	}
}

func TestAIUsageSummary(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	user := models.User{Name: "U", Email: "u@x.co", PasswordHash: "x"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	cat := models.Category{Name: "Tiendas"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	ent := models.Entity{OwnerID: user.ID, Name: "Tienda Rosa", CategoryID: cat.ID}
	if err := db.Omit("Owner", "Category").Create(&ent).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewAIUsageService(db, AIPrices{InputPerM: 1, OutputPerM: 2})
	rec := func(feature string, entity *uuid.UUID, in, out int) {
		if err := svc.Record(AIUsageRecord{Meta: AIUsageMeta{Feature: feature, EntityID: entity}, Model: "m", Usage: AIUsage{InputTokens: in, OutputTokens: out}, LatencyMS: 100}); err != nil {
			t.Fatal(err)
		}
	}
	rec(AIFeatureDictado, &ent.ID, 1000, 100)
	rec(AIFeatureDictado, &ent.ID, 3000, 300)
	rec(AIFeaturePregunta, nil, 500, 50)
	// Una vieja que no entra en la ventana.
	old := usageRow(AIUsageRecord{Meta: AIUsageMeta{Feature: AIFeatureDictado}, Usage: AIUsage{InputTokens: 99999}}, now.Add(-40*24*time.Hour))
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	s, err := svc.Summary(30, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total.Calls != 3 || s.Total.InputTokens != 4500 || s.Total.OutputTokens != 450 {
		t.Fatalf("total = %+v", s.Total)
	}
	if len(s.ByFeature) != 2 || s.ByFeature[0].Feature != AIFeatureDictado || s.ByFeature[0].Calls != 2 {
		t.Fatalf("por función = %+v", s.ByFeature)
	}
	// 4000 entrada * 1 + 400 salida * 2 = 4800 / 1M.
	if s.ByFeature[0].CostUSD != 0.0048 || s.ByFeature[0].CostPerCallUSD != 0.0024 {
		t.Fatalf("costo dictado = %+v", s.ByFeature[0].AIUsageTotals)
	}
	if len(s.TopEntities) != 1 || s.TopEntities[0].Name != "Tienda Rosa" || s.TopEntities[0].Calls != 2 {
		t.Fatalf("negocios = %+v", s.TopEntities)
	}
}
