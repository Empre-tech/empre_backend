// Prueba de carga de Empre (3.53): simula visitantes, dueños que registran en Finance y clientes que miran sus
// reclamos, en escalones de usuarios virtuales. Solo usa la biblioteca estándar. Mide latencia (p50/p95/p99) y errores
// por ruta y escalón, e imprime una tabla y un JSON.
//
// Uso (contra el backend local, con la base de desarrollo y una cuenta demo):
//
//	go run ./cmd/loadtest -steps 1,5,10,25,50 -dur 60s -out resultados.json
//
// Las ventas que registra se anulan enseguida: los totales del negocio no cambian.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sample struct {
	route string
	ms    float64
	code  int
}

type routeStats struct {
	Route  string         `json:"route"`
	Count  int            `json:"count"`
	Errors int            `json:"errors"`
	Codes  map[string]int `json:"codes"`
	P50    float64        `json:"p50_ms"`
	P95    float64        `json:"p95_ms"`
	P99    float64        `json:"p99_ms"`
	Max    float64        `json:"max_ms"`
}

type stepResult struct {
	Users  int          `json:"users"`
	Secs   float64      `json:"secs"`
	RPS    float64      `json:"rps"`
	Errors int          `json:"errors"`
	Total  int          `json:"total"`
	P95    float64      `json:"p95_ms"`
	Routes []routeStats `json:"routes"`
}

type runner struct {
	base     string
	token    string
	entity   string
	entities []string
	client   *http.Client
	mu       sync.Mutex
	samples  []sample
}

func main() {
	base := flag.String("base", "http://localhost:8080", "URL del backend")
	steps := flag.String("steps", "1,5,10,25,50", "usuarios virtuales por escalón")
	dur := flag.Duration("dur", 60*time.Second, "duración de cada escalón")
	email := flag.String("email", "demo01@empre.app", "cuenta demo del dueño")
	pass := flag.String("pass", "demo1234", "contraseña de la cuenta demo")
	out := flag.String("out", "", "archivo JSON con los resultados")
	think := flag.Duration("think", 500*time.Millisecond, "pausa media entre acciones de un usuario")
	flag.Parse()

	r := &runner{base: strings.TrimRight(*base, "/"), client: &http.Client{Timeout: 30 * time.Second}}
	if err := r.setup(*email, *pass); err != nil {
		fmt.Fprintln(os.Stderr, "preparación:", err)
		os.Exit(1)
	}
	fmt.Printf("Negocio del dueño: %s · fichas para visitar: %d\n", r.entity, len(r.entities))

	var results []stepResult
	for _, s := range strings.Split(*steps, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n <= 0 {
			continue
		}
		res := r.step(n, *dur, *think)
		results = append(results, res)
		printStep(res)
	}
	if *out != "" {
		b, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "no se pudo guardar:", err)
		}
	}
}

// setup inicia sesión con la cuenta demo, busca su negocio y unas fichas públicas para visitar.
func (r *runner) setup(email, pass string) error {
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if _, err := r.do("setup", "POST", "/api/auth/login", map[string]string{"email": email, "password": pass}, false, "", &login); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	r.token = login.AccessToken
	var mine struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if _, err := r.do("setup", "GET", "/api/entities/mine", nil, true, "", &mine); err != nil {
		return fmt.Errorf("mis negocios: %w", err)
	}
	switch {
	case len(mine.Items) > 0:
		r.entity = mine.Items[0].ID
	case len(mine.Data) > 0:
		r.entity = mine.Data[0].ID
	default:
		return fmt.Errorf("la cuenta no tiene negocios")
	}
	var disc struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_, _ = r.do("setup", "GET", "/api/discovery?"+bbox()+"&limit=30", nil, false, "", &disc)
	for _, e := range append(disc.Items, disc.Data...) {
		r.entities = append(r.entities, e.ID)
	}
	if len(r.entities) == 0 {
		r.entities = []string{r.entity}
	}
	return nil
}

func bbox() string {
	return "min_lat=11.15&max_lat=11.30&min_lng=-74.25&max_lng=-74.10&lat=11.2408&long=-74.2110"
}

var terms = []string{"corte", "cafe", "pizza", "barberia", "hotel", "gym"}

// step corre n usuarios durante d y resume lo medido.
func (r *runner) step(n int, d time.Duration, think time.Duration) stepResult {
	r.mu.Lock()
	r.samples = nil
	r.mu.Unlock()
	stop := time.Now().Add(d)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ip := fmt.Sprintf("10.%d.%d.%d", id%250, mrand.IntN(250), mrand.IntN(250)) // cada usuario, su IP (RateLimit es por IP)
			for time.Now().Before(stop) {
				r.act(ip)
				time.Sleep(time.Duration(float64(think) * (0.5 + mrand.Float64())))
			}
		}(i)
	}
	wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return summarize(n, d, r.samples)
}

