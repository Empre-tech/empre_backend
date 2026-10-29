# Observabilidad y monitoreo de Empre (3.53)

Todo está en el código y no usa servicios externos. Se puede conectar después a Prometheus + Grafana sin cambiar nada.

## Qué hay

### Backend (`empre_backend`)
| Dónde | Qué da |
|---|---|
| **Logs** (salida estándar, una línea JSON por evento) | En cada petición: `request_id`, `method`, `route` (la plantilla, como `/api/entities/:id`, nunca el id real), `status`, `ms`, `bytes` y `user` (un hash, nunca el id). Los 5xx salen como `ERROR`, las de más de 1 s como `WARN` y los pánicos con su pila. El SQL lento lo escribe GORM, sin colores, con el umbral de `DB_SLOW_MS` (200 ms por omisión). |
| **`X-Request-ID`** | Si la app manda la cabecera, el backend la usa; si no, crea una. Siempre la devuelve. Con ella se sigue una petición de punta a punta en los logs. Los 500 la incluyen en su cuerpo. |
| **`GET /health`** | El proceso vive. No toca la base. Es para el balanceador. |
| **`GET /ready`** | Hace ping a la base (con un límite de 2 s) y devuelve el estado del pool. Si la base no responde, da **503**: no hay que mandarle tráfico. |
| **`GET /metrics`** | Formato Prometheus:<br>• `empre_http_request_duration_ms` (histograma por ruta y método);<br>• `empre_http_requests_total` (por clase de código);<br>• `empre_db_connections{state}`, `empre_db_wait_total` y `empre_db_wait_ms_total` (el pool);<br>• `empre_events_total{name}` (pánicos, errores de trabajos como `job_error_claim_expiry`);<br>• `empre_uptime_seconds`.<br>Con `METRICS_TOKEN` exige `Authorization: Bearer <token>`; sin él, solo responde desde la misma máquina. |
| **`GET /api/admin/health`** (solo admin) | El resumen para una persona:<br>• las 15 rutas más lentas desde que arrancó el proceso (p95 aproximado, promedio, 4xx y 5xx);<br>• el pool;<br>• lo que la app reportó en 24 h: rutas lentas o fallidas, errores de pantalla y arranque (p50 y p95). |
| **`/debug/pprof`** | Perfiles de CPU, memoria y goroutines. Solo se monta con `ENABLE_PPROF=1` y sigue las mismas reglas de acceso que `/metrics`. |

### App (`empre_app`)
Va por la medición anónima de uso que ya existía (`/api/events`) y respeta el interruptor «Medición anónima de uso».
- **`api_health`:** una petición sin respuesta (estado 0: sin señal o pasaron los 15 s del límite nuevo), con error 5xx o que tardó más de 3 s. Lleva la ruta como plantilla, el código y los ms. Como mucho un reporte por ruta por minuto. `/api/events` no se mide.
- **`app_start`:** ms desde que empieza a correr el código de la app hasta la primera pantalla (fuentes listas, splash fuera). No incluye el arranque nativo.
- **`js_error`:** errores que rompen una pantalla (`ErrorBoundary`, que ahora muestra «Algo falló en esta pantalla · Reintentar») y los que no atrapa nadie (`ErrorUtils`). Solo el mensaje, sin correos ni cifras largas, y dónde pasó.

## Cómo mirarlo
```bash
curl -s http://localhost:8080/ready
curl -s http://localhost:8080/metrics | grep -E "duration_ms_count|db_connections"
# Una petición de punta a punta:
curl -s -H "X-Request-ID: soporte-0001" http://localhost:8080/api/categories -o /dev/null
# …y en los logs:
grep '"request_id":"soporte-0001"' backend.log
```

## Alertas sugeridas (cuando haya Prometheus)
| Señal | Umbral | Qué suele querer decir |
|---|---|---|
| p95 de una ruta (`histogram_quantile(0.95, …duration_ms_bucket)`) | > 1.000 ms durante 5 min | Muchas consultas por petición o la base lejos (ver PRUEBAS_RENDIMIENTO.md §2) |
| 5xx / total | > 1 % durante 5 min | Un error nuevo: buscar el `request_id` en los logs |
| `empre_db_connections{state="in_use"}` / `max` | > 80 % | El pool se está llenando: las peticiones van a esperar |
| `rate(empre_db_wait_total[5m])` | > 0 sostenido | Ya hay peticiones esperando conexión |
| `/ready` | ≠ 200 | Sin base |
| `js_error` (en `/api/admin/health`) | > 0,5 % de las instalaciones del día | Una pantalla rota en una versión publicada |
| `api_health` con `offline` alto en una ruta | — | Timeouts: esa ruta tarda más de 15 s |
| `empre_events_total{name=~"job_error.*"}` | > 0 | Un trabajo en segundo plano falla (los reclamos vencidos no se liberan) |

## Conectarlo a Prometheus + Grafana (opcional, sin cambiar código)
1. Poner `METRICS_TOKEN` en el backend.
2. En `prometheus.yml`:
   ```yaml
   scrape_configs:
     - job_name: empre
       metrics_path: /metrics
       authorization: { credentials: "<METRICS_TOKEN>" }
       static_configs: [{ targets: ["api.empre.app"] }]
   ```
3. Grafana: un panel por cada fila de la tabla de alertas.
4. Los logs JSON se pueden mandar a Loki (o a cualquier colector) tal como salen.
