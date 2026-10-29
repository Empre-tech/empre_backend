package observability

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Tiempo de base por petición (3.54). Cada consulta de GORM suma a dos contadores del proceso: cuántas hubo y cuánto
// tardaron. Así se ve en /metrics cuánto pesa la base, y la prueba de latencia puede separar una respuesta lenta en
// «consultas × ida y vuelta» y «cálculo del servidor».

var (
	dbQueries atomic.Int64
	dbNanos   atomic.Int64
)

const startKey = "obs:start"

// InstrumentGorm agrega a GORM el conteo y el tiempo de cada consulta (crear, leer, actualizar, borrar y SQL crudo).
func InstrumentGorm(db *gorm.DB) error {
	cb := db.Callback()
	start := func(tx *gorm.DB) { tx.InstanceSet(startKey, time.Now()) }
	end := func(tx *gorm.DB) {
		if v, ok := tx.InstanceGet(startKey); ok {
			if t, ok := v.(time.Time); ok {
				dbQueries.Add(1)
				dbNanos.Add(int64(time.Since(t)))
			}
		}
	}
	regs := []struct {
		before func(string, func(*gorm.DB)) error
		after  func(string, func(*gorm.DB)) error
	}{
		{cb.Create().Before("*").Register, cb.Create().After("*").Register},
		{cb.Query().Before("*").Register, cb.Query().After("*").Register},
		{cb.Update().Before("*").Register, cb.Update().After("*").Register},
		{cb.Delete().Before("*").Register, cb.Delete().After("*").Register},
		{cb.Row().Before("*").Register, cb.Row().After("*").Register},
		{cb.Raw().Before("*").Register, cb.Raw().After("*").Register},
	}
	for _, r := range regs {
		if err := r.before("obs:before", start); err != nil {
			return err
		}
		if err := r.after("obs:after", end); err != nil {
			return err
		}
	}
	return nil
}

// DBTotals: consultas y milisegundos de base acumulados desde que arrancó el proceso.
func DBTotals() (int64, float64) {
	return dbQueries.Load(), float64(dbNanos.Load()) / 1e6
}

// HeaderTiming: si la petición trae «X-Timing: 1», la respuesta lleva Server-Timing con el tiempo del servidor, el de
// base y cuántas consultas hizo. Las cuentas son del proceso entero: son exactas cuando las peticiones van de una en
// una (la prueba de latencia) y aproximadas con tráfico a la vez, por eso no se ponen en todas las respuestas.
const HeaderTiming = "X-Timing"

// timingWriter escribe Server-Timing justo antes de la primera escritura (después ya no se pueden cambiar cabeceras).
type timingWriter struct {
	gin.ResponseWriter
	start   time.Time
	q0      int64
	ms0     float64
	written bool
}

func (w *timingWriter) stamp() {
	if w.written {
		return
	}
	w.written = true
	q, ms := DBTotals()
	app := float64(time.Since(w.start).Microseconds()) / 1000
	w.Header().Set("Server-Timing", fmt.Sprintf(`app;dur=%.1f, db;dur=%.1f;desc="%d"`, app, ms-w.ms0, q-w.q0))
}

func (w *timingWriter) WriteHeader(code int) { w.stamp(); w.ResponseWriter.WriteHeader(code) }
func (w *timingWriter) WriteHeaderNow()      { w.stamp(); w.ResponseWriter.WriteHeaderNow() }
func (w *timingWriter) Write(b []byte) (int, error) {
	w.stamp()
	return w.ResponseWriter.Write(b)
}
func (w *timingWriter) WriteString(s string) (int, error) {
	w.stamp()
	return w.ResponseWriter.WriteString(s)
}

// Timing envuelve la respuesta para agregar Server-Timing cuando se pide.
func Timing() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader(HeaderTiming) != "1" {
			c.Next()
			return
		}
		q, ms := DBTotals()
		c.Writer = &timingWriter{ResponseWriter: c.Writer, start: time.Now(), q0: q, ms0: ms}
		c.Next()
	}
}
