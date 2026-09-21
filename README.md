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

Variables principales (ver `.env.example` para la lista completa):

| Variable | Descripción |
|---|---|
| `DB_HOST`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_PORT` | Postgres. Con Supabase usa el **Session pooler** (puerto 5432); el Transaction pooler (6543) no es compatible con los prepared statements. |
| `DB_SSLMODE` | `disable` (por defecto), `require` o `verify-full`. Con Supabase se recomienda `require`. |
| `JWT_SECRET` | Secreto largo y aleatorio (`openssl rand -hex 32`). |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_SESSION_TOKEN`, `S3_BUCKET`, `S3_REGION` | Almacenamiento de imágenes. El token de sesión solo aplica a credenciales temporales de AWS. |
| `APP_URL` | URL pública del backend. |

Al arrancar, el backend migra las tablas y, si la base está vacía, crea las categorías por defecto.

---

## 🚀 Instalación y Ejecución

### Opción A: Docker (recomendada, no requiere Go)
```bash
git clone https://github.com/Empre-tech/empre_backend.git
cd empre_backend
cp .env.example .env        # y complétalo
docker compose up --build
```
El API queda en `http://localhost:8080` (Swagger en `/api/swagger/index.html`, salud en `/health`).

### Opción B: Go local
```bash
go mod tidy
go run cmd/api/main.go
```

### Actualizar Documentación (opcional)
Si añades nuevos endpoints o cambias los comentarios de los handlers, regenera la doc con:
```bash
go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/api/main.go
```

---

## 📸 Sistema de Imágenes

Las imágenes se guardan en un bucket privado de S3 y la base de datos guarda su ruta. Al leer un negocio o un usuario,
el backend devuelve en `url` una **URL firmada de S3 que caduca a los 15 minutos**; para renovarla basta volver a pedir el recurso.
- `POST /api/entities/:id/images` (multipart: `file` + `type` = `profile` | `banner` | `gallery`, solo el dueño) y
  `POST /api/users/profile/image` (multipart: `file`).
- Formatos aceptados: JPEG, PNG y WebP.

---

## 💬 Módulo de Chat

El chat funciona mediante WebSockets en `/api/chat/ws`. 
- Autenticación: header `Authorization: Bearer JWT` (apps móviles) o, para clientes que no pueden enviar headers (navegador), `?token=JWT` en la URL.
- El cliente envía `{"entity_id", "user_id", "sent_by_entity", "content"}` (contenido de 1 a 1000 caracteres). El servidor valida que quien envía sea el cliente o el dueño del negocio, guarda el mensaje y lo devuelve como eco (con su `id` real) al remitente además de entregarlo al destinatario si está conectado.
- Cada frame del WebSocket lleva un único mensaje JSON (máx. 4096 bytes).
- `GET /api/chat/conversations` devuelve `entity_id` en cada conversación; `GET /api/chat/history/:entity_id` (con `?user_id=` cuando consulta el dueño) devuelve el historial.
- El historial se guarda automáticamente en la tabla `messages`.
