// Package observability mide cómo se comporta el backend (3.53) sin servicios externos: latencia y errores por
// ruta, el pool de la base y contadores de negocio. Se lee en /metrics (formato de texto de Prometheus, así se puede
// conectar a Prometheus + Grafana sin cambiar código) y en el resumen de salud de admin.
package observability

import (
	"database/sql"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Buckets de latencia en milisegundos: del rango de la caché (5 ms) al de una petición colgada (10 s).
var Buckets = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}

type routeKey struct{ method, route string }

type routeStat struct {
	counts  []uint64 // por bucket (acumulado se calcula al exponer); el último es +Inf
	sumMs   float64
	total   uint64
	byClass [6]uint64 // 1xx..5xx por la primera cifra del código
}

// Registry guarda las métricas de un proceso. Es seguro usarlo desde varias peticiones a la vez.
type Registry struct {
	mu       sync.Mutex
	routes   map[routeKey]*routeStat
	counters sync.Map // nombre → *atomic.Int64
	started  time.Time
	db       *sql.DB
}

func NewRegistry() *Registry {
	return &Registry{routes: map[routeKey]*routeStat{}, started: time.Now()}
}

// Default es el registro del proceso (lo usan el middleware, /metrics y el resumen de salud).
var Default = NewRegistry()

// SetDB conecta las estadísticas del pool de la base.
func (r *Registry) SetDB(db *sql.DB) { r.db = db }

// Observe anota una petición terminada.
func (r *Registry) Observe(method, route string, status int, d time.Duration) {
	ms := float64(d.Microseconds()) / 1000
	k := routeKey{method, route}
	r.mu.Lock()
	st := r.routes[k]
	if st == nil {
		st = &routeStat{counts: make([]uint64, len(Buckets)+1)}
		r.routes[k] = st
	}
	i := sort.SearchFloat64s(Buckets, ms)
	st.counts[i]++
	st.sumMs += ms
	st.total++
	if c := status / 100; c >= 1 && c <= 5 {
		st.byClass[c]++
	}
	r.mu.Unlock()
}

// Inc suma 1 a un contador de negocio (ventas registradas, reclamos, errores de trabajos…).
func (r *Registry) Inc(name string) {
	v, _ := r.counters.LoadOrStore(name, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

// RouteSnapshot es lo medido de una ruta.
type RouteSnapshot struct {
	Method   string  `json:"method"`
	Route    string  `json:"route"`
	Count    uint64  `json:"count"`
	Errors5x uint64  `json:"errors_5xx"`
	Errors4x uint64  `json:"errors_4xx"`
	AvgMs    float64 `json:"avg_ms"`
	P95Ms    float64 `json:"p95_ms"` // aproximado por buckets (el límite superior del bucket)
}

// Snapshot devuelve las rutas medidas, de la más lenta (p95) a la más rápida.
func (r *Registry) Snapshot() []RouteSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RouteSnapshot, 0, len(r.routes))
	for k, st := range r.routes {
		out = append(out, RouteSnapshot{
			Method: k.method, Route: k.route, Count: st.total, Errors5x: st.byClass[5], Errors4x: st.byClass[4],
			AvgMs: math.Round(st.sumMs / float64(max(st.total, 1))), P95Ms: quantile(st.counts, st.total, 0.95),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].P95Ms != out[j].P95Ms {
			return out[i].P95Ms > out[j].P95Ms
		}
		return out[i].Route < out[j].Route
	})
	return out
}

// quantile: el límite superior del bucket donde cae el cuantil q (+Inf se informa como el último límite × 2).
func quantile(counts []uint64, total uint64, q float64) float64 {
	if total == 0 {
		return 0
	}
	want := uint64(math.Ceil(q * float64(total)))
	var acc uint64
	for i, c := range counts {
		acc += c
		if acc >= want {
			if i < len(Buckets) {
				return Buckets[i]
			}
			return Buckets[len(Buckets)-1] * 2
		}
	}
	return Buckets[len(Buckets)-1] * 2
}

// PoolSnapshot es el estado del pool de conexiones de la base.
type PoolSnapshot struct {
	Open         int     `json:"open"`
	InUse        int     `json:"in_use"`
	Idle         int     `json:"idle"`
	Max          int     `json:"max"`
	WaitCount    int64   `json:"wait_count"`
	WaitMs       float64 `json:"wait_ms"`
	UsagePercent float64 `json:"usage_percent"`
	// Conexiones cerradas por pasar el máximo de inactivas o por vejez: cada una se vuelve a abrir después (TLS y
	// autenticación con la base remota, varias idas y vueltas).
	IdleClosed     int64 `json:"idle_closed"`
	LifetimeClosed int64 `json:"lifetime_closed"`
}

