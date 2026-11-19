#!/usr/bin/env bash
# Despliega una versión del backend en el servidor. Lo llama GitHub Actions por SSH en cada push a main,
# y también se puede correr a mano:
#
#   /opt/empre/deploy.sh <tag>        # tag = sha del commit (la imagen que subió el CI)
#   /opt/empre/deploy.sh rollback     # vuelve a la versión anterior
#
# Baja la imagen, reinicia el backend, espera a que /health responda y, si no arranca sano, vuelve solo a la
# versión que estaba. Caddy se queda corriendo, así que el corte dura lo que tarda en arrancar el backend.
set -euo pipefail

APP_DIR=/opt/empre
COMPOSE=(docker compose -f "$APP_DIR/docker-compose.prod.yml" --project-directory "$APP_DIR")
ENV_FILE="$APP_DIR/.env"
PREV_FILE="$APP_DIR/.previous_tag"
HEALTH_WAIT_SECONDS=120

cd "$APP_DIR"

current_tag() {
  grep -E '^TAG=' "$ENV_FILE" | tail -n1 | cut -d= -f2-
}

set_tag() {
  if grep -qE '^TAG=' "$ENV_FILE"; then
    sed -i "s|^TAG=.*|TAG=$1|" "$ENV_FILE"
  else
    echo "TAG=$1" >>"$ENV_FILE"
  fi
}

wait_healthy() {
  local waited=0 status=""
  while [ "$waited" -lt "$HEALTH_WAIT_SECONDS" ]; do
    status=$(docker inspect -f '{{.State.Health.Status}}' empre-api 2>/dev/null || echo "missing")
    if [ "$status" = "healthy" ]; then
      return 0
    fi
    sleep 5
    waited=$((waited + 5))
  done
  echo "El backend no quedó sano en ${HEALTH_WAIT_SECONDS}s (estado: $status)." >&2
  return 1
}

start() {
  "${COMPOSE[@]}" up -d --remove-orphans
}

[ -f "$ENV_FILE" ] || { echo "Falta $ENV_FILE (ver deploy/README.md)." >&2; exit 1; }

target="${1:?Uso: deploy.sh <tag> | rollback}"
previous="$(current_tag)"

if [ "$target" = "rollback" ]; then
  [ -s "$PREV_FILE" ] || { echo "No hay versión anterior guardada." >&2; exit 1; }
  target="$(cat "$PREV_FILE")"
  echo "==> Volviendo a $target"
fi

echo "==> Desplegando $target (antes: ${previous:-ninguna})"
set_tag "$target"

if ! "${COMPOSE[@]}" pull api; then
  echo "No se pudo bajar la imagen $target." >&2
  set_tag "$previous"
  exit 1
fi

start

if wait_healthy; then
  if [ -n "$previous" ] && [ "$previous" != "$target" ]; then
    echo "$previous" >"$PREV_FILE"
  fi
  echo "==> Listo: $target está sano."
  # Borra imágenes viejas que ya no usa ningún contenedor (el disco es de 8 GB).
  docker image prune -af --filter "until=168h" >/dev/null || true
  exit 0
fi

echo "==> Últimas líneas del backend:" >&2
docker logs --tail 50 empre-api >&2 || true

if [ -n "$previous" ] && [ "$previous" != "$target" ]; then
  echo "==> Volviendo a $previous" >&2
  set_tag "$previous"
  start
  wait_healthy || echo "La versión anterior tampoco quedó sana: revisa a mano." >&2
fi
exit 1
