# Pruebas de rendimiento de Empre (3.53)

Fecha: 25 oct 2026. Entorno: backend local (Go + Gin) contra la base de **desarrollo** remota (Supabase, detrás del pooler pgbouncer, pool de 15 conexiones). App: dev client en el emulador Pixel 7 Pro (x86_64, GPU por software). Cuenta demo local `demo01`.

Herramientas, todas en el repositorio y sin instalar nada:
- `empre_backend/cmd/loadtest`: prueba de carga en escalones.
- `internal/services/perf_bench_test.go`: benchmarks.
- `cmd/zz_dbprobe`: sonda temporal de la base, de solo lectura.
- `npx expo export`: el bundle de producción.

## 1. Prueba de carga del backend (antes de las mejoras)
Mezcla de tráfico:
- **60 % visitantes:** discovery, sections, search, market, categorías y ficha.
- **35 % dueños en Finance:** resumen del día y de la semana, items, gastos de siempre, ventas, y venta + anulación.
- **5 % reclamos.**

Cada usuario hace una pausa media de 0,5 s entre acciones; cada escalón dura 60 s.

| Usuarios | Peticiones/s | p95 global | Errores | La ruta más lenta (p95) |
|---:|---:|---:|---:|---|
| 1 | 0,7 | 2,2 s | 0 % | `offers/market` 3,6 s |
| 5 | 3,5 | 1,9 s | 0 % | `offers/market` 2,4 s |
| 10 | 4,7 | 4,7 s | 0 % | `ledger/summary week` 19,9 s (pico) |
| 25 | 12,9 | 3,2 s | 0 % | `offers/market` 10,4 s |
| 50 | **14,6** | **7,0 s** | 0 % | `offers/market` 11,2 s |

**Cómo leerlo:**
- Entre 25 y 50 usuarios el rendimiento **se satura en unas 15 peticiones/s**. Duplicar los usuarios casi no sube el caudal, pero sí duplica la latencia.
- Coincide con el pool: 15 conexiones × ~1 s por petición ≈ 15/s.
- No hay errores 5xx: el sistema se pone lento, no se cae.

Con 1 usuario, la mediana (p50) por ruta:

| Ruta | Mediana (p50) |
|---|---:|
| market | 2,7 s |
| summary | 1,1 s |
| discovery | 1,0–1,1 s |
| ficha | 1,0 s |
| crear venta | 0,96 s |
| categorías | 0,48 s |
| items | 0,33 s |
| reclamos | 0,30 s |
| gastos de siempre | 0,29 s |

## 2. La base: el costo es la ida y vuelta, no el SQL
- **Ida y vuelta a la base (`SELECT 1`, 30 veces):** p50 **101 ms**, p95 116 ms y mínimo 83 ms.
- **`EXPLAIN ANALYZE` de las consultas calientes:** **0,04 a 2,9 ms** de ejecución (ventas y gastos del periodo, saldos de fiados, abonos, categorías, subcategorías, reclamos recientes y ofertas activas).
- **Por eso, la latencia de una petición ≈ número de consultas × 100 ms.** El resumen de Finance hace unas 11 consultas seguidas, lo que da ~1,1 s. Las categorías hacen 3 consultas por datos que casi no cambian.
- **Índices:** están los que hacen falta (`sales` y `expenses` tienen `(entity_id, occurred_at)`). Con 18 filas el planificador prefiere el de `status`, y eso es normal en una tabla tan pequeña. Una primera lectura, con la lista cortada, sugirió que faltaba: se revisó y no falta.
- **`pg_stat_statements`:** lo que más tiempo suma son las consultas de arranque. `AutoMigrate` lee `information_schema` unas 2.500 veces, y el pooler hace `DISCARD ALL` (47 ms de media) y `get_auth`. Las consultas de la app ni aparecen: son baratas.