func (r *Registry) Pool() *PoolSnapshot {
	if r.db == nil {
		return nil
	}
	s := r.db.Stats()
	p := &PoolSnapshot{Open: s.OpenConnections, InUse: s.InUse, Idle: s.Idle, Max: s.MaxOpenConnections, WaitCount: s.WaitCount, WaitMs: float64(s.WaitDuration.Milliseconds()),
		IdleClosed: s.MaxIdleClosed + s.MaxIdleTimeClosed, LifetimeClosed: s.MaxLifetimeClosed}
	if s.MaxOpenConnections > 0 {
		p.UsagePercent = math.Round(float64(s.InUse) * 100 / float64(s.MaxOpenConnections))
	}
	return p
}

// WritePrometheus escribe todo en el formato de texto de Prometheus.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	keys := make([]routeKey, 0, len(r.routes))
	for k := range r.routes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].route+keys[i].method < keys[j].route+keys[j].method })
	fmt.Fprintln(w, "# HELP empre_http_request_duration_ms Duración de las peticiones HTTP en milisegundos.")
	fmt.Fprintln(w, "# TYPE empre_http_request_duration_ms histogram")
	for _, k := range keys {
		st := r.routes[k]
		lbl := fmt.Sprintf(`method="%s",route="%s"`, k.method, esc(k.route))
		var acc uint64
		for i, b := range Buckets {
			acc += st.counts[i]
			fmt.Fprintf(w, "empre_http_request_duration_ms_bucket{%s,le=\"%g\"} %d\n", lbl, b, acc)
		}
		fmt.Fprintf(w, "empre_http_request_duration_ms_bucket{%s,le=\"+Inf\"} %d\n", lbl, st.total)
		fmt.Fprintf(w, "empre_http_request_duration_ms_sum{%s} %g\n", lbl, st.sumMs)
		fmt.Fprintf(w, "empre_http_request_duration_ms_count{%s} %d\n", lbl, st.total)
	}
	fmt.Fprintln(w, "# HELP empre_http_requests_total Peticiones por clase de código.")
	fmt.Fprintln(w, "# TYPE empre_http_requests_total counter")
	for _, k := range keys {
		st := r.routes[k]
		for c := 1; c <= 5; c++ {
			if st.byClass[c] > 0 {
				fmt.Fprintf(w, "empre_http_requests_total{method=\"%s\",route=\"%s\",class=\"%dxx\"} %d\n", k.method, esc(k.route), c, st.byClass[c])
			}
		}
	}
	r.mu.Unlock()

	if p := r.Pool(); p != nil {
		fmt.Fprintln(w, "# HELP empre_db_connections Conexiones del pool de la base por estado.")
		fmt.Fprintln(w, "# TYPE empre_db_connections gauge")
		fmt.Fprintf(w, "empre_db_connections{state=\"in_use\"} %d\nempre_db_connections{state=\"idle\"} %d\nempre_db_connections{state=\"max\"} %d\n", p.InUse, p.Idle, p.Max)
		fmt.Fprintln(w, "# TYPE empre_db_wait_total counter")
		fmt.Fprintf(w, "empre_db_wait_total %d\nempre_db_wait_ms_total %g\n", p.WaitCount, p.WaitMs)
		fmt.Fprintf(w, "empre_db_closed_total{reason=\"idle\"} %d\nempre_db_closed_total{reason=\"lifetime\"} %d\n", p.IdleClosed, p.LifetimeClosed)
	}
	var names []string
	r.counters.Range(func(k, _ any) bool { names = append(names, k.(string)); return true })
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Fprintln(w, "# HELP empre_events_total Contadores de negocio y de trabajos en segundo plano.")
		fmt.Fprintln(w, "# TYPE empre_events_total counter")
		for _, n := range names {
			v, _ := r.counters.Load(n)
			fmt.Fprintf(w, "empre_events_total{name=\"%s\"} %d\n", esc(n), v.(*atomic.Int64).Load())
		}
	}
	q, ms := DBTotals()
	fmt.Fprintln(w, "# HELP empre_db_queries_total Consultas a la base (y su tiempo total) desde que arrancó el proceso.")
	fmt.Fprintln(w, "# TYPE empre_db_queries_total counter")
	fmt.Fprintf(w, "empre_db_queries_total %d\nempre_db_query_ms_total %g\n", q, ms)
	fmt.Fprintln(w, "# TYPE empre_uptime_seconds gauge")
	fmt.Fprintf(w, "empre_uptime_seconds %d\n", int(time.Since(r.started).Seconds()))
}

func esc(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) }
