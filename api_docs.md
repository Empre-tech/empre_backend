# 📱 API Documentation - Empre Backend

Esta documentación está diseñada para el equipo de desarrollo Mobile. El servidor corre por defecto en `http://localhost:8080`. Todas las rutas protegidas requieren el header: `Authorization: Bearer <token>`.

---

## 1. Autenticación 🔑

### Registro de Usuario
**Endpoint**: `POST /api/auth/register`
**Body**:
```json
{
  "email": "user@example.com",
  "password": "password123",
  "name": "Juan Perez"
}
```

### Login
**Endpoint**: `POST /api/auth/login`
**Response**:
```json
{
  "token": "eyJhbGciOiJIUzI1..."
}
```

---

## 2. Negocios (Entities) 🏬

### Listar/Buscar Negocios
**Endpoint**: `GET /api/entities?lat=6.24&long=-75.58&radius=5000&category=<uuid>`
**Query Params**: `lat`, `long` (opcionales para geo), `radius` (metros), `category` (UUID opcional).
**Response (List)**:
```json
[
  {
    "id": "uuid-negocio",
    "name": "Cafe Sol",
    "description": "El mejor café",
    "banner_url": "/api/images/uuid-banner",
    "profile_url": "/api/images/uuid-profile",
    "latitude": 6.244,
    "longitude": -75.589,
    "category": { "id": "...", "name": "Comida" },
    "photos": []
  }
]
```

### Detalle de un Negocio (Incluye Galería)
**Endpoint**: `GET /api/entities/:id`
**Response**:
```json
{
  "id": "uuid-negocio",
  "name": "Cafe Sol",
  "photos": [
    {
      "id": "uuid-relacion",
      "media_id": "uuid-imagen",
      "order": 0,
      "media": {
        "id": "uuid-imagen",
        "url": "/api/images/uuid-imagen",
        "content_type": "image/jpeg"
      }
    }
  ]
}
```

---

## 3. Imágenes y S3 📸

### Gestión Directa por Negocio (Recomendado)
Sube una imagen y la asocia automáticamente al negocio.
**Endpoint**: `POST /api/entities/:id/images`
**Headers**: `Content-Type: multipart/form-data`
**Body**:
- `file`: (Archivo binario)
- `type`: `profile` | `banner` | `gallery`

### Consumo de Imágenes
**Endpoint**: `GET /api/images/:id`
Simplemente pon el ID que recibes en las URLs anteriores.

---

## 4. Chat y WebSockets 💬

### Conexión en tiempo real
**URL**: `ws://localhost:8080/api/chat/ws?token=<JWT_TOKEN>`

### Listar mis conversaciones
**Endpoint**: `GET /api/chat/conversations`
**Response**:
```json
[
  {
    "id": "uuid-mensaje",
    "entity_id": "uuid-negocio",
    "user_id": "uuid-usuario",
    "content": "Hola, una pregunta...",
    "created_at": "2024-01-27T..."
  }
]
```

### Historial con un negocio
**Endpoint**: `GET /api/chat/history/:entity_id`
**Response**:
```json
[
  {
    "id": "...",
    "sender_type": "customer",
    "content": "Hola!",
    "created_at": "..."
  },
  {
    "id": "...",
    "sender_type": "owner",
    "content": "En qué puedo ayudarte?",
    "created_at": "..."
  }
]
```