## 3. Benchmarks en memoria (sin base)
| Función | 50 candidatos | 200 candidatos |
|---|---:|---:|
| `BuildPlans` (rutas temáticas) | **199 ms · 35,7 MB/llamada** | **1,95 s · 414 MB/llamada** |
| `BuildRoutes` | 124 ms · 23,7 MB | 1,92 s · 365 MB |
| `PickSpotlight` | 9,6 ms · 0,9 MB | 3,9 ms · 3,6 MB |
| `PickBento` | 0,36 ms | — |
| `BuildNeighbors` | 0,11 ms | — |

**Finance:** `DailyTotals` tarda 2,1 ms con 1.000 registros y 10,4 ms con 5.000; `UsualExpenses`, 14,9 ms con 5.000. Crecen en línea recta: están bien.

**Causa, según el perfil de CPU y memoria:** el **97 % de la memoria** y la mayor parte de la CPU se van en `FoldText` (`golang.org/x/text`, `transform.Chain`). Se llama a través de `kindOf` → `ExploreGroupOf` **dentro de los bucles que comparan paradas de a pares**, y recalcula miles de veces el grupo de la misma categoría.

Con 200 ofertas activas en una ciudad, cada `GET /offers/market` costaría ~4 s de CPU y ~800 MB de memoria; con unos pocos usuarios a la vez, el servidor se queda sin memoria.

## 4. La app
- **Bundle de producción** (`expo export`, Android):
  - **6,7 MB de bytecode Hermes** (2.392 módulos).
  - **7,9 MB de assets, casi todo fuentes `.ttf`**: la app solo usa Ionicons (unos 390 KB), pero `import { Ionicons } from '@expo/vector-icons'` mete todas las familias de íconos, así que sobran ~7,5 MB.
- **Arranque en frío y fluidez:** **no son válidos en este emulador.** El dev client baja el bundle de Metro sin minificar, salió entre 12 y 55 s (con «la app no responde») y la GPU es por software (`ro.hardware.egl=emulation`). Hay que medirlos en un teléfono real con un build de release (ver P1-5).
- **Memoria:** 156 MB PSS y 283 MB RSS en el dev client (Java heap 22 MB y nativo 13 MB). Es razonable para una app con mapa.

## 5. Cuellos de botella y recomendaciones (por impacto)
| Prioridad | Hallazgo | Recomendación |
|---|---|---|
| **P0** | `BuildPlans` y `BuildRoutes` crecen peor que en cuadrado: 1,9 s y ~400 MB por llamada con 200 ofertas | Calcular el grupo de cada categoría una sola vez, con caché por nombre, y no llamar `FoldText` dentro de los bucles. Volver a medir. |
| **P1** | La latencia es el número de consultas × 100 ms, y el resumen de Finance hace ~11 seguidas | Hacer en paralelo las consultas independientes del resumen (ventas, gastos, abonos, saldos y periodo anterior). Juntar los tres saldos en una sola consulta. |
| **P1** | Las categorías hacen 3 viajes por datos casi fijos | Caché en memoria de 5 min, invalidada al editar, más `Cache-Control` y `ETag`. |
| **P1** | Saturación en ~15 peticiones/s por el pool de 15 | Bajar las consultas por petición (lo de arriba) y, en producción, poner el backend en la **misma región** que la base (la ida y vuelta pasaría de 100 ms a 1–2 ms). Revisar el modo del pooler (transaction) y el tamaño del pool. |
| **P1** | Sobran ~7,5 MB de fuentes en el bundle | Importar `@expo/vector-icons/Ionicons` en lugar del barril. |
| **P1-5** | Sin medición real del arranque ni de la fluidez | Medir en el teléfono con un build de release: `am start -W` y `dumpsys gfxinfo`. Después lo cubre la métrica `app_start` (Parte 3). |
| **P2** | El servidor no tiene timeouts ni apagado ordenado | `http.Server` con timeouts y `Shutdown`. |
| **P2** | La app no tiene timeout ni `ErrorBoundary` | `AbortController` de 15 s en `request()` y un `ErrorBoundary` raíz. |
| **P3** | `AutoMigrate` lee `information_schema` miles de veces en cada arranque | Migrar solo con una opción (`MIGRATE=1`) o en el despliegue, no en cada inicio. |

