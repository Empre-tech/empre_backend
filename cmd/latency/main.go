// Prueba de latencia de Empre (3.54): por qué tarda en cargar cada respuesta. A diferencia de la prueba de carga, va
// de a una petición (sin tráfico a la vez) y separa cada respuesta en sus partes, gracias a Server-Timing:
//
//   - red: lo que el cliente esperó de más sobre lo que el servidor dice que tardó;
//   - base: el tiempo en consultas y cuántas hubo (cada una es una ida y vuelta a la base);
//   - servidor: el resto del tiempo del backend (cálculo, armado del JSON).
//
// Después abre cada pantalla como la app (sus peticiones a la vez) y dice cuál la frena.
//
//	go run ./cmd/latency -n 8 -out latencia.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptrace"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type endpoint struct {
	Name string
	Path string
	Auth bool
}

type sample struct {
	Total, TTFB, App, DB float64
	Queries              int
	Bytes                int
	Code                 int
}

type result struct {
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	N         int     `json:"n"`
	TotalP50  float64 `json:"total_p50_ms"`
	TotalP90  float64 `json:"total_p90_ms"`
	AppP50    float64 `json:"server_p50_ms"`
	DBP50     float64 `json:"db_p50_ms"`
	Queries   int     `json:"queries"`
	NetP50    float64 `json:"net_p50_ms"`
	CPUP50    float64 `json:"cpu_p50_ms"`
	KB        float64 `json:"kb"`
	Codes     string  `json:"codes"`
	Diagnosis string  `json:"diagnosis"`
}

type screen struct {
	Name  string
	Calls []string // nombres de endpoints
}

var timingRe = regexp.MustCompile(`app;dur=([\d.]+), db;dur=([\d.]+);desc="(\d+)"`)

