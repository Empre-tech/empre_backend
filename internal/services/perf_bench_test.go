package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"empre_backend/internal/models"
)

// Benchmarks de las funciones de memoria más pesadas (3.53): el armado de Ofertas (rutas, bento, vecinos, planes) y
// los totales de Finance, con 50, 200 y 1.000 candidatos. Sin base de datos:
//
//	go test -run XXX -bench . -benchmem ./internal/services/

var benchCats = []string{"Restaurantes", "Barberías", "Gimnasios", "Hoteles", "Cafeterías", "Tiendas de mascotas", "Bares", "Panaderías"}

func benchCands(n int, now time.Time) []MarketCandidate {
	zones := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	out := make([]MarketCandidate, 0, n)
	for i := 0; i < n; i++ {
		lat := 11.20 + float64(i%40)*0.002
		long := -74.23 + float64(i/40%40)*0.002
		z := zones[i%len(zones)]
		c := planCand(fmt.Sprintf("Oferta %d", i), benchCats[i%len(benchCats)], lat, long, &z, "06:00", "22:00", 10000+i%9*1000, 20000+i%7*2000, now)
		out = append(out, c)
	}
	return out
}

func BenchmarkMarketPieces(b *testing.B) {
	now := time.Date(2026, 10, 24, 15, 0, 0, 0, time.FixedZone("COT", -5*3600))
	for _, n := range []int{50, 200, 1000} {
		cands := benchCands(n, now)
		b.Run(fmt.Sprintf("BuildPlans/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				BuildPlans(cands, PlanOptions{HasLocation: true, Now: now})
			}
		})
		b.Run(fmt.Sprintf("BuildRoutes/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				BuildRoutes(cands, 4, true)
			}
		})
		b.Run(fmt.Sprintf("PickBento/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				PickBento(cands, nil, 5, true)
			}
		})
		b.Run(fmt.Sprintf("PickSpotlight/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				PickSpotlight(cands, now)
			}
		})
		b.Run(fmt.Sprintf("BuildNeighbors/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				BuildNeighbors(cands, true, now, 12)
			}
		})
	}
}

func BenchmarkFinanceTotals(b *testing.B) {
	loc := time.FixedZone("COT", -5*3600)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 1, 0)
	for _, n := range []int{50, 200, 1000, 5000} {
		sales := make([]models.Sale, n)
		exps := make([]models.Expense, n)
		for i := 0; i < n; i++ {
			at := from.Add(time.Duration(i%(30*24)) * time.Hour)
			sales[i] = models.Sale{Total: 1000 + i, Status: models.LedgerActive, OccurredAt: at}
			exps[i] = models.Expense{Amount: 500 + i, Status: models.LedgerActive, OccurredAt: at, Note: fmt.Sprintf("Gasto %d", i%15), Category: "otro"}
		}
		b.Run(fmt.Sprintf("DailyTotals/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				DailyTotals(sales, exps, from, to)
			}
		})
		b.Run(fmt.Sprintf("UsualExpenses/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				UsualExpenses(exps, 4)
			}
		})
	}
}