## 6. Después de las mejoras (misma prueba, misma base)
**Qué se cambió en el backend:**
- **`FoldText`** reutiliza su cadena de transformación, y el grupo de cada categoría queda en caché.
- **En paralelo**, hasta 4 a la vez:
  - las consultas independientes del resumen de Finance (11 seguidas);
  - las de los saldos (3);
  - las de Ofertas: base, señales, términos, servicios y voces.
- **Las categorías**, en una caché de 5 minutos que se vacía al editar.
- **`http.Server`** con `ReadHeaderTimeout` e `IdleTimeout`, y apagado ordenado.

**Benchmarks en memoria:**
| Función | Antes | Después |
|---|---:|---:|
| `BuildPlans`, 50 ofertas | 199 ms · 35,7 MB | **3,2 ms · 1,2 MB** (62×) |
| `BuildPlans`, 200 ofertas | 1.951 ms · 414 MB | **20 ms · 4,9 MB** (96×) |
| `BuildRoutes`, 200 ofertas | 1.920 ms · 365 MB | **10 ms · 1,2 MB** (186×) |
| `BuildPlans`, 1.000 ofertas | (no terminaba en minutos) | 293 ms · 25 MB |

**Carga, mediana (p50) y p95 por ruta:**
| Ruta | 1 usuario: antes → después | 25 usuarios: antes → después | 50 usuarios: antes → después |
|---|---|---|---|
| `offers/market` | 2.709 → **1.802** ms | p50 3.069 → **2.320** · p95 10.396 → **4.288** | p50 6.500 → **4.758** · p95 11.182 → **7.594** |
| `ledger/summary week` | 1.073 → 1.204 ms | p50 1.793 → **760** · p95 3.512 → 3.882 | p50 3.828 → **1.596** · p95 6.982 → **2.909** |
| `ledger/summary day` | 1.142 → **886** ms | p50 1.771 → **781** · p95 3.420 → **2.344** | p50 3.694 → **1.561** · p95 6.544 → **2.742** |
| `categories` | 482 → 359 ms (primera vez) | p50 560 → **0** · p95 871 → **6** | p50 1.147 → **0** · p95 2.225 → **2** |
| **Total** | 0,7 → 0,7 pet./s | 12,9 → 13,7 pet./s · p95 3,2 → 3,1 s | **14,6 → 15,8 pet./s** · p95 **7,0 → 6,2 s** |

Errores: 0 % antes y después.

**Cómo leerlo:**
- Las rutas que se tocaron mejoran entre 30 % y 60 % con carga, y las categorías pasan a ~0 ms.
- El **techo global casi no cambia (~15–16 pet./s)**. Paralelizar no reduce el número total de consultas, y el pool sigue siendo de 15 conexiones con 100 ms de ida y vuelta.
- `discovery`, que no se tocó, empeora un poco con 50 usuarios: ahora compite por el pool con las consultas en paralelo.
- **Lo que sube el techo de verdad es infraestructura (P1-4):** el backend en la misma región que la base, con 1–2 ms de ida y vuelta en vez de 100 ms. Aparte de eso, ayuda menos consultas por petición (más caché, juntar consultas).
- **Nota:** la red a la base varía de una corrida a otra (con 1 usuario hay ±20 %). Hay que comparar con carga, no con un solo usuario.

**App (bundle de producción):**
| | Antes | Después |
|---|---:|---:|
| Bytecode Hermes | 6,7 MB · 2.392 módulos | **6,4 MB · 2.342 módulos** |
| Fuentes `.ttf` | 7,86 MB | **2,24 MB** (−71 %) |

**Qué cambió en la app:**
- **Ionicons** se importa por su ruta, en 102 archivos.
- **Nunito y Manrope**, peso por peso: antes entraban los 18 pesos de cada familia.
- **Queda fuera de esta mejora:** `material-symbols`, de 967 KB, que lo trae la dependencia `expo-symbols` (P3: ver si se usa y quitarlo).

## 7. Latencia: por qué «a veces demora en cargar» (3.54)
**Herramienta:** `empre_backend/cmd/latency`. Va de una petición a la vez y separa cada respuesta en sus partes:
- **red:** lo que el cliente esperó de más sobre lo que el servidor dice que tardó;
- **base:** cuántas consultas hizo y cuánto tardaron;
- **servidor:** el cálculo.

