
#!/bin/sh
set -eu

# Repositorios publicos. Cada build trae los ultimos commits de la rama 18.0.
# Para releases reproducibles, reemplazar ramas por SHAs verificadas/pinneadas.
branch=18.0
root=/opt/odoo-addons
mkdir -p "$root"
: > "$root/REVISIONS.txt"

while IFS='|' read -r remote name; do
    [ -n "$remote" ] || continue
    echo "Cloning $remote ($branch)"
    git clone --quiet --depth 1 --branch "$branch" "https://github.com/$remote.git" "$root/$name"
    sha="$(git -C "$root/$name" rev-parse HEAD)"
    printf '%s %s %s\n' "$remote" "$branch" "$sha" >> "$root/REVISIONS.txt"
    # Falta de manifest significa que la estructura del repositorio cambio.
    if ! find "$root/$name" -mindepth 2 -maxdepth 2 -name __manifest__.py -print -quit | grep -q .; then
        echo "ERROR: no se encontraron addons Odoo en $remote" >&2
        exit 1
    fi
    rm -rf "$root/$name/.git"
done <<'REPOS'
ingadhoc/odoo-argentina|odoo-argentina
ingadhoc/odoo-argentina-ce|odoo-argentina-ce
OCA/account-financial-reporting|account-financial-reporting
OCA/account-reconcile|account-reconcile
OCA/account-financial-tools|account-financial-tools
OCA/account-budgeting|account-budgeting
OCA/server-ux|server-ux
OCA/server-tools|server-tools
OCA/reporting-engine|reporting-engine
REPOS

# Verificar modulos indispensables para los flujos de prueba descritos en README.
for module in \
    l10n_ar_afipws l10n_ar_afipws_fe \
    account_financial_report account_reconcile_oca \
    account_asset_management account_budget_oca \
    base_view_inheritance_extension \
    date_range report_xlsx report_xlsx_helper; do
    if ! find "$root" -mindepth 2 -maxdepth 2 -type d -name "$module" -print -quit | grep -q .; then
        echo "ERROR: modulo esperado ausente: $module" >&2
        exit 1
    fi
done
echo "Odoo addons descargados. Revisiones:"
cat "$root/REVISIONS.txt"
