package services

import (
	"testing"

	"empre_backend/internal/models"
)

func TestOfferSaleAmount(t *testing.T) {
	special := 25000
	cases := []struct {
		name  string
		offer models.Offer
		want  int
	}{
		{"precio especial manda", models.Offer{SpecialPrice: &special, Price: "$30.000"}, 25000},
		{"con puntos", models.Offer{Price: "$25.000"}, 25000},
		{"solo cifras", models.Offer{Price: "14000"}, 14000},
		{"porcentaje", models.Offer{Price: "15%"}, 0},
		{"dos por uno", models.Offer{Price: "2x1"}, 0},
		{"vacío", models.Offer{}, 0},
	}
	for _, c := range cases {
		if got := offerSaleAmount(&c.offer); got != c.want {
			t.Errorf("%s: %d, quería %d", c.name, got, c.want)
		}
	}
}
