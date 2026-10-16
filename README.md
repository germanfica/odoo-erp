# odoo-erp

![Odoo ERP](https://github.com/user-attachments/assets/8f198a2b-37d1-4acf-8415-e9d2b7f1e670)

Odoo Community 18 con addons OCA, localización Argentina y `odoo-web-responsive-dark-glass`. Los repositorios de addons se descargan con una **CLI escrita en Go (stdlib, sintaxis compatible con Go 1.17)**. No se necesitan Git ni Python para resolver, descargar, validar y preparar los addons.

El Dockerfile tiene tres etapas: **Go** (descargas y configuración), **Python wheels** (construcción de dependencias Python de Odoo/Argentina) e **imagen final `odoo:18.0`** (recibe únicamente los addons, la configuración generada y las dependencias Python ya compiladas). El compilador Go, Git y las herramientas de compilación de Python no pasan a la imagen final.

## Dependencias de addons

| Archivo | Propósito |
| --- | --- |
| `config/addons.mod` | Repositorios, ramas y nombres de carpeta. Es la fuente de dependencias. |
| `config/addons.lock` | **Opcional.** Revisiones exactas y SHA-256 del `.tar.gz`. No se incluye inicialmente. |
| `config/addons.required` | Directorios de módulos obligatorios (uno por línea), editable sin cambiar el código. Puede estar vacío si no se exigen módulos. |
| `config/odoo.conf` | Plantilla editable que **nunca se modifica** por el programa. |
| `/opt/odoo-addons/REVISIONS.txt` | Generado durante el build: repositorio, rama, SHA efectivo y SHA-256 descargado. |

Ejemplo de `addons.mod`:

```text
# repository|branch|directory
OCA/web|18.0|web
```

Si agregás un repositorio, editá `config/addons.mod` y, si corresponde, `config/addons.required`. **No hace falta modificar código Go, el Dockerfile ni `odoo.conf`.** Las rutas de Odoo se generan leyendo `addons.mod`.

### CLI en Go

Se compila localmente con Go 1.17 o superior (en el Dockerfile la etapa de compilación usa Go 1.26):

```bash
go test ./...
go build -o odoo-addons ./cmd/odoo-addons
./odoo-addons validate
./odoo-addons fetch --dry-run
```

También se pueden ejecutar los comandos sin compilar explícitamente:

```bash
go run ./cmd/odoo-addons validate
go run ./cmd/odoo-addons update
go run ./cmd/odoo-addons generate --config-destination ./build/odoo.conf
go run ./cmd/odoo-addons fetch --destination ./build/odoo-addons \
  --config-destination ./build/odoo.conf
```

Los comandos son:

- `validate`: comprueba formato de `addons.mod`, `addons.required`, lock opcional y plantilla de Odoo sin descargar ni escribir nada.
- `fetch`: con lock válido utiliza el commit bloqueado y comprueba SHA-256; sin lock consulta el SHA de la rama actual mediante la API de GitHub, descarga ese SHA y registra las revisiones. Extrae con validación de rutas, límites de tamaño y rechazo de enlaces simbólicos. No sobrescribe carpetas existentes.
- `update`: consulta los SHA actuales, descarga/valida los archivos y actualiza `addons.lock` mediante reemplazo de archivo temporal solo después de completar correctamente todas las descargas.
- `generate`: genera un nuevo `odoo.conf` a partir de la plantilla y de `addons.mod`, sin tocar el archivo original ni sobrescribir un destino existente.

Para fijar dependencias:

```bash
go run ./cmd/odoo-addons update
git add config/addons.mod config/addons.required config/addons.lock
git commit -m "chore(deps): lock Odoo addons"
```

Para volver a seguir las ramas, quitá `config/addons.lock` del proyecto. **Si existe pero está incompleto o difiere de `addons.mod`, el build falla; no hace fallback silencioso.**

`REVISIONS.txt` siempre contiene el SHA resuelto y el SHA-256 del archivo recibido, incluso en modo sin lock. El SHA fija la revisión del código y el hash del archivo valida los bytes del `.tar.gz`. GitHub puede regenerar un archivo para el mismo commit: en ese caso un lock con SHA-256 viejo fallará explícitamente y deberá verificarse/actualizarse. No es idéntico al sistema de checksum DB de Go.

Las ramas flotantes **no producen builds reproducibles**. El bloqueo cubre únicamente addons: la imagen base y las dependencias Python no están aún fijadas mediante digest/hashes. Para una release estricta fijá también estos componentes.

### Red y credenciales

Para resolver ramas, el programa consulta `api.github.com`; para los archivos usa `codeload.github.com`. Funciona con repositorios públicos y HTTPS, sin ejecutar `git clone`. Opcionalmente admite `GITHUB_TOKEN` como variable de entorno **únicamente para las llamadas a la API**, por límite de solicitudes. No lo guardes en Docker `ARG`, variables versionadas ni logs. Si lo usás durante `docker build`, transmitilo como secreto de BuildKit en lugar de incorporarlo a la imagen.

Los tamaños de archivos y descompresión están limitados como defensa contra archivos maliciosos. La instalación se prepara fuera de las rutas finales y solo se mueve cuando todas las comprobaciones y addons requeridos pasan. El comando `fetch` se niega a reemplazar un destino existente; utilizá una carpeta nueva para probarlo de nuevo.

## `odoo.conf` editable y `addons_path` autogenerado

La plantilla `config/odoo.conf` conserva las opciones y los comentarios del usuario. Si el usuario agrega un `addons_path = ...` activo, **solo esa opción se sustituye en el archivo generado**, que se almacena en `/etc/odoo/odoo.conf` de la imagen final. La plantilla del repositorio no cambia.

Las rutas generadas incluyen en orden:

1. `/usr/lib/python3/dist-packages/odoo/addons` (core).
2. `/mnt/extra-addons` (personalizaciones montadas por el usuario).
3. `/opt/odoo-addons/<directory>` por cada repositorio en `addons.mod`.

El Compose generado **no** monta `./config/odoo.conf` sobre `/etc/odoo/odoo.conf`, ya que ocultaría el archivo que creó Go. Para aplicar modificaciones a la plantilla o los repositorios es necesario construir y desplegar otra imagen.

## Docker, Ansible y Jenkins

```bash
docker build --pull --build-arg ADDONS_REFRESH="$(date +%s)" \
  -t odoo-community-argentina:18-local .
```

`ADDONS_REFRESH` invalida la caché de descarga de los addons en la etapa Go. En Jenkins se pasa `BUILD_NUMBER`. Antes de compilar el binario, la etapa Go del Dockerfile corre `go test ./...` sin acceso a red para los tests. Jenkins también ejecuta `go test`/`validate` si hay Go instalado en el agente; si no, los tests se ejecutan igualmente en el build remoto de Docker.

`playbooks/build-odoo-image.yml` copia al host de despliegue el Dockerfile, `go.mod`, los archivos `.go`, los manifiestos y el archivo de requisitos Python. Copia el `addons.lock` solo si existe y **elimina un lock antiguo del contexto remoto** si ya no está en Git. El código Go solo se utiliza en la etapa de construcción; no necesita estar instalado en el host remoto ni en el contenedor final.

Los playbooks externos mencionados por Jenkins (`/opt/ansible-infra/playbooks/odoo-copy-config.yml` y `deploy-docker-app.yml`) **no fueron proporcionados**. Es necesario confirmar que no vuelvan a montar la plantilla `config/odoo.conf` encima del archivo generado y que el volumen de PostgreSQL siga funcionando como se espera.

### Dependencias Python

Odoo continúa usando Python. El Dockerfile genera wheels de M2Crypto, PyAfipWs y los demás paquetes en una etapa aparte y los instala *offline* desde esos wheels en la imagen final. Se conserva la referencia existente a `filoquin/pyafipws`, rama `py3k`, mediante un ZIP HTTPS en vez de `git+https`. Esto evita requerir Git **para esa descarga**, pero no fija su versión. Verificá compilación y compatibilidad de esos paquetes antes de utilizar facturación argentina en producción.

### Verificaciones antes del despliegue

Se probaron los flujos de configuración y lock, validación de nombres, rechazo de archivos `.tar.gz` con rutas peligrosas, rechazo de symlinks y ejecución sin mutaciones con `--dry-run`. El ZIP no sustituye un build real de Docker ni pruebas completas de Odoo, PostgreSQL y homologación de ARCA. Antes de usarlo en producción, verificá la instalación de módulos, facturación electrónica, HTTPS, permisos, copias de seguridad y restauración de datos. Evitá colocar secretos en el Dockerfile o los manifiestos.

## Reverse proxy example

Ejemplo conservado del README original:

```nginx
location / {
    # Si van a la raíz, los llevamos a /odoo/
    rewrite ^/$ /odoo/ break;

    # Apuntamos al backend de Odoo (RFC 5737, TEST-NET-1)
    proxy_pass http://192.0.2.10:8069/;

    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

- Si Nginx Proxy Manager y Odoo comparten red Docker, podés usar `http://odoo:8069/`.
- Revisá el prefijo `/odoo/`; no establezcas `http_root = /odoo` sin ajustar el proxy.
- Restringí la exposición de `8069` a las redes necesarias.

## Update apps list

After deploying new addons, go to **Apps - Update Apps List** to make them available for installation.

<img width="897" height="246" alt="image" src="https://github.com/user-attachments/assets/0c440c53-c3ca-466c-85d6-69b3017e2c7f" />

**Note:** activate developer mode before updating the apps list.

`Settings - General Settings - Developer Tools - Activate the developer mode`

<img width="405" height="169" alt="image" src="https://github.com/user-attachments/assets/e996b0b0-1ed4-4213-9981-153edbb775e9" />
