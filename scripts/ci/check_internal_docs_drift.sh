#!/usr/bin/env bash
# check_internal_docs_drift.sh — la documentación INTERNA no cita ficheros que
# no existen.
#
# El sitio público tiene guard de deriva (scripts/website/check-coverage.sh),
# pero `docs/**` no tenía ninguno, y se nota: el manual de desarrollo estuvo
# catorce meses describiendo un panel de administración extraído del núcleo,
# con un enlace a `docs/ADMIN_CLUSTER_LAB.md` que nunca existió. Nadie lo vio
# porque nada lo miraba.
#
# Este guard comprueba lo verificable por máquina sin adivinar intenciones:
# cada ruta que la documentación interna cita como si existiera —enlaces
# markdown relativos, y rutas de fichero entre comillas invertidas— resuelve
# en el árbol. No juzga si el texto es cierto; solo que aquello a lo que
# apunta está ahí.
#
# Alcance: la documentación VIVA, la que describe el sistema tal y como es hoy
# (docs/*.md, guides/, reference/, governance/). Los directorios históricos
# quedan fuera a propósito: un ADR de mayo que cita `pkg/admin/panel.go`, o una
# auditoría fechada que enlaza una página que luego se movió, no están
# derivados — son actas de lo que era cierto entonces, y "corregirlas" sería
# falsificar el registro. Los históricos son: adrs/, audits/, iterations/,
# reports/, deprecations/, migration_assistants/, templates/.
#
# Uso: bash scripts/ci/check_internal_docs_drift.sh [dir]   (default docs)
set -uo pipefail

DOCS_DIR="${1:-docs}"
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT" || exit 1

if [ ! -d "$DOCS_DIR" ]; then
  echo "FAIL: no existe $DOCS_DIR"
  exit 1
fi

DOCS_DIR="$DOCS_DIR" python3 - <<'PY'
import os
import re
import sys

docs_dir = os.environ["DOCS_DIR"]

# 1) Enlaces markdown relativos: [texto](ruta.md) / (ruta.md#ancla).
link_re = re.compile(r'\[[^\]]*\]\((?!https?://|mailto:|#)([^)\s]+)\)')
# 2) Rutas de fichero en `código`, restringidas a los directorios de PRIMER
#    NIVEL de este repo. La restricción no es cosmética: buena parte de la
#    documentación describe el árbol del LECTOR (`handlers/`, `models/`,
#    `config/rbac_policy.csv` son rutas de su proyecto, no del nuestro), y sin
#    ella el guard perseguiría ficheros que nunca debieron existir aquí.
REPO_DIRS = ("pkg/", "internal/", "cmd/", "scripts/", "docs/", "website/",
             "examples/", "contracts/", ".github/", "migrations/")
path_re = re.compile(r'`((?:[\w.-]+/)+[\w.-]+\.(?:go|md|yml|yaml|json|sql|sh|ts|tsx|csv|txt))`')

# Escape por página, para las que documentan de arriba abajo el proyecto del
# lector (el layout generado, el tutorial): ahí toda ruta es ajena.
ESCAPE = "<!-- docs-drift-allow-external-paths -->"

broken = []
scanned = 0

# Directorios de registro histórico: ver la cabecera del script.
HISTORICAL = {"adrs", "audits", "iterations", "reports", "deprecations",
              "migration_assistants", "templates"}

# Los .md de la raíz del repo (README, SECURITY, CONTRIBUTING, CLAUDE, SPEC)
# son documentación viva con más lectores que nadie, y hasta 2026-08 nadie
# los miraba: SECURITY.md enlazó rutas muertas durante meses y README
# enlazaba un PLUGIN_EXAMPLES.md que nunca existió. Mismo contrato que
# docs/: lo que citan tiene que existir.
# CHANGELOG.md queda fuera por la misma razón que audits/ e iterations/:
# es un acta acumulada (además, propiedad de release-please) — sus entradas
# antiguas citan ficheros que existían entonces, y "corregirlas" sería
# falsificar el registro.
ROOT_HISTORICAL = {"CHANGELOG.md"}
pages = []
for fn in sorted(os.listdir(".")):
    if fn.endswith(".md") and fn not in ROOT_HISTORICAL:
        pages.append(("." , fn))

for dirpath, dirnames, filenames in os.walk(docs_dir):
    if os.path.dirname(dirpath) == docs_dir or dirpath == docs_dir:
        dirnames[:] = [d for d in dirnames if d not in HISTORICAL]
    for filename in filenames:
        if filename.endswith(".md"):
            pages.append((dirpath, filename))

for dirpath, filename in pages:
        scanned += 1
        page = os.path.join(dirpath, filename)
        text = open(page, encoding="utf-8", errors="ignore").read()

        for target in link_re.findall(text):
            target = target.split("#", 1)[0]
            if not target:
                continue  # enlace a un ancla de la propia página
            resolved = os.path.normpath(os.path.join(dirpath, target))
            if not os.path.exists(resolved):
                broken.append((page, target, "enlace"))

        if ESCAPE in text:
            continue
        for target in path_re.findall(text):
            # Las rutas en código se citan desde la raíz del repo.
            if not target.startswith(REPO_DIRS):
                continue
            if not os.path.exists(os.path.normpath(target)):
                broken.append((page, target, "ruta citada"))

if broken:
    print(f"FAIL: {len(broken)} referencia(s) rotas en la documentación interna ({scanned} páginas escaneadas):")
    for page, target, kind in sorted(set(broken)):
        print(f"  {page}: {kind} {target!r} no existe")
    print("")
    print("Corrige la ruta, o borra el párrafo si describe algo que ya no existe:")
    print("una referencia a un fichero ausente suele señalar texto que quedó atrás.")
    print("")
    print("Si el fichero SÍ existe pero en otra rama todavía sin fusionar, esto no")
    print("es deriva: es orden. La documentación que cita código nuevo va DESPUÉS")
    print("del código, en su propio PR o en el mismo — nunca antes.")
    sys.exit(1)

print(f"OK: la documentación interna no cita ficheros ausentes ({scanned} páginas escaneadas)")
PY