Lo hace con la cabecera estándar `Server-Timing`, que el backend pone cuando se pide con `X-Timing: 1`. Después abre cada pantalla como la app, con sus peticiones a la vez, y dice cuál la frena. En `/metrics` quedaron tres contadores nuevos: `empre_db_queries_total`, `empre_db_query_ms_total` y `empre_db_closed_total`.

### 7.1 Cada endpoint, de a uno (ida y vuelta a la base: 101 ms)
| Endpoint | Total | Servidor | Base | Consultas | Cálculo | Red | Tamaño |
|---|---:|---:|---:|---:|---:|---:|---:|
| Ofertas (market) | 1.784 ms | 1.783 | 4.127* | **19** | 0 | 3 | 9 KB |
| búsqueda | 1.195 | 1.194 | 1.423* | **11** | 0 | 3 | 2 KB |
| ficha | 1.158 | 1.157 | 1.455* | **7** | 0 | 2 | 1 KB |
| lista (discovery) | 1.109 | 1.099 | 1.519* | **10** | 0 | 7 | 9 KB |
| Descubre (sections) | 1.078 | 1.074 | 1.402* | **9** | 0 | 2 | 5 KB |
| Finance hoy, semana y mes | 877–947 | ~930 | ~1.850* | **11** | 0 | 1–4 | 0,5–2 KB |
| ficha: reseñas | 503 | 500 | 500 | 5 | 1 | 1 | 0,1 KB |
| mis negocios | 441 | 440 | 665 | 4 | 0 | 4 | 0,3 KB |
| las demás | 97–331 | | | 1–3 | 0 | ≤ 6 | |
| categorías | 1 | 0 | 0 | 0 (caché) | 0 | 1 | 2 KB |

\* La base suma más que el total porque esas consultas ya van en paralelo (3.53).

**Conclusión 1:** en todas las respuestas lentas el 100 % del tiempo es base. El cálculo es 0 ms, la red de 1 a 7 ms y las respuestas pesan menos de 10 KB. **Una respuesta tarda lo que suman sus consultas, a ~100 ms cada una.** No es la red del teléfono ni el tamaño de los datos.

### 7.2 Las pantallas, como las abre la app (5 veces cada una)
| Pantalla | p50 | p90 | La frena |
|---|---:|---:|---|
| Abrir la app | 557 ms | 898 ms | mis negocios |
| Explorar | 1.551 | 2.229 | lista (discovery) |
| Ofertas | 1.205 | 1.418 | market |
| Ficha de negocio | 2.364 | **7.928** | ficha |
| Empre Finance | 2.646 | **15.967** | resumen de la semana |
| Mi negocio (hoy) | 1.640 | 2.400 | resumen de hoy |

**Conclusión 2, el «a veces»:** cada pantalla sola tarda entre 1 y 2,6 s, pero **1 de cada 10 veces** la ficha tarda 8 s y Finance 16 s.

**La causa:**
- Cuando una pantalla pide 5 o 6 cosas a la vez, el pool abre conexiones nuevas hasta 15. Al soltarlas, **cierra todas las que pasan de 5 inactivas** (`DB_MAX_IDLE_CONNS=5`).
- La próxima ráfaga las vuelve a abrir. Cada apertura con la base remota es TLS más autenticación en el pooler, varias idas y vueltas: por eso `pgbouncer.get_auth` y `DISCARD ALL` encabezaban `pg_stat_statements`.
- El pool de Go nunca tuvo que esperar (`wait_total 0`): el tiempo se va en abrir conexiones, no en esperar una libre.