// act elige qué hace un usuario: 60 % visita, 35 % dueño en Finance, 5 % mira sus reclamos.
func (r *runner) act(ip string) {
	p := mrand.Float64()
	switch {
	case p < 0.60:
		switch mrand.IntN(5) {
		case 0:
			_, _ = r.do("GET discovery/sections", "GET", "/api/discovery/sections?"+bbox(), nil, false, ip, nil)
		case 1:
			_, _ = r.do("GET discovery", "GET", "/api/discovery?"+bbox()+"&limit=15", nil, false, ip, nil)
		case 2:
			_, _ = r.do("GET search", "GET", "/api/search?q="+terms[mrand.IntN(len(terms))]+"&"+bbox(), nil, false, ip, nil)
		case 3:
			_, _ = r.do("GET offers/market", "GET", "/api/offers/market?lat=11.2408&long=-74.2110&city=Santa%20Marta", nil, false, ip, nil)
		default:
			if mrand.IntN(2) == 0 {
				_, _ = r.do("GET categories", "GET", "/api/categories", nil, false, ip, nil)
			} else {
				_, _ = r.do("GET entities/:id", "GET", "/api/entities/"+r.entities[mrand.IntN(len(r.entities))], nil, false, ip, nil)
			}
		}
	case p < 0.95:
		e := "/api/entities/" + r.entity + "/ledger"
		switch mrand.IntN(6) {
		case 0:
			_, _ = r.do("GET ledger/summary day", "GET", e+"/summary?period=day", nil, true, ip, nil)
		case 1:
			_, _ = r.do("GET ledger/summary week", "GET", e+"/summary?period=week", nil, true, ip, nil)
		case 2:
			_, _ = r.do("GET ledger/items", "GET", e+"/items", nil, true, ip, nil)
		case 3:
			_, _ = r.do("GET ledger/expenses/usual", "GET", e+"/expenses/usual", nil, true, ip, nil)
		case 4:
			_, _ = r.do("GET ledger/sales", "GET", e+"/sales?limit=100", nil, true, ip, nil)
		default:
			// Una venta de prueba y su anulación: los totales quedan iguales.
			id := uuid()
			sale := map[string]any{"client_id": id, "method": "cash", "total": 1000, "note": "prueba de carga", "lines": []map[string]any{{"name": "Prueba de carga", "qty": 1, "unit_price": 1000}}}
			if code, _ := r.do("POST ledger/sales", "POST", e+"/sales", sale, true, ip, nil); code < 300 {
				_, _ = r.do("POST ledger/sales/:id/void", "POST", e+"/sales/"+id+"/void", map[string]string{"reason": "prueba de carga"}, true, ip, nil)
			}
		}
	default:
		_, _ = r.do("GET claims/mine", "GET", "/api/claims/mine", nil, true, ip, nil)
	}
}

// do hace una petición, mide su tiempo y la guarda bajo el nombre de su ruta.
func (r *runner) do(route, method, path string, body any, auth bool, ip string, into any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, r.base+path, rd)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth && r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	start := time.Now()
	res, err := r.client.Do(req)
	ms := float64(time.Since(start).Microseconds()) / 1000
	code := 0
	if err == nil {
		code = res.StatusCode
		if into != nil && code < 300 {
			_ = json.NewDecoder(res.Body).Decode(into)
		} else {
			_, _ = io.Copy(io.Discard, res.Body)
		}
		res.Body.Close()
	}
	if route != "setup" {
		r.mu.Lock()
		r.samples = append(r.samples, sample{route, ms, code})
		r.mu.Unlock()
	}
	if err != nil {
		return 0, err
	}
	if code >= 300 && route == "setup" {
		return code, fmt.Errorf("%s %s respondió %d", method, path, code)
	}
	return code, nil
}

func summarize(users int, d time.Duration, samples []sample) stepResult {
	by := map[string][]sample{}
	for _, s := range samples {
		by[s.route] = append(by[s.route], s)
	}
	res := stepResult{Users: users, Secs: d.Seconds(), Total: len(samples)}
	var all []float64
	for route, list := range by {
		st := routeStats{Route: route, Count: len(list), Codes: map[string]int{}}
		ms := make([]float64, 0, len(list))
		for _, s := range list {
			ms = append(ms, s.ms)
			st.Codes[strconv.Itoa(s.code)]++
			if s.code == 0 || s.code >= 500 || s.code == 429 {
				st.Errors++
			}
		}
		sort.Float64s(ms)
		st.P50, st.P95, st.P99, st.Max = pct(ms, 50), pct(ms, 95), pct(ms, 99), ms[len(ms)-1]
		res.Errors += st.Errors
		all = append(all, ms...)
		res.Routes = append(res.Routes, st)
	}
	sort.Float64s(all)
	res.P95 = pct(all, 95)
	res.RPS = float64(len(samples)) / d.Seconds()
	sort.Slice(res.Routes, func(i, j int) bool { return res.Routes[i].P95 > res.Routes[j].P95 })
	return res
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

func printStep(s stepResult) {
	errPct := 0.0
	if s.Total > 0 {
		errPct = float64(s.Errors) * 100 / float64(s.Total)
	}
	fmt.Printf("\n== %d usuarios · %.0f s · %d peticiones · %.1f/s · p95 %.0f ms · errores %d (%.1f %%)\n", s.Users, s.Secs, s.Total, s.RPS, s.P95, s.Errors, errPct)
	fmt.Printf("%-30s %6s %7s %7s %7s %7s %6s  %s\n", "ruta", "n", "p50", "p95", "p99", "max", "err", "códigos")
	for _, r := range s.Routes {
		codes := make([]string, 0, len(r.Codes))
		for c, n := range r.Codes {
			codes = append(codes, fmt.Sprintf("%s×%d", c, n))
		}
		sort.Strings(codes)
		fmt.Printf("%-30s %6d %7.0f %7.0f %7.0f %7.0f %6d  %s\n", r.Route, r.Count, r.P50, r.P95, r.P99, r.Max, r.Errors, strings.Join(codes, " "))
	}
}

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
