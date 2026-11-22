package services

import (
	"encoding/json"
	"strings"
	"testing"
)

// promptModel guarda el mensaje de sistema que recibió y responde lo que se le diga.
type promptModel struct {
	reply  string
	system string
}

func (p *promptModel) Enabled() bool { return true }
func (p *promptModel) ChatTool(system string, _ []AIMessage, _ AITool) (*AIReply, error) {
	p.system = system
	return &AIReply{ToolCalls: []AIToolCall{{Name: voiceToolName, Input: json.RawMessage(p.reply)}}}, nil
}

// Una pregunta en la hoja de dictado: Empre AI recibe los números del negocio y la respuesta vuelve en «answer».
func TestVoiceAnswersQuestions(t *testing.T) {
	_, svc, owner, _, ent := ledgerFixture(t)
	pm := &promptModel{reply: `{"actions":[],"answer":"Esta semana vendiste $0: todavía no hay ventas anotadas."}`}
	svc.SetVoiceModel(pm)

	res, err := svc.ParseVoiceAt(owner.ID, ent.ID, "¿cuánto vendí esta semana?", nil, VoiceAtFinance)
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer == "" || len(res.Actions) != 0 {
		t.Fatalf("respuesta = %+v", res)
	}
	for _, want := range []string{"DATOS DEL NEGOCIO", "Hoy: ventas", "Esta semana: ventas", "Le deben al negocio"} {
		if !strings.Contains(pm.system, want) {
			t.Fatalf("el prompt no trae %q", want)
		}
	}

	// En Inventario, los datos son lo que se acaba.
	if _, err := svc.ParseVoiceAt(owner.ID, ent.ID, "¿qué se está acabando?", nil, VoiceAtInventory); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.system, "Inventario") && !strings.Contains(pm.system, "Agotados") {
		t.Fatalf("el prompt de inventario no trae el inventario")
	}
}