### 7.3 La prueba: 5 contra 15 conexiones inactivas (solo con variable de entorno)
| Pantalla | p50 · p90 con 5 inactivas | p50 · p90 con 15 inactivas |
|---|---|---|
| Empre Finance | 2.167 · **13.029 ms** | **907 · 2.891 ms** |
| Mi negocio (hoy) | 2.236 · 6.412 | **730 · 893** |
| Ficha de negocio | 2.555 · 5.496 | **1.651 · 1.806** |
| Abrir la app | 807 · 1.305 | 743 · 1.522 |
| Explorar | 2.163 · 2.474 | 2.160 · 2.981 |
| Ofertas | 2.393 · 2.585 | 2.364 · 2.745 |
| Conexiones cerradas y reabiertas | **64** en 30 aperturas | **0** |
| Tiempo medio por consulta | 332 ms | 257 ms |

Explorar y Ofertas no cambian: ahí lo que pesa son **las consultas** (market 19, discovery 10, sections 9 y búsqueda 11), no las conexiones.

### 7.4 Qué hacer, en orden
| Prioridad | Qué | Efecto esperado |
|---|---|---|
| **P0** (ya probado) | `DB_MAX_IDLE_CONNS` igual a `DB_MAX_OPEN_CONNS` (15) como valor por omisión, más `SetConnMaxIdleTime` de unos minutos para soltar las que sobran de noche | Se acaban las cargas de 6 a 16 s: p90 de Finance 13 → 2,9 s y de Mi negocio 6,4 → 0,9 s |
| **P1** | Abrir unas conexiones al arrancar el backend (precalentar el pool) | La primera pantalla después de un despliegue no paga la apertura |
| **P1** | Menos consultas en Explorar y Ofertas: juntar las precargas (`Preload`) en un solo JOIN, cachear zonas y resúmenes de reseñas por 1 o 2 min y unir las 3 de `ficha` que se pueden unir | discovery, sections y market de ~1,1–1,8 s a ~0,5 s |
| **P1** | El backend en la misma región que la base | Cada consulta de 100 ms a 1–2 ms: todo lo anterior se divide por 50 |
| **P2** | La app: mostrar lo que ya está en caché mientras recarga, y esqueletos en vez de pantallas en blanco | Tapa el tiempo que quede; no lo quita |

### 7.5 Aplicado: pool con 15 inactivas, tiempo máximo sin uso y precalentado (3.54)
**El cambio:**
- `DefaultMaxIdleConns` pasa de 5 a 15 (igual a las abiertas).
- `SetConnMaxIdleTime` de 15 min (`DB_MAX_IDLE_MINUTES`): las que nadie usa se sueltan solas.
- Al arrancar se abren las 15 a la vez, antes de recibir tráfico (`DB_WARM_CONNS`; en el log: «Pool de la base: 15 conexiones listas en 1884 ms»).

**La misma prueba completa, comparada con la primera corrida (§7.2):**
| Pantalla | p50 · p90 antes | p50 · p90 después |
|---|---|---|
| Empre Finance | 2.646 · **15.967 ms** | **770 · 878 ms** |
| Mi negocio (hoy) | 1.640 · 2.400 | **639 · 650** |
| Ficha de negocio | 2.364 · **7.928** | **1.429 · 1.606** |
| Abrir la app | 557 · 898 | 696 · 1.166 |
| Ofertas | 1.205 · 1.418 | 1.647 · 2.132 |
| Explorar | 1.551 · 2.229 | 2.315 · 2.903 |

**Endpoints de Finance, de a uno:**
| Endpoint | Antes | Después |
|---|---:|---:|
| hoy | 877 ms | **529 ms** |
| semana | 936 ms | **455 ms** |
| mes | 947 ms | **346 ms** |

Con las conexiones ya abiertas, sus consultas en paralelo de verdad van a la vez.

**Lo que dicen los contadores:**
- conexiones cerradas y reabiertas: 0;
- tiempo medio por consulta: de 232 a 182 ms (la ida y vuelta es de 96 ms).

**Lo que falta:**
- **Lo que se arregló:** las cargas largas de 1 de cada 10 veces (6 a 16 s) desaparecieron. El p90 de Finance bajó 18 veces.
- **Explorar y Ofertas:** se mueven dentro de la variación entre corridas (en las pruebas A y B de §7.3 daban ~2,2 s las dos). Su causa es otra: **las consultas** (market 19, discovery 10, sections 9, búsqueda 11). Es el siguiente paso (P1 de §7.4).
