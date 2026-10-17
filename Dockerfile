# Etapa de Go: prepara los addons y la configuracion, sin Git ni Python.
# El codigo fuente utiliza exclusivamente APIs compatibles con Go 1.17.
FROM golang:1.26-alpine AS addons-builder
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
RUN go test ./... && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /usr/local/bin/odoo-addons ./cmd/odoo-addons
COPY config/ ./config/

# Jenkins define ADDONS_REFRESH=BUILD_NUMBER para invalidar el cache del clone.
# Ahora invalida el cache de descargas HTTPS (sin git clone).
ARG ADDONS_REFRESH=0
RUN echo "Refreshing Odoo 18 addons (build=${ADDONS_REFRESH})" && \
    /usr/local/bin/odoo-addons fetch \
        --mod /src/config/addons.mod \
        --lock /src/config/addons.lock \
        --required /src/config/addons.required \
        --destination /build/odoo-addons \
        --config-source /src/config/odoo.conf \
        --config-destination /build/odoo.conf \
        --installed-prefix /opt/odoo-addons

# Python sigue siendo necesario para Odoo, pero las herramientas de compilacion
# de las dependencias nativas no se incluyen en la imagen final.
FROM odoo:18.0 AS python-wheels
USER root
# Dependencias de compilacion requeridas por M2Crypto/pyafipws.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates build-essential python3-dev swig pkg-config \
    libssl-dev libffi-dev && \
    rm -rf /var/lib/apt/lists/*
COPY requirements-argentina-ce.txt /tmp/requirements-argentina-ce.txt
RUN python3 -m pip wheel --no-cache-dir --wheel-dir /opt/wheels \
    -r /tmp/requirements-argentina-ce.txt

# Usa la imagen base de Odoo Community
FROM odoo:18.0

# 1. Pasamos a root para posibles instalaciones de sistema
USER root

# Copia los archivos de Odoo Enterprise al contenedor
#COPY enterprise /mnt/enterprise
#COPY addons /mnt/extra-addons

# Dependencias de compilacion requeridas por M2Crypto/pyafipws.
# Compiladas en la etapa python-wheels, no permanecen en la imagen final.
# Instala las wheels en una ruta aislada; no reemplaza paquetes de Ubuntu.
# Python utiliza esta ruta primero, incluyendo el entrypoint original de Odoo.
ENV PYTHONPATH=/opt/odoo-python
COPY --from=python-wheels /opt/wheels /tmp/wheels
RUN python3 -m pip install --no-cache-dir --no-index --find-links=/tmp/wheels \
    --target /opt/odoo-python /tmp/wheels/*.whl && \
    python3 -c "import M2Crypto, OpenSSL; from importlib.metadata import version; version('PyAfipWs')" && \
    rm -rf /tmp/wheels

# El resultado de Go se copia a la imagen final, sin copiar Go ni Git.
COPY --from=addons-builder /build/odoo-addons /opt/odoo-addons
COPY --from=addons-builder /build/odoo.conf /etc/odoo/odoo.conf

# Cambia el propietario de los archivos (si es necesario)
#RUN chown -R odoo:odoo /mnt/enterprise
RUN chmod -R a+rX /opt/odoo-addons && \
    chown odoo:odoo /etc/odoo/odoo.conf && \
    chown -R odoo:odoo /mnt/extra-addons

USER odoo

# Añade la ruta de los addons de Enterprise a la configuración de Odoo
# addons_path se genera desde addons.mod; esta variable se conserva por compatibilidad.
ENV ADDONS_PATH=/mnt/extra-addons

# Comando para ejecutar Odoo
USER odoo
ENTRYPOINT ["/entrypoint.sh"]
CMD ["odoo", "-c", "/etc/odoo/odoo.conf"]
