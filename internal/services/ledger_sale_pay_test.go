package services

import (
	"testing"

	"github.com/google/uuid"

	"empre_backend/internal/models"
)

func TestSalePaysReparteElTotal(t *testing.T) {
	one := models.Sale{Method: models.PayCash, Total: 35000}.Pays()
	if len(one) != 1 || one[0].Amount != 35000 {
		t.Fatalf("una forma: %+v", one)
	}
	two := models.Sale{Method: models.PayCash, Total: 35000, Method2: models.PayCredit, Amount2: 15000}.Pays()
	if len(two) != 2 || two[0] != (models.SalePay{Method: models.PayCash, Amount: 20000}) || two[1] != (models.SalePay{Method: models.PayCredit, Amount: 15000}) {
		t.Fatalf("dos formas: %+v", two)
	}
}

func TestCheckSalePay(t *testing.T) {
	party := uuid.New()
	for name, in := range map[string]SaleInput{
		"una forma":      {Method: models.PayCash},
		"dos formas":     {Method: models.PayCash, Method2: models.PayTransfer, Amount2: 40},
		"pagó una parte": {Method: models.PayCash, Method2: models.PayCredit, Amount2: 40, PartyID: &party},
	} {
		if err := checkSalePay(in, 100); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, in := range map[string]SaleInput{
		"monto sin segunda forma": {Method: models.PayCash, Amount2: 40},
		"medio inválido":          {Method: models.PayCash, Method2: "x", Amount2: 40},
		"la misma forma":          {Method: models.PayCash, Method2: models.PayCash, Amount2: 40},
		"fiado primero":           {Method: models.PayCredit, Method2: models.PayCash, Amount2: 40, PartyID: &party},
		"sin segundo monto":       {Method: models.PayCash, Method2: models.PayCard},
		"segundo monto completo":  {Method: models.PayCash, Method2: models.PayCard, Amount2: 100},
		"debe sin cliente":        {Method: models.PayCash, Method2: models.PayCredit, Amount2: 40},
	} {
		if checkSalePay(in, 100) == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
}

func TestCashSpanCuentaSoloLaParteEnEfectivo(t *testing.T) {
	f := cashSpan([]models.Sale{
		{Method: models.PayCash, Status: models.LedgerActive, Total: 35000, Method2: models.PayCredit, Amount2: 15000},
		{Method: models.PayTransfer, Status: models.LedgerActive, Total: 10000, Method2: models.PayCash, Amount2: 4000},
		{Method: models.PayCash, Status: models.LedgerVoided, Total: 9000, Method2: models.PayCard, Amount2: 1000},
	}, nil, nil, nil)
	if f.Sales != 24000 || f.SalesCount != 2 {
		t.Fatalf("%+v", f)
	}
}

func TestTeQuedoSinFiado(t *testing.T) {
	if got := periodLeft(100_000, 30_000, 10_000, 25_000); got != 55_000 {
		t.Fatalf("te quedó: %d", got)
	}
	if got := SaleEntered(models.Sale{Method: models.PayCredit, Total: 16_000}); got != 0 {
		t.Fatalf("todo fiado: %d", got)
	}
	if got := SaleEntered(models.Sale{Method: models.PayCash, Total: 35_000, Method2: models.PayCredit, Amount2: 15_000}); got != 20_000 {
		t.Fatalf("pagó una parte: %d", got)
	}
	if got := SaleEntered(models.Sale{Method: models.PayCash, Total: 35_000, Method2: models.PayTransfer, Amount2: 15_000}); got != 35_000 {
		t.Fatalf("dos formas sin fiado: %d", got)
	}
}
