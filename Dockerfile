# Usa la imagen base de Odoo Community
FROM odoo:18.0

# 1. Pasamos a root para posibles instalaciones de sistema
USER root

# Copia los archivos de Odoo Enterprise al contenedor
#COPY enterprise /mnt/enterprise
COPY addons /mnt/extra-addons

# Cambia el propietario de los archivos (si es necesario)
#RUN chown -R odoo:odoo /mnt/enterprise
RUN chown -R odoo:odoo /mnt/extra-addons

USER odoo

# Añade la ruta de los addons de Enterprise a la configuración de Odoo
ENV ADDONS_PATH /mnt/enterprise,/mnt/extra-addons

# Comando para ejecutar Odoo
USER odoo
ENTRYPOINT ["/entrypoint.sh"]
CMD ["odoo", "-c", "/etc/odoo/odoo.conf"]