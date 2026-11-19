# Despliegue de Empre en AWS (EC2 t4g.micro + Caddy)

El backend corre en una EC2 ARM en Ohio (`us-east-2`), la misma región de la base de Supabase y del bucket de S3.
Caddy, en la misma máquina, pone el HTTPS con un certificado gratis de Let's Encrypt. GitHub Actions construye la
imagen en cada push a `main` y la despliega por SSH.

```
App (celular) ──HTTPS / WSS──▶ Caddy :443 ──▶ backend :8080 ──▶ Supabase (us-east-2)
                                                         └────▶ S3, IA, Wompi
```

| Archivo | Para qué |
|---|---|
| `docker-compose.prod.yml` | El backend y Caddy. El puerto 8080 no se abre hacia afuera. |
| `Caddyfile` | HTTPS automático y proxy al backend (incluye el websocket del chat). |
| `env.production.example` | Plantilla del `.env` del servidor. |
| `bootstrap.sh` | Prepara la máquina nueva una sola vez: Docker, swap, actualizaciones. |
| `deploy.sh` | Despliega una versión, espera a que esté sana y vuelve atrás sola si no arranca. |

Costo: unos 10,40 USD al mes (máquina, IP fija y disco). Ver la hoja «Empre · Costos y precios».

---

## 1. Dominio temporal (DuckDNS)

1. Entra a <https://www.duckdns.org> con tu cuenta de Google o GitHub.
2. Crea un subdominio, por ejemplo `empre-api`. Tu dominio queda `empre-api.duckdns.org`.
3. Déjalo abierto: en el paso 3 le pones la IP de la máquina.

## 2. Crear la máquina

En la consola de AWS, **región Ohio (us-east-2)**:

1. **EC2 → Launch instance**
   - Nombre: `empre-api`
   - Imagen: **Ubuntu Server 24.04 LTS**, arquitectura **64-bit (Arm)**
   - Tipo: **t4g.micro**
   - Key pair: crea uno nuevo, tipo **ED25519**, nombre `empre-deploy`. Guarda el `.pem`: es la llave para entrar
     y la que usará GitHub Actions.
   - Red, security group nuevo `empre-api` con estas reglas de entrada:

     | Tipo | Puerto | Origen | Por qué |
     |---|---|---|---|
     | SSH | 22 | 0.0.0.0/0 | Tú y GitHub Actions (sus IP cambian). Solo entra quien tenga la llave: Ubuntu no acepta contraseñas. |
     | HTTP | 80 | 0.0.0.0/0 | Let's Encrypt valida el dominio por aquí y Caddy redirige a HTTPS. |
     | HTTPS | 443 | 0.0.0.0/0 | La app. |

   - Almacenamiento: **8 GB gp3**.
   - Advanced details → Credit specification: **Unlimited** (es el valor por defecto de las t4g). No se pone lenta
     si gasta sus créditos; las alarmas del paso 9 avisan si eso empieza a costar.
2. **EC2 → Elastic IPs → Allocate**, y luego **Associate** a `empre-api`. Esta es la IP fija.

## 3. Apuntar el dominio

En DuckDNS, escribe la Elastic IP en tu subdominio y dale **update ip**. Comprueba desde tu PC:

```bash
nslookup empre-api.duckdns.org   # debe responder la Elastic IP
```

## 4. Preparar la máquina

Desde la carpeta `empre_backend` de tu PC (en Windows, PowerShell trae `ssh` y `scp`):

```bash
scp -i empre-deploy.pem deploy/bootstrap.sh ubuntu@<ELASTIC_IP>:~
ssh -i empre-deploy.pem ubuntu@<ELASTIC_IP> "bash bootstrap.sh"
```

Tarda unos minutos. Al terminar, la máquina tiene Docker, 1 GB de swap y la carpeta `/opt/empre`.

## 5. El `.env` de producción

1. Copia `deploy/env.production.example` a un archivo `env.production` **fuera del repo** y llénalo. Los valores de
   la base, S3, la IA y Wompi son los mismos de tu `.env` actual. Cambia:
   - `DOMAIN` y `APP_URL` con tu dominio de DuckDNS.
   - `ACME_EMAIL` con tu correo.
   - `JWT_SECRET`: uno nuevo y largo (`openssl rand -hex 32`). Con uno débil, el backend no arranca en producción.
   - `METRICS_TOKEN`: otro valor aleatorio.
2. Súbelo junto con los demás archivos:

```bash
scp -i empre-deploy.pem deploy/docker-compose.prod.yml deploy/Caddyfile deploy/deploy.sh ubuntu@<ELASTIC_IP>:/opt/empre/
scp -i empre-deploy.pem env.production ubuntu@<ELASTIC_IP>:/opt/empre/.env
ssh -i empre-deploy.pem ubuntu@<ELASTIC_IP> "chmod 600 /opt/empre/.env && chmod +x /opt/empre/deploy.sh"
```