func main() {
	base := flag.String("base", "http://localhost:8080", "URL del backend")
	n := flag.Int("n", 8, "muestras por endpoint")
	email := flag.String("email", "demo01@empre.app", "cuenta demo")
	pass := flag.String("pass", "demo1234", "contraseña de la cuenta demo")
	out := flag.String("out", "", "archivo JSON con los resultados")
	screensOnly := flag.Bool("screens", false, "solo las pantallas (sin la tabla por endpoint)")
	flag.Parse()
	c := &http.Client{Timeout: 60 * time.Second}
	b := strings.TrimRight(*base, "/")

	token, entity, other, err := setup(c, b, *email, *pass)
	if err != nil {
		fmt.Fprintln(os.Stderr, "preparación:", err)
		os.Exit(1)
	}
	bbox := "min_lat=11.15&max_lat=11.30&min_lng=-74.25&max_lng=-74.10&lat=11.2408&long=-74.2110"
	led := "/api/entities/" + entity + "/ledger"
	eps := []endpoint{
		{"usuario (users/me)", "/api/users/me", true},
		{"bandeja (inbox-summary)", "/api/users/me/inbox-summary", true},
		{"mis negocios", "/api/entities/mine", true},
		{"categorías", "/api/categories", false},
		{"zonas", "/api/zones", false},
		{"Descubre (sections)", "/api/discovery/sections?" + bbox, false},
		{"mapa (cells)", "/api/discovery/cells?" + bbox + "&lat_delta=0.15", false},
		{"lista (discovery)", "/api/discovery?" + bbox + "&limit=15", false},
		{"búsqueda", "/api/search?q=corte&" + bbox, false},
		{"Ofertas (market)", "/api/offers/market?lat=11.2408&long=-74.2110&city=Santa%20Marta", true},
		{"ahorro del mes", "/api/offers/savings", true},
		{"mis reclamos", "/api/claims/mine", true},
		{"ficha", "/api/entities/" + other, true},
		{"ficha: catálogo", "/api/entities/" + other + "/catalog", false},
		{"ficha: reseñas", "/api/entities/" + other + "/reviews", false},
		{"ficha: publicaciones", "/api/entities/" + other + "/posts", false},
		{"ficha: estado", "/api/entities/" + other + "/status", false},
		{"ficha: ofertas", "/api/entities/" + other + "/offers/active", false},
		{"Finance: hoy", led + "/summary?period=day", true},
		{"Finance: semana", led + "/summary?period=week", true},
		{"Mi negocio: mes", led + "/summary?period=month", true},
		{"Mi negocio: ventas", led + "/sales?limit=100", true},
		{"Mi negocio: gastos", led + "/expenses?limit=100", true},
		{"Finance: catálogo", led + "/items", true},
		{"Finance: gastos de siempre", led + "/expenses/usual", true},
		{"mensajes", "/api/chat/conversations", true},
	}
	byName := map[string]endpoint{}
	for _, e := range eps {
		byName[e.Name] = e
	}

	// Referencia: cuánto cuesta una ida y vuelta a la base (lo mide el propio backend en /ready).
	rtt := dbRTT(c, b)
	fmt.Printf("Ida y vuelta a la base (según /ready): %.0f ms\n\n", rtt)

	var results []result
	for _, e := range eps {
		if *screensOnly {
			break
		}
		_, _ = hit(c, b, e, token) // calentar (conexión, cachés en frío aparte)
		var ss []sample
		for i := 0; i < *n; i++ {
			s, err := hit(c, b, e, token)
			if err == nil {
				ss = append(ss, s)
			}
		}
		results = append(results, summarize(e, ss, rtt))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].TotalP50 > results[j].TotalP50 })
	fmt.Printf("%-28s %7s %7s %7s %7s %4s %6s %6s %7s  %s\n", "endpoint", "total", "p90", "server", "base", "cons", "cpu", "red", "KB", "diagnóstico")
	for _, r := range results {
		fmt.Printf("%-28s %7.0f %7.0f %7.0f %7.0f %4d %6.0f %6.0f %7.1f  %s\n", trunc(r.Name, 28), r.TotalP50, r.TotalP90, r.AppP50, r.DBP50, r.Queries, r.CPUP50, r.NetP50, r.KB, r.Diagnosis)
	}

	// Pantallas: sus peticiones a la vez, como la app. La pantalla está lista cuando llega la última.
	screens := []screen{
		{"Abrir la app", []string{"usuario (users/me)", "bandeja (inbox-summary)", "mis negocios", "categorías"}},
		{"Explorar", []string{"Descubre (sections)", "mapa (cells)", "lista (discovery)", "categorías", "zonas", "mis reclamos"}},
		{"Ofertas", []string{"Ofertas (market)", "ahorro del mes", "mis reclamos", "categorías"}},
		{"Ficha de negocio", []string{"ficha", "ficha: catálogo", "ficha: reseñas", "ficha: publicaciones", "ficha: estado", "ficha: ofertas"}},
		{"Empre Finance", []string{"mis negocios", "Finance: hoy", "Finance: semana", "Finance: catálogo", "Finance: gastos de siempre"}},
		{"Mi negocio (hoy)", []string{"Finance: hoy", "Mi negocio: ventas", "Mi negocio: gastos", "Finance: catálogo"}},
	}
	fmt.Printf("\n%-20s %8s %8s  %s\n", "pantalla", "lista p50", "p90", "la frena")
	type screenRes struct {
		Name    string  `json:"name"`
		P50     float64 `json:"ready_p50_ms"`
		P90     float64 `json:"ready_p90_ms"`
		Slowest string  `json:"slowest"`
	}
	var srs []screenRes
	for _, sc := range screens {
		var readies []float64
		slow := map[string]int{}
		for i := 0; i < 5; i++ {
			start := time.Now()
			var mu sync.Mutex
			var wg sync.WaitGroup
			worst, worstName := 0.0, ""
			for _, name := range sc.Calls {
				wg.Add(1)
				go func(e endpoint) {
					defer wg.Done()
					s, err := hit(c, b, e, token)
					if err != nil {
						return
					}
					mu.Lock()
					if s.Total > worst {
						worst, worstName = s.Total, e.Name
					}
					mu.Unlock()
				}(byName[name])
			}
			wg.Wait()
			readies = append(readies, float64(time.Since(start).Microseconds())/1000)
			slow[worstName]++
		}
		sort.Float64s(readies)
		top, topN := "", 0
		for k, v := range slow {
			if v > topN {
				top, topN = k, v
			}
		}
		sr := screenRes{sc.Name, pct(readies, 50), pct(readies, 90), fmt.Sprintf("%s (%d de 5)", top, topN)}
		srs = append(srs, sr)
		fmt.Printf("%-20s %8.0f %8.0f  %s\n", sr.Name, sr.P50, sr.P90, sr.Slowest)
	}

	if *out != "" {
		data, _ := json.MarshalIndent(map[string]any{"db_rtt_ms": rtt, "endpoints": results, "screens": srs}, "", "  ")
		_ = os.WriteFile(*out, data, 0o644)
	}
}

func setup(c *http.Client, b, email, pass string) (token, entity, other string, err error) {
	body := strings.NewReader(fmt.Sprintf(`{"email":%q,"password":%q}`, email, pass))
	res, err := c.Post(b+"/api/auth/login", "application/json", body)
	if err != nil {
		return
	}
	var login struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(res.Body).Decode(&login)
	res.Body.Close()
	token = login.AccessToken
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	get := func(path string, auth bool) {
		req, _ := http.NewRequest("GET", b+path, nil)
		if auth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if r, e := c.Do(req); e == nil {
			_ = json.NewDecoder(r.Body).Decode(&list)
			r.Body.Close()
		}
	}
	get("/api/entities/mine", true)
	if ids := append(list.Items, list.Data...); len(ids) > 0 {
		entity = ids[0].ID
	}
	list.Items, list.Data = nil, nil
	get("/api/discovery?min_lat=11.15&max_lat=11.30&min_lng=-74.25&max_lng=-74.10&lat=11.2408&long=-74.2110&limit=5", false)
	if ids := append(list.Items, list.Data...); len(ids) > 0 {
		other = ids[0].ID
	}
	if entity == "" || other == "" {
		err = fmt.Errorf("faltan negocios (dueño %q, ficha %q)", entity, other)
	}
	return
}

