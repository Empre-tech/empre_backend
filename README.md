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

Crea un archivo `.env` en la raíz del proyecto y configura las siguientes variables:

```env
PORT=8080
DB_HOST=localhost
DB_USER=postgres
DB_PASSWORD=tu_password
DB_NAME=empre_db
DB_PORT=5432
JWT_SECRET=tu_secreto_super_seguro

# AWS S3 Configuration
S3_ACCESS_KEY=TU_ACCESS_KEY
S3_SECRET_KEY=TU_SECRET_KEY
S3_SESSION_TOKEN=TU_SESSION_TOKEN (Solo si usas credenciales temporales de AWS)
S3_BUCKET=nombre-de-tu-bucket
S3_REGION=us-east-1
```

---

## 🚀 Instalación y Ejecución

### 1. Clonar y descargar dependencias
```bash
git clone <url-del-repo>
cd empre_backend
go mod tidy
```

### 2. Ejecutar el servidor
```bash
go run cmd/api/main.go
```

### 3. Actualizar Documentación (Opcional)
Si añades nuevos endpoints o cambias los comentarios de los handlers, regenera la doc con:
```bash
go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/api/main.go
```

---

## 📸 Sistema de Imágenes (Seguridad)

El sistema utiliza un **Proxy Seguro**. Nunca exponemos las URLs reales de AWS S3 al cliente.
1.  **Mapeo**: El backend guarda la imagen en S3 con una ruta privada y genera un UUID en la DB.
2.  **Servicio**: El cliente recibe `/api/images/{uuid}`.
3.  **Proxy**: El backend recibe la solicitud, busca el path real de S3 en la DB, y envía los bytes del archivo al cliente.

---

## 💬 Módulo de Chat

El chat funciona mediante WebSockets en `/api/chat/ws`. 
- Requiere autenticación vía token en la Query String: `?token=JWT_TOKEN`.
- El historial se guarda automáticamente en la tabla `messages`.
