# Imagen del backend de Empre.
#
# Se construye para la arquitectura que pida buildx (`--platform linux/arm64` para la EC2 t4g, que es ARM):
# Go compila de forma cruzada en la máquina que construye, sin emulación, así que el build es rápido.

# --- Compilación ---
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

WORKDIR /app

# Primero las dependencias: se cachean mientras go.mod y go.sum no cambien.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/main ./cmd/api

# --- Imagen final ---
FROM alpine:3.20

# ca-certificates: TLS con Supabase, S3, la IA y Wompi.
# tzdata: Ofertas usa la zona America/Bogota (time.LoadLocation).
RUN apk --no-cache add ca-certificates tzdata \
    && adduser -D -H -u 10001 empre

WORKDIR /app
COPY --from=builder /out/main ./main
COPY --from=builder /app/docs ./docs

USER empre
EXPOSE 8080

# /health responde si el proceso está vivo, sin tocar la base.
HEALTHCHECK --interval=15s --timeout=3s --start-period=60s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

CMD ["./main"]