func dbRTT(c *http.Client, b string) float64 {
	var ms []float64
	for i := 0; i < 8; i++ {
		res, err := c.Get(b + "/ready")
		if err != nil {
			continue
		}
		var r struct {
			DBMs float64 `json:"db_ms"`
		}
		_ = json.NewDecoder(res.Body).Decode(&r)
		res.Body.Close()
		ms = append(ms, r.DBMs)
	}
	sort.Float64s(ms)
	return pct(ms, 50)
}

func hit(c *http.Client, b string, e endpoint, token string) (sample, error) {
	req, _ := http.NewRequest("GET", b+e.Path, nil)
	req.Header.Set("X-Timing", "1")
	req.Header.Set("Accept-Encoding", "identity")
	if e.Auth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	var first time.Time
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { first = time.Now() }}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	start := time.Now()
	res, err := c.Do(req)
	if err != nil {
		return sample{}, err
	}
	n, _ := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	s := sample{Total: ms(time.Since(start)), Bytes: int(n), Code: res.StatusCode}
	if !first.IsZero() {
		s.TTFB = ms(first.Sub(start))
	}
	if m := timingRe.FindStringSubmatch(res.Header.Get("Server-Timing")); m != nil {
		s.App, _ = strconv.ParseFloat(m[1], 64)
		s.DB, _ = strconv.ParseFloat(m[2], 64)
		s.Queries, _ = strconv.Atoi(m[3])
	}
	return s, nil
}

func summarize(e endpoint, ss []sample, rtt float64) result {
	r := result{Name: e.Name, Path: e.Path, N: len(ss)}
	if len(ss) == 0 {
		r.Diagnosis = "sin respuesta"
		return r
	}
	col := func(f func(sample) float64) []float64 {
		v := make([]float64, len(ss))
		for i, s := range ss {
			v[i] = f(s)
		}
		sort.Float64s(v)
		return v
	}
	tot := col(func(s sample) float64 { return s.Total })
	app := col(func(s sample) float64 { return s.App })
	db := col(func(s sample) float64 { return s.DB })
	net := col(func(s sample) float64 { return math.Max(0, s.Total-s.App) })
	cpu := col(func(s sample) float64 { return math.Max(0, s.App-s.DB) })
	q := col(func(s sample) float64 { return float64(s.Queries) })
	r.TotalP50, r.TotalP90 = pct(tot, 50), pct(tot, 90)
	r.AppP50, r.DBP50, r.NetP50, r.CPUP50 = pct(app, 50), pct(db, 50), pct(net, 50), pct(cpu, 50)
	r.Queries = int(pct(q, 50))
	r.KB = math.Round(float64(ss[len(ss)/2].Bytes)/102.4) / 10
	codes := map[int]int{}
	for _, s := range ss {
		codes[s.Code]++
	}
	var cs []string
	for k, v := range codes {
		cs = append(cs, fmt.Sprintf("%d×%d", k, v))
	}
	sort.Strings(cs)
	r.Codes = strings.Join(cs, " ")
	r.Diagnosis = diagnose(r, rtt)
	return r
}

// diagnose dice qué parte pesa más en una respuesta lenta.
func diagnose(r result, rtt float64) string {
	if r.TotalP50 < 300 {
		return "rápida"
	}
	parts := []string{}
	if r.AppP50 > 0 && r.DBP50/r.AppP50 >= 0.6 {
		seq := r.DBP50
		if rtt > 0 {
			seq = float64(r.Queries) * rtt
		}
		if r.Queries >= 4 && seq > r.DBP50*1.3 {
			parts = append(parts, fmt.Sprintf("base: %d consultas (parte en paralelo)", r.Queries))
		} else {
			parts = append(parts, fmt.Sprintf("base: %d consultas × ~%.0f ms", r.Queries, rtt))
		}
	}
	if r.CPUP50 >= 150 {
		parts = append(parts, fmt.Sprintf("cálculo del servidor %.0f ms", r.CPUP50))
	}
	if r.KB >= 150 {
		parts = append(parts, fmt.Sprintf("respuesta grande (%.0f KB)", r.KB))
	}
	if r.NetP50 >= 200 {
		parts = append(parts, fmt.Sprintf("red/transferencia %.0f ms", r.NetP50))
	}
	if len(parts) == 0 {
		return "repartido"
	}
	return strings.Join(parts, " · ")
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	return math.Round(sorted[i])
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
