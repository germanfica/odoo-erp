
# Usa la imagen base de Odoo Community
FROM odoo:18.0

# 1. Pasamos a root para posibles instalaciones de sistema
USER root

# Copia los archivos de Odoo Enterprise al contenedor
#COPY enterprise /mnt/enterprise
COPY addons /mnt/extra-addons

# Dependencias de compilacion requeridas por M2Crypto/pyafipws.
RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates build-essential python3-dev swig pkg-config \
    libssl-dev libffi-dev && \
    rm -rf /var/lib/apt/lists/*

COPY requirements-argentina-ce.txt /tmp/requirements-argentina-ce.txt
RUN python3 -m pip install --no-cache-dir --break-system-packages \
    -r /tmp/requirements-argentina-ce.txt && \
    rm /tmp/requirements-argentina-ce.txt

COPY scripts/fetch-addons.sh /usr/local/bin/fetch-odoo-addons
RUN chmod 755 /usr/local/bin/fetch-odoo-addons

# Jenkins define ADDONS_REFRESH=BUILD_NUMBER para invalidar el cache del clone.
ARG ADDONS_REFRESH=0
RUN echo "Refreshing Odoo 18 addons (build=${ADDONS_REFRESH})" && \
    /usr/local/bin/fetch-odoo-addons && \
    chmod -R a+rX /opt/odoo-addons

# Cambia el propietario de los archivos (si es necesario)
#RUN chown -R odoo:odoo /mnt/enterprise
RUN chown -R odoo:odoo /mnt/extra-addons

USER odoo

# Añade la ruta de los addons de Enterprise a la configuración de Odoo
ENV ADDONS_PATH=/mnt/enterprise,/mnt/extra-addons

# Comando para ejecutar Odoo
USER odoo
ENTRYPOINT ["/entrypoint.sh"]
CMD ["odoo", "-c", "/etc/odoo/odoo.conf"]