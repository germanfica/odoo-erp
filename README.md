# odoo-erp

![image](https://github.com/user-attachments/assets/8f198a2b-37d1-4acf-8415-e9d2b7f1e670)

```nginx
location / {
    # 1) Si van a la raíz, los llevamos a /odoo/
    rewrite ^/$ /odoo/ break;

    # 2) Apuntamos al backend de Odoo
    #    - Cambialo por la IP real del host: 192.0.2.10 (RFC 5737, TEST-NET-1)
    #    - O por el nombre del contenedor en la red Docker: "odoo"
    proxy_pass http://192.0.2.10:8069/;

    # 3) Preservamos los headers para que Odoo conozca el Host y el esquema
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}

# ──────────────────────────────────────────────
# NOTAS:
# - Asegurate de que NPM y Odoo compartan red Docker si querés usar
#   "http://odoo:8069/" en lugar de IP numérica.
# - Fijate siempre el slash final en proxy_pass para evitar rutas rotas.
# - Estos comentarios no afectan la ejecución, solo te ayudan a entenderlo.
```
