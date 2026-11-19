package services

import (
	"strings"
	"testing"
)

// El módulo solo suma una línea que desempata; sin módulo (o uno desconocido) el prompt queda igual que antes.
func TestVoiceModuleLine(t *testing.T) {
	if voiceModuleLine("") != "" || voiceModuleLine("search") != "" {
		t.Fatal("sin módulo conocido no debe sumar nada")
	}
	if l := voiceModuleLine(VoiceAtInventory); !strings.Contains(l, "Inventario") || !strings.Contains(l, "stock_counted") {
		t.Fatalf("inventario: %q", l)
	}
	if l := voiceModuleLine(VoiceAtFinance); !strings.Contains(l, "Finance") || !strings.Contains(l, "venta") {
		t.Fatalf("finance: %q", l)
	}
	if l := voiceModuleLine(VoiceAtDebts); !strings.Contains(l, "payment") {
		t.Fatalf("cuentas: %q", l)
	}
}
