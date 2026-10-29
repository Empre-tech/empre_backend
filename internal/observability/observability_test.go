package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testRouter(reg *Registry) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(reg), Recovery(reg))
	r.GET("/api/entities/:id", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.GET("/boom", func(c *gin.Context) { panic("se rompió") })
	return r
}

func TestRequestIDIsKeptOrCreated(t *testing.T) {
	reg := NewRegistry()
	r := testRouter(reg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/entities/123", nil)
	req.Header.Set(HeaderRequestID, "app-abc12345")
	r.ServeHTTP(w, req)
	if got := w.Header().Get(HeaderRequestID); got != "app-abc12345" {
		t.Fatalf("the app's id must come back, got %q", got)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/entities/123", nil)
	req.Header.Set(HeaderRequestID, "x y") // inválido: se crea uno nuevo
	r.ServeHTTP(w, req)
	if got := w.Header().Get(HeaderRequestID); len(got) != 16 {
		t.Fatalf("want a new 16-char id, got %q", got)
	}
}

func TestRoutesAreTemplatesNotURLs(t *testing.T) {
	reg := NewRegistry()
	r := testRouter(reg)
	for _, id := range []string{"a", "b", "c"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/entities/"+id, nil))
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/no/existe/123", nil))
	snap := reg.Snapshot()
	got := map[string]uint64{}
	for _, s := range snap {
		got[s.Route] = s.Count
	}
	if got["/api/entities/:id"] != 3 {
		t.Fatalf("the three ids must share one series: %+v", snap)
	}
	if got["sin_ruta"] != 1 || len(got) != 2 {
		t.Fatalf("404s go to one series: %+v", snap)
	}
}

func TestPanicBecomes500WithRequestID(t *testing.T) {
	reg := NewRegistry()
	r := testRouter(reg)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/boom", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["request_id"] == "" || body["request_id"] != w.Header().Get(HeaderRequestID) {
		t.Fatalf("the error must carry the request id: %v", body)
	}
	if s := reg.Snapshot(); len(s) != 1 || s[0].Errors5x != 1 {
		t.Fatalf("the 500 must be counted: %+v", s)
	}
}

func TestPrometheusOutputAndQuantile(t *testing.T) {
	reg := NewRegistry()
	for i := 0; i < 95; i++ {
		reg.Observe("GET", "/api/x", 200, 20*time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		reg.Observe("GET", "/api/x", 503, 3*time.Second)
	}
	reg.Inc("sale_created")
	var b strings.Builder
	reg.WritePrometheus(&b)
	out := b.String()
	for _, want := range []string{
		`empre_http_request_duration_ms_bucket{method="GET",route="/api/x",le="25"} 95`,
		`empre_http_request_duration_ms_bucket{method="GET",route="/api/x",le="+Inf"} 100`,
		`empre_http_requests_total{method="GET",route="/api/x",class="5xx"} 5`,
		`empre_events_total{name="sale_created"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if p95 := reg.Snapshot()[0].P95Ms; p95 != 25 {
		t.Fatalf("p95 should fall in the 25 ms bucket, got %v", p95)
	}
}

func TestServerTimingOnlyWhenAsked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Timing())
	r.GET("/x", func(c *gin.Context) {
		// Dos consultas simuladas de 30 ms.
		dbQueries.Add(2)
		dbNanos.Add(int64(60 * time.Millisecond))
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Header().Get("Server-Timing") != "" {
		t.Fatal("without X-Timing there must be no Server-Timing")
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set(HeaderTiming, "1")
	r.ServeHTTP(w, req)
	st := w.Header().Get("Server-Timing")
	if !strings.Contains(st, `db;dur=60.0;desc="2"`) || !strings.HasPrefix(st, "app;dur=") {
		t.Fatalf("unexpected Server-Timing: %q", st)
	}
}