Si el repositorio de Docker Hub `gioleonp/empre-backend` es **privado**, la máquina tiene que poder bajarlo. Crea en
Docker Hub un token de solo lectura y corre una vez en el servidor: `docker login -u gioleonp`.

## 6. GitHub Actions

En el repo `Empre-tech/empre_backend` → Settings → Secrets and variables → Actions:

| Tipo | Nombre | Valor |
|---|---|---|
| Secret | `EC2_HOST` | La Elastic IP |
| Secret | `EC2_USER` | `ubuntu` |
| Secret | `EC2_SSH_KEY` | Todo el contenido de `empre-deploy.pem` |
| Secret | `DOCKER_USERNAME`, `DOCKER_TOKEN` | Ya existen; déjalos |
| Variable | `API_DOMAIN` | `empre-api.duckdns.org` (sin https) |

Los secretos `DB_*`, `JWT_SECRET`, `S3_*` y `APP_URL` del despliegue anterior ya no se usan: la configuración vive en
el `.env` del servidor. Puedes borrarlos.

## 7. Primer despliegue

Haz push a `main`. En la pestaña **Actions** corren, en orden:

1. **CI - Build & Push Docker Image**: construye la imagen ARM y la sube con el sha del commit y `latest`.
2. **CD - Deploy to EC2**: copia los archivos de `deploy/`, corre `deploy.sh <sha>` y prueba
   `https://<tu dominio>/health`.

La primera vez Caddy pide el certificado: puede tardar unos segundos más. Comprueba:

```bash
curl https://empre-api.duckdns.org/health   # {"status":"ok"}
curl https://empre-api.duckdns.org/ready    # 200 si la base responde
```

## 8. Lo que apunta al dominio nuevo

- **App**: en el build de producción, `EXPO_PUBLIC_API_URL=https://empre-api.duckdns.org`. El chat usa la misma
  dirección con `wss://`.
- **Wompi**: en el panel, la URL de eventos es `https://empre-api.duckdns.org/api/payments/wompi/webhook`.

## 9. Alarmas y presupuesto

Con la AWS CLI (región `us-east-2`). Cambia el correo y el id de la instancia:

```bash
REGION=us-east-2
INSTANCE=i-0123456789abcdef0
EMAIL=tu@correo.com

TOPIC=$(aws sns create-topic --name empre-alertas --region $REGION --query TopicArn --output text)
aws sns subscribe --topic-arn $TOPIC --protocol email --notification-endpoint $EMAIL --region $REGION
# Confirma la suscripción en el correo que te llega.

# Los créditos de CPU bajan: lleva rato usando más de lo que la máquina tiene asegurado.
aws cloudwatch put-metric-alarm --region $REGION --alarm-name empre-creditos-cpu-bajos \
  --namespace AWS/EC2 --metric-name CPUCreditBalance --dimensions Name=InstanceId,Value=$INSTANCE \
  --statistic Minimum --period 300 --evaluation-periods 3 --threshold 50 \
  --comparison-operator LessThanThreshold --alarm-actions $TOPIC

# Empezó a cobrar CPU extra (modo Unlimited).
aws cloudwatch put-metric-alarm --region $REGION --alarm-name empre-cpu-extra-cobrada \
  --namespace AWS/EC2 --metric-name CPUSurplusCreditsCharged --dimensions Name=InstanceId,Value=$INSTANCE \
  --statistic Maximum --period 300 --evaluation-periods 1 --threshold 0 \
  --comparison-operator GreaterThanThreshold --alarm-actions $TOPIC --treat-missing-data notBreaching

# La máquina no responde.
aws cloudwatch put-metric-alarm --region $REGION --alarm-name empre-maquina-caida \
  --namespace AWS/EC2 --metric-name StatusCheckFailed --dimensions Name=InstanceId,Value=$INSTANCE \
  --statistic Maximum --period 60 --evaluation-periods 3 --threshold 1 \
  --comparison-operator GreaterThanOrEqualToThreshold --alarm-actions $TOPIC
```

Presupuesto: **Billing → Budgets → Create budget → Monthly cost budget**, 15 USD, aviso al 80 % y al 100 % a tu
correo.

## 10. El día a día

```bash
ssh -i empre-deploy.pem ubuntu@<ELASTIC_IP>
docker logs -f --tail 100 empre-api         # logs del backend
docker logs --tail 100 empre-caddy          # logs de Caddy (certificado, peticiones)
/opt/empre/deploy.sh rollback               # volver a la versión anterior
```

- **Cambiar una variable**: edita `/opt/empre/.env` y corre
  `cd /opt/empre && docker compose -f docker-compose.prod.yml up -d`.
- **Desplegar a mano**: Actions → CD - Deploy to EC2 → Run workflow (vacío = el último commit de `main`).
- **Si la máquina se queda corta** (los créditos bajan todos los días): detenla, cambia el tipo a **t4g.small**
  (en la consola de EC2: Actions → Instance settings → Change instance type) y enciéndela. La IP fija y los datos se quedan igual.
- **Cuando funcione todo**, apaga la EC2 del despliegue anterior para no pagar dos máquinas.
