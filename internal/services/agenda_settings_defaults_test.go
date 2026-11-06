package services

import (
	"testing"

	"github.com/google/uuid"

	"empre_backend/internal/agenda"
	"empre_backend/internal/models"
)

// Un negocio sin agenda guardada debe recibir ajustes que se puedan guardar tal cual.
func TestDefaultSettingsCanBeSavedAsIs(t *testing.T) {
	st := defaultSettings(uuid.New())
	if st.IntakeMode != agenda.IntakeRules || st.QuietMode != agenda.QuietHours {
		t.Fatalf("modos vacíos: intake=%q quiet=%q", st.IntakeMode, st.QuietMode)
	}
	if st.ReviewNoShows != 2 || st.ReviewNoticeMin != 120 || st.NotifyEvery != 60 {
		t.Fatalf("avisos distintos a los de la tabla: %+v", st)
	}
	var saved models.AgendaSettings
	in := SettingsInput{IntakeMode: &st.IntakeMode, QuietMode: &st.QuietMode, NotifyEvery: &st.NotifyEvery}
	if err := applyIntake(&saved, in); err != nil {
		t.Fatalf("no se pudo guardar: %v", err)
	}
}
