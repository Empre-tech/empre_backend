#!/usr/bin/env bash
# Prepara una EC2 nueva (Ubuntu 24.04, ARM) para correr Empre. Se corre una sola vez, como el usuario ubuntu:
#
#   curl -fsSL <url-raw-de-este-archivo> -o bootstrap.sh   (o cópialo con scp)
#   bash bootstrap.sh
#
# Instala Docker y Compose, crea 1 GB de swap, deja /opt/empre listo y activa las actualizaciones de seguridad.
# Se puede correr de nuevo sin romper nada.
set -euo pipefail

APP_DIR=/opt/empre
SWAP_FILE=/swapfile

echo "==> Paquetes del sistema"
sudo apt-get update -y
sudo DEBIAN_FRONTEND=noninteractive apt-get upgrade -y
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io docker-compose-v2 unattended-upgrades curl

echo "==> Docker"
sudo systemctl enable --now docker
# El usuario actual puede usar docker sin sudo (vale desde la próxima sesión SSH).
sudo usermod -aG docker "$USER"

echo "==> Swap de 1 GB (colchón para la máquina de 1 GB de RAM)"
if ! swapon --show | grep -q "$SWAP_FILE"; then
  sudo fallocate -l 1G "$SWAP_FILE"
  sudo chmod 600 "$SWAP_FILE"
  sudo mkswap "$SWAP_FILE"
  sudo swapon "$SWAP_FILE"
  grep -q "$SWAP_FILE" /etc/fstab || echo "$SWAP_FILE none swap sw 0 0" | sudo tee -a /etc/fstab >/dev/null
fi
# Usar la swap solo cuando de verdad haga falta.
echo "vm.swappiness=10" | sudo tee /etc/sysctl.d/99-empre.conf >/dev/null
sudo sysctl --system >/dev/null

echo "==> Actualizaciones de seguridad automáticas"
sudo dpkg-reconfigure -f noninteractive unattended-upgrades

echo "==> Carpeta de la app: $APP_DIR"
sudo mkdir -p "$APP_DIR"
sudo chown "$USER:$USER" "$APP_DIR"

echo
echo "Listo. Sigue con la guía (deploy/README.md):"
echo "  1. Sal y vuelve a entrar por SSH para usar docker sin sudo."
echo "  2. Copia a $APP_DIR: docker-compose.prod.yml, Caddyfile, deploy.sh y el .env lleno."
