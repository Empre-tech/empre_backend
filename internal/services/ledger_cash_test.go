package services

import (
	"testing"

	"empre_backend/internal/models"
)

func TestCashSpanSoloCuentaEfectivoActivo(t *testing.T) {
	f := cashSpan(
		[]models.Sale{
			{Method: models.PayCash, Status: models.LedgerActive, Total: 83000},
			{Method: models.PayTransfer, Status: models.LedgerActive, Total: 40000},
			{Method: models.PayCredit, Status: models.LedgerActive, Total: 10000},
			{Method: models.PayCash, Status: models.LedgerVoided, Total: 9000},
		},
		[]models.Expense{
			{Method: models.PayCash, Status: models.LedgerActive, Amount: 18000},
			{Method: models.PayCard, Status: models.LedgerActive, Amount: 5000},
			{Method: models.PayCash, Status: models.LedgerVoided, Amount: 7000},
		},
		[]models.DebtPayment{
			{Method: models.PayCash, Direction: models.DebtReceivable, Amount: 3000},
			{Method: models.PayCash, Direction: models.DebtPayable, Amount: 2000},
			{Method: models.PayTransfer, Direction: models.DebtReceivable, Amount: 8000},
		},
		[]models.CashMove{
			{Direction: models.CashOut, Status: models.LedgerActive, Amount: 20000},
			{Direction: models.CashIn, Status: models.LedgerActive, Amount: 10000},
			{Direction: models.CashOut, Status: models.LedgerVoided, Amount: 50000},
		},
	)
	if f.Sales != 83000 || f.SalesCount != 1 || f.Expenses != 18000 || f.ExpensesCount != 1 {
		t.Fatalf("ventas o gastos: %+v", f)
	}
	if f.DebtsIn != 3000 || f.DebtsOut != 2000 || f.MovesIn != 10000 || f.MovesOut != 20000 {
		t.Fatalf("abonos o movimientos: %+v", f)
	}
	if got, want := f.Net(), 83000+3000+10000-18000-2000-20000; got != want {
		t.Fatalf("net %d, quería %d", got, want)
	}
}

func TestCheckCashMove(t *testing.T) {
	ok := CashMoveInput{Direction: models.CashOut, Reason: "para_mi", Amount: 20000}
	if err := checkCashMove(ok); err != nil {
		t.Fatalf("debía pasar: %v", err)
	}
	for name, in := range map[string]CashMoveInput{
		"sin sentido":               {Direction: "x", Reason: "para_mi", Amount: 1},
		"motivo de la otra columna": {Direction: models.CashIn, Reason: "para_mi", Amount: 1},
		"un pago no es movimiento":  {Direction: models.CashOut, Reason: "le_pague", Amount: 1},
		"sin monto":                 {Direction: models.CashOut, Reason: "para_mi", Amount: 0},
	} {
		if checkCashMove(in) == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
}

func TestCheckCashClose(t *testing.T) {
	neg := -1
	if err := checkCashClose(CashCloseInput{Counted: 100, BaseNext: 100}); err != nil {
		t.Fatalf("debía pasar: %v", err)
	}
	if checkCashClose(CashCloseInput{Counted: 100, BaseNext: 101}) == nil {
		t.Error("la base no puede pasar lo contado")
	}
	if checkCashClose(CashCloseInput{Counted: -1}) == nil {
		t.Error("lo contado no puede ser negativo")
	}
	if checkCashClose(CashCloseInput{Counted: 1, Opening: &neg}) == nil {
		t.Error("la base de apertura no puede ser negativa")
	}
}

func TestCashDayStartNoAceptaElFuturo(t *testing.T) {
	if _, err := cashDayStart("2999-01-01"); err == nil {
		t.Error("debía rechazar el futuro")
	}
	if _, err := cashDayStart("ayer"); err == nil {
		t.Error("debía rechazar una fecha mala")
	}
	if _, err := cashDayStart(""); err != nil {
		t.Errorf("vacío es hoy: %v", err)
	}
}
