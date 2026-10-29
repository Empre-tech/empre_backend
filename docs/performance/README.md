# Rendimiento de Empre: por qué estaba lenta

> [!IMPORTANT]
> **La causa principal no era Postgres ni el código, sino los viajes.** Cada petición hacía varias consultas **una detrás de otra**, y cada una iba y volvía desde el backend hasta la base de datos en Ohio (AWS `us-east-2`): **~80–100 ms por viaje**.
>
> **latencia ≈ consultas en serie × ida y vuelta + CPU**
>
> Postgres tardaba **menos de 1–3 ms** por petición y el backend, pocos milisegundos de CPU. El resto era esperar la red: con 6 consultas, ~500 ms; con 11, más de 1 s.

Esto está **confirmado con causa**: en `GET /api/entities` se cambió solo el número de consultas, sin tocar la red, y la latencia se movió exactamente como predecía la cuenta.

| Consultas por petición | p50 con 1 a la vez | Previsto por la cuenta | Techo con 15 conexiones |
|---|---|---|---|
| 6 (antes) | **530 ms** | 528 ms | 25,5 pet./s |
| 4 | **334 ms** | 333 ms | 41,5 pet./s |
| 2 (ahora) | **170 ms** | 185 ms | **78,4 pet./s** (×3,1) |

La respuesta es idéntica en las tres versiones, verificada con una prueba de integración y contra la base de desarrollo.

## Las tres causas, por orden de impacto

### 1. Consultas en serie × distancia a la base
- **Qué pasaba:** cada consulta espera la anterior. Con la base a ~80–100 ms, cada consulta de más suma ese tiempo. El resumen de Finance hacía ~11 consultas seguidas (~1,1 s); Ofertas, 19; Descubre, 10; las secciones, 9; la búsqueda, 11.
- **También limita la capacidad:** una petición ocupa una conexión del pool en cada consulta. Con 15 conexiones para todo el proyecto (el pooler de Supabase en modo sesión), el techo es **15 ÷ (consultas × ida y vuelta)**. Con carga, cada petición vuelve a hacer fila antes de cada consulta, y por eso la latencia crecía tan rápido.
- **Lo que se hizo:**
  - el listado de negocios pasó de 6 a 2 consultas: quitar lo que la respuesta no usa y juntar total y reseñas en la consulta de la página;
  - las consultas independientes de Finance y Ofertas ahora van en paralelo (3.53).
- **Lo que falta:** reducir consultas en Ofertas (19), Descubre (10), secciones (9) y búsqueda (11), con el mismo método: prueba de respuesta idéntica, medición antes y después.

### 2. El pool cerraba y reabría conexiones: el «a veces tarda mucho»
- **Qué pasaba:** cuando una pantalla pedía 5 o 6 cosas a la vez, el pool abría conexiones hasta 15 y, al soltarlas, cerraba todas las que pasaban de 5 inactivas. La siguiente pantalla pagaba otra vez la apertura (TCP + TLS + pooler). **1 de cada 10 veces** la ficha tardaba 8 s y Finance, 16 s.
- **Lo que se hizo (3.54):** 15 conexiones inactivas, un tiempo máximo sin uso y el pool precalentado al arrancar. Las cargas largas desaparecieron y **el p90 de Finance bajó 18 veces**.

### 3. Cálculo en memoria de las rutas de Ofertas
- **Qué pasaba:** al armar las rutas del barrio se normalizaba texto una y otra vez (`FoldText`). Eso se llevaba el 97 % de la memoria: `BuildPlans` con 200 ofertas tardaba 1,95 s y usaba 414 MB por llamada.
- **Lo que se hizo (3.53):** normalizar una vez y reutilizar. Ahora tarda 20 ms y usa 4,9 MB (de **62× a 186× más rápido** según la función).

## Qué recomendamos ahora
1. **Backend en la misma región que la base** (`us-east-2`). Es lo que más mueve: de ~80–100 ms a 1–2 ms por consulta, sin tocar código. El workflow de AWS ya usa esa región; falta confirmar dónde está la instancia EC2 de producción.
2. **Menos consultas en serie** en los endpoints que quedan, de a uno, con prueba de respuesta idéntica y medición antes y después.
3. **No subir el pool sin más:** el modo sesión del pooler limita a 15 clientes para todo el proyecto. Más conexiones en un backend se las quita a los demás.

## Documentos
| Documento | Qué tiene |
|---|---|
| [`benchmark-practice-01.md`](benchmark-practice-01.md) | Benchmark de `GET /api/entities` de 1 a 60 a la vez: percentiles, throughput, CPU, memoria, consultas, ida y vuelta, `pg_stat_activity`, espera del pool segundo a segundo, hipótesis H1–H6 y la reducción de 6 a 2 consultas (sección 12). |
| [`PRUEBAS_RENDIMIENTO.md`](PRUEBAS_RENDIMIENTO.md) | Prueba de carga por escenarios (3.53), benchmarks en memoria, la app, y la latencia por pantalla con el pool (3.54). |
| [`OBSERVABILIDAD.md`](OBSERVABILIDAD.md) | Cómo leer `/metrics`, `/ready` y `Server-Timing`, y los umbrales de alerta. |

**Herramientas en el repo:**
- `cmd/loadtest`: prueba de carga por escenarios;
- `cmd/latency`: latencia por endpoint y por pantalla;
- `internal/handlers/entity_list_integration_test.go`: fija la respuesta y el número de consultas del listado.

**Límites de las mediciones:** todo se midió desde una PC en Colombia contra la base de desarrollo, con datos demo. En producción la ida y vuelta depende de la región del backend.
