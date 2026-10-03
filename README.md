# Empre Backend - Local Discovery App 🚀

Este es el backend oficial para la aplicación de descubrimiento de negocios locales. Construido con **Go (Golang)** y diseñado para ser altamente escalable, seguro y fácil de integrar con clientes Mobile.

## 🛠️ Tech Stack

- **Framework**: [Gin Gonic](https://gin-gonic.com/) (HTTP Web Framework)
- **Base de Datos**: PostgreSQL con [GORM](https://gorm.io/)
- **Almacenamiento**: AWS S3 (Imágenes seguras)
- **Documentación**: [Swaggo](https://github.com/swaggo/swag) (Swagger UI)
- **Real-time**: WebSockets

---

## 📖 Documentación de la API (Swagger)

Hemos implementado **Swagger UI** para que puedas probar la API interactivamente sin necesidad de configurar Postman manualmente.

### Cómo acceder:
1.  Inicia el servidor localmente.
2.  Abre en tu navegador: `http://localhost:8080/api/swagger/index.html`

### Cómo probar rutas protegidas:
1.  Usa el endpoint `POST /api/auth/login` para obtener tu JWT.
2.  Haz clic en el botón **"Authorize"** arriba a la derecha en Swagger.
3.  Ingresa: `Bearer TU_TOKEN_AQUÍ` y dale a Authorize.
4.  ¡Ya puedes usar el botón "Try it out" en cualquier endpoint!

---

## ⚙️ Configuración del Entorno (.env)

Copia la plantilla y rellénala (el `.env` real **nunca** se sube a Git):

```bash
cp .env.example .env        # en PowerShell: copy .env.example .env
```

> El equipo comparte un mismo proyecto de Supabase (y el bucket de S3), así que **pide las credenciales reales
> al equipo** en vez de inventarlas — con eso todos ven los mismos negocios/usuarios de prueba. Si prefieres una
> base de datos aislada solo para ti (sin tocar los datos compartidos), usa el Postgres local con Docker que se
> explica más abajo, en "¿No tienes una base de datos Postgres a mano?".

Variables principales (ver `.env.example` para la lista completa):

| Variable | Descripción |
|---|---|
| `DB_HOST`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_PORT` | Postgres. Con Supabase usa el **Session pooler** (puerto 5432); el Transaction pooler (6543) no es compatible con los prepared statements. |
| `DB_SSLMODE` | `disable` (por defecto), `require` o `verify-full`. Con Supabase se recomienda `require`. |
| `JWT_SECRET` | Secreto largo y aleatorio (`openssl rand -hex 32`). |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_SESSION_TOKEN`, `S3_BUCKET`, `S3_REGION` | Almacenamiento de imágenes. El token de sesión solo aplica a credenciales temporales de AWS. |
| `APP_URL` | URL pública del backend. |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_SENDER` | Opcional. Sin `SMTP_HOST`, los correos de recuperación de contraseña se imprimen en la consola en vez de enviarse. |

Al arrancar, el backend migra las tablas y, si la base está vacía, crea las categorías por defecto.

---

## 🚀 Instalación y Ejecución

> **Windows**: Docker Desktop requiere WSL2 (te lo pide al instalar si no lo tienes). Corre los comandos desde
> PowerShell o una terminal de WSL. Si vas a usar la Opción B (Go local), instala Go para Windows desde
> [go.dev/dl](https://go.dev/dl/) — el `go run` de abajo funciona igual en PowerShell.

### Opción A: Docker (recomendada, no requiere Go)
```bash
git clone https://github.com/Empre-tech/empre_backend.git
cd empre_backend
cp .env.example .env        # y complétalo
docker compose up --build
```
El API queda en `http://localhost:8080` (Swagger en `/api/swagger/index.html`, salud en `/health`).

> **¿No tienes una base de datos Postgres a mano?** Puedes levantar una local con Docker:
> ```bash
> docker run -d --name empre-postgres -p 5432:5432 \
>   -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=empre_db \
>   postgres:16
> ```
> y en tu `.env` deja `DB_HOST=localhost`, `DB_USER=postgres`, `DB_PASSWORD=postgres`, `DB_NAME=empre_db`, `DB_PORT=5432`, `DB_SSLMODE=disable` (son los valores por defecto). Con Docker Compose, el backend corre en un contenedor aparte, así que usa `DB_HOST=host.docker.internal` en vez de `localhost` para que alcance a este contenedor de Postgres.

### Opción B: Go local (requiere Go 1.24+)
```bash
go mod tidy
go run cmd/api/main.go
```

### Actualizar Documentación (opcional)
Si añades nuevos endpoints o cambias los comentarios de los handlers, regenera la doc con:
```bash
go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/api/main.go
```

### ⚠️ Utilidades para reiniciar la base de datos (destructivas)

Como el equipo comparte una sola base de datos (Supabase), **estos comandos borran los datos de todos**, no solo
los tuyos — úsalos solo si el equipo está de acuerdo, nunca por probar. Requieren Go local (`go run ...`; no hay
equivalente en Docker) y usan el mismo `.env` que `cmd/api`, así que apuntan a la base que tengas configurada ahí.

- `go run ./cmd/resetall` — borra todos los negocios (con todo lo que cuelga de ellos: chats, reseñas, favoritos,
  fotos/publicaciones, horarios, suscripciones) y todas las cuentas que no sean admin. Pide confirmación
  (escribir "si") antes de borrar. Las categorías/admins sobreviven; las categorías por defecto se recrean solas
  la próxima vez que arranque `cmd/api`.
- `go run ./cmd/resetcatalog` — borra negocios, categorías y subcategorías, y recrea el catálogo de categorías
  por defecto. También pide confirmación.

---

## 📸 Sistema de Imágenes

Las imágenes se guardan en un bucket privado de S3 y la base de datos guarda su ruta. Al leer un negocio o un usuario,
el backend devuelve en `url` una **URL firmada de S3 que caduca a los 15 minutos**; para renovarla basta volver a pedir el recurso.
- `POST /api/entities/:id/images` (multipart: `file` + `type` = `profile` | `banner` | `gallery`, solo el dueño;
  `gallery` acepta además un `caption` opcional en el mismo multipart) y `POST /api/users/profile/image` (multipart: `file`).
- Formatos aceptados: `profile`/`banner` solo JPEG, PNG y WebP. `gallery` (las "publicaciones") además acepta video
  corto (MP4, MOV o WebM), hasta 60 MB. La respuesta y `GET /api/entities/:id` devuelven `content_type` para cada
  foto/video de la galería, con el que el frontend decide si mostrar un reproductor o una imagen.

---

## 💬 Módulo de Chat

El chat funciona mediante WebSockets en `/api/chat/ws`. 
- Autenticación: header `Authorization: Bearer JWT` (apps móviles) o, para clientes que no pueden enviar headers (navegador), `?token=JWT` en la URL.
- El cliente envía `{"entity_id", "user_id", "sent_by_entity", "content"}` (contenido de 1 a 1000 caracteres). El servidor valida que quien envía sea el cliente o el dueño del negocio, guarda el mensaje y lo devuelve como eco (con su `id` real) al remitente además de entregarlo al destinatario si está conectado.
- Cada frame del WebSocket lleva un único mensaje JSON (máx. 4096 bytes).
- `GET /api/chat/conversations` devuelve `entity_id` en cada conversación; `GET /api/chat/history/:entity_id` (con `?user_id=` cuando consulta el dueño) devuelve el historial.
- El historial se guarda automáticamente en la tabla `messages`.

---

## 🔔 Notificaciones push

Usamos el servicio de push de Expo (no hace falta ninguna credencial: es gratis y no requiere variables de entorno).

- `POST /api/users/push-token` — registra (o reasigna, si el token ya existía con otra cuenta) el token de este
  dispositivo. Body: `{"token": "ExponentPushToken[...]"}`.
- `DELETE /api/users/push-token` — quita el token (la app lo llama al cerrar sesión).

El backend manda una notificación en estos casos:
- **Chat**: al guardar un mensaje, si el destinatario no está conectado por WebSocket en ese momento.
- **Reseña nueva**: al dueño del negocio, cuando alguien deja o actualiza una reseña.
- **Favorito nuevo**: al dueño del negocio, cuando alguien lo agrega a favoritos.
- **Verificación**: al dueño, cuando el admin marca su negocio como verificado o rechazado (`pending` no notifica).

Todo pasa por `PushService.Notify`, que es *best-effort*: si Expo no responde o el token ya no es válido, se
registra en el log y sigue de largo — nunca hace fallar la acción que disparó la notificación (guardar un mensaje,
una reseña, etc.). Un token con error `DeviceNotRegistered` se borra automáticamente.

> La app (`empre_app`) necesita un **development build** para recibir estas notificaciones: Expo Go ya no las
> soporta. Ver la sección de notificaciones en el README de la app.

---

## 🕐 Horarios de atención y modalidad de servicio

Cada negocio puede configurar, por día de la semana, si abre, si está cerrado o si atiende las 24 horas, y también
declarar cómo presta el servicio.

- `Entity.service_mode`: `in_place` (en el lugar), `delivery` (a domicilio), `both` (ambos), o vacío ("sin especificar").
- Tabla `business_hours`: una fila por negocio y día (`weekday` sigue la numeración de Go `time.Weekday`, 0=domingo
  ... 6=sábado, igual a `Date.getDay()` en JS). Cada fila tiene `closed`, `is_24h`, `open_time`/`close_time`
  (formato `"HH:MM"`). Si `close_time <= open_time` se interpreta como un horario que cruza la medianoche (ej. un bar
  de 18:00 a 02:00).
- La hora "actual" para calcular si un negocio está abierto usa un offset fijo UTC-5 (hora de Colombia, igual en todo el país), sin
  depender de que el contenedor tenga tzdata instalado.
- `POST /api/entities` y `PUT /api/entities/:id` aceptan `service_mode` y `hours: [{weekday, closed, is_24h,
  open_time, close_time}, ...]`.
- `GET /api/entities/:id` y la respuesta de creación/edición devuelven `service_mode`, `hours` e `is_open_now`.
- `GET /api/entities` (mapa/lista) devuelve `service_mode`, `is_open_now` y `has_hours` (para poder distinguir
  "cerrado ahora" de "este negocio no configuró horario") por cada negocio, y acepta `?open_now=true` para filtrar
  solo los que están abiertos en este momento (el filtro se aplica en SQL, no después de paginar, para no romper la
  paginación).

---

## 🤖 Asistente de IA para crear negocios

`POST /api/ai/business-assistant` (requiere sesión) ayuda al dueño a armar su negocio charlando en vez de llenar el
formulario a mano. El frontend manda toda la conversación en cada llamada (este endpoint no guarda estado entre
peticiones):

```json
{ "messages": [{ "role": "user", "content": "vendo arepas y jugos naturales cerca del mercado" }] }
```

Y devuelve:

```json
{ "reply": "¡Qué rico! ¿Cómo se llama tu negocio, o quieres que te sugiera unos nombres?", "draft": { ... }, "ready": false }
```

- Usa [OpenCode Zen](https://opencode.ai/zen), un gateway que da acceso a varios modelos (Claude, GPT, Gemini...)
  con una sola API key, a través de su endpoint compatible con la Anthropic Messages API (`/v1/messages`),
  incluyendo *tool use* para sacar datos estructurados de una charla libre.
- Variables de entorno: `AI_API_KEY` (sin ella, el endpoint responde 503 y el resto de la app sigue normal),
  `AI_BASE_URL` (default `https://opencode.ai/zen/v1`), `AI_MODEL` (default `claude-haiku-4-5`).
- El modelo recibe el catálogo real de categorías/subcategorías en el system prompt y solo puede usar esos IDs;
  el backend además revalida cada `category_id`/`subcategory_id` que el modelo proponga contra la base de datos
  antes de devolverlo — un ID inventado se descarta en silencio en vez de llegar al frontend.
- El `draft` (`name_suggestions`, `description`, `category_id`, `subcategory_ids`, `service_mode`, `hours`) es solo
  una sugerencia: la IA nunca crea el negocio directamente. El frontend siempre lo lleva al mismo formulario de
  creación de siempre para que el dueño revise y edite antes de guardar.

### Redactar textos con IA (descripciones y captions)

`POST /api/ai/writing-assistant` (requiere sesión) es más simple que el asistente conversacional: una sola llamada,
sin historial, para pedir 2-3 opciones de texto listas para usar.

```json
{ "kind": "business_description", "current_text": "vendo arepas", "business_name": "Arepas de la Plaza", "category_name": "Restaurantes" }
```

```json
{ "suggestions": ["Arepas rellenas hechas al momento...", "..."] }
```

- `kind`: `business_description` (para el perfil del negocio) o `post_caption` (para el texto de una publicación,
  más corto). `current_text`, `business_name` y `category_name` son opcionales — dan mejor contexto si se mandan.
- Usa el mismo `AIService`/API key que el asistente conversacional; si `AI_API_KEY` no está configurada, responde 503.
- Igual que el asistente conversacional, el dueño siempre elige una opción a mano: nada se guarda solo.

## Suscripción de negocios (pagos con Wompi)

Cada negocio (`Entity`) tiene una fila en `subscriptions` (`inactive` | `active` + `current_period_end`).
Cada intento de pago queda registrado en `subscription_payments` (auditoría: referencia, monto,
estado, id de la transacción en Wompi).

Endpoints (todos requieren sesión y ser el dueño del negocio, excepto el webhook):

- `GET /api/entities/:id/subscription` — estado actual del plan.
- `POST /api/entities/:id/subscription/checkout` — crea un intento de pago (`PENDING`) y devuelve los
  datos firmados que necesita el Widget Web Checkout de Wompi: `public_key`, `reference`,
  `amount_in_cents`, `currency`, `signature`, `redirect_url`.
- `POST /api/payments/wompi/webhook` — público (lo llama Wompi, no un usuario). Verifica la firma del
  evento con `WOMPI_EVENTS_SECRET` antes de aplicar nada. En `transaction.updated` con status
  `APPROVED`, activa/extiende la suscripción 30 días desde el vencimiento actual (o desde hoy si
  estaba vencida).

Variables de entorno (ver `.env.example`): `WOMPI_PUBLIC_KEY`, `WOMPI_INTEGRITY_SECRET`,
`WOMPI_EVENTS_SECRET`, `WOMPI_REDIRECT_URL`. Sin `WOMPI_PUBLIC_KEY`/`WOMPI_INTEGRITY_SECRET`, el
checkout responde 503 y el resto de la app sigue funcionando igual.

El precio del plan (`ProMonthlyPriceCOP`, hoy $39.900 COP/mes) y el nombre del plan
(`PlanProMonthly`) están hardcodeados en `internal/services/subscription_service.go`; si mañana hay
más de un plan, eso pasa a ser una tabla en vez de constantes.

### Prueba gratis por demanda real, no por fecha

En vez de un trial de N días, el negocio es gratis hasta que le escriban `FreeTrialCustomerThreshold`
(hoy 10) **clientes distintos** — mensajes repetidos del mismo cliente no cuentan más de una vez. La
idea: no tiene sentido pedirle a un negocio que pague por destacar si todavía no le está llegando
demanda real; el que paga es el que ya está recibiendo contactos.

- `ChatRepository.CountDistinctCustomers` cuenta `user_id` distintos que le han escrito a un negocio.
- `GET /api/entities/:id/subscription` devuelve `{ subscription, distinct_customers, trial_threshold,
  requires_payment }` — el frontend usa esto para mostrar una barra de progreso mientras
  `requires_payment` es `false`, y el llamado a pagar cuando se vuelve `true`.
- Cuando un mensaje de un cliente hace que un negocio cruce el umbral (`websocket/hub.go`, tras
  guardar el mensaje), `SubscriptionService.CheckTrialThreshold` le manda al dueño **una sola vez**
  una notificación push ("Ya te están contactando en Empre") — `Subscription.TrialThresholdNotifiedAt`
  evita que se repita en cada mensaje nuevo.
- **Sí hay una consecuencia real** (si no, nadie pagaría): pasado un margen de gracia de
  `TrialGracePeriodDays` (7 días) desde ese aviso, si sigue sin pagar, `EntityRepository.FindAll`
  (el mapa y las búsquedas) deja de devolver ese negocio — `NOT EXISTS` contra `subscriptions` en la
  misma query, sin tocar `FindByID`. Esto es deliberadamente parcial:
  - El negocio deja de aparecer para clientes NUEVOS (mapa/búsqueda), que es el costo real de no pagar.
  - Las conversaciones que YA tenía siguen abiertas — un cliente que ya le escribió lo puede seguir
    viendo y hablándole (no se le rompe nada a una relación que ya existía).
  - Nunca se borra el negocio ni se bloquea por completo: apenas paga, `ActivateSubscription` lo
    reactiva (y limpia `TrialThresholdNotifiedAt`, así que si el plan vuelve a vencer más adelante
    recibe un aviso y un margen de gracia nuevos, no el de esta vez).
  - El número de días vive en dos lugares que hay que mantener sincronizados a mano:
    `services.TrialGracePeriodDays` y el `INTERVAL '7 days'` hardcodeado en `entity_repository.go`
    (no hay una sola fuente de verdad entre la constante de Go y el SQL crudo).

Pendiente/decisiones futuras:
- Qué desbloquea exactamente el plan (hoy es solo el estado "Pro"; falta decidir y aplicar el gate,
  ej. destacar el negocio en el mapa/búsquedas).
- Renovación hoy es manual (el dueño vuelve a pagar); Wompi permite recurrencia con tarjetas
  tokenizadas pero requiere un flujo de tokenización PCI que no se implementó en este MVP.
- No hay cron que avise antes de que venza; se podría enviar un push unos días antes usando
  `PushService` + `Repo.FindTokensByUser` sobre el dueño.
