FROM odoo:18.0  # Usa la imagen base de Odoo Community

# Copia los archivos de Odoo Enterprise al contenedor
COPY enterprise /mnt/enterprise

# Cambia el propietario de los archivos (si es necesario)
RUN chown -R odoo:odoo /mnt/enterprise

# Añade la ruta de los addons de Enterprise a la configuración de Odoo
ENV ADDONS_PATH /mnt/enterprise,/mnt/extra-addons

# Comando para ejecutar Odoo
CMD ["odoo"]