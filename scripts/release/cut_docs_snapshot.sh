#!/usr/bin/env bash
# cut_docs_snapshot.sh — congela la documentación actual como snapshot de una
# versión, para que el archivo publicado siga el ritmo de las releases.
#
# El sitio sirve SIEMPRE la doc actual, y bajo su ruta de versión los
# snapshots de las minors ya publicadas: es lo que permite a quien está
# clavado en una minor anterior leer la doc que le corresponde. Ese archivo
# se congeló en su día —se publicaron minors sin cortar snapshot— y el
# desfase no lo veía nadie; check_docs_archive_freshness.sh lo vigila ahora.
#
# Uso:
#   bash scripts/release/cut_docs_snapshot.sh            # versión del manifiesto
#   bash scripts/release/cut_docs_snapshot.sh 1.6.0      # explícita
#
# CUÁNDO: en el PR que precede al corte de una MINOR, para que el tag la
# contenga — el paraguas sirve la doc del TAG pinado, así que un snapshot
# añadido después no llega al sitio hasta la release siguiente.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

VERSION="${1:-$(python3 -c "import json;print(json.load(open('.release-please-manifest.json'))['.'])")}"

if ! printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "FAIL: versión inválida '$VERSION' (se espera X.Y.Z)" >&2
  exit 1
fi

if [ -d "website/versioned_docs/version-$VERSION" ]; then
  echo "OK: el snapshot $VERSION ya existe; nada que hacer."
  exit 0
fi

echo "Cortando snapshot de documentación $VERSION…"
(cd website && npm run docusaurus -- docs:version "$VERSION")

# El snapshot congela la doc TAL CUAL está en main, y en main el marcador
# `x-release-please-version` todavía dice la versión ANTERIOR: es
# release-please quien lo sube, y lo hace en su propia rama, después. El
# resultado es que cada snapshot anunciaba una versión que no era la suya
# —la doc de 1.14.0 decía «the current release is v1.13.0»— en la página
# que el sitio sirve por defecto, porque la última versión archivada es la
# que se publica en la raíz. Cinco snapshots salieron así antes de que
# nadie lo mirara.
#
# Aquí el snapshot ES la versión que se está cortando, así que el marcador
# se fija a ella.
echo "Fijando el marcador de versión del snapshot a v$VERSION…"
python3 - "$VERSION" <<'PYEOF'
import io, os, re, sys
version = sys.argv[1]
root = os.path.join("website", "versioned_docs", "version-" + version)
changed = 0
for dirpath, _, filenames in os.walk(root):
    for name in filenames:
        if not name.endswith((".md", ".mdx")):
            continue
        path = os.path.join(dirpath, name)
        text = io.open(path, encoding="utf-8").read()
        if "x-release-please-version" not in text:
            continue
        fixed = re.sub(
            r"v\d+\.\d+\.\d+(?=[^\n]*x-release-please-version)",
            "v" + version,
            text,
        )
        if fixed != text:
            io.open(path, "w", encoding="utf-8").write(fixed)
            changed += 1
print("  marcador fijado en %d fichero(s)" % changed)
PYEOF

echo
echo "Hecho. Revisa y commitea:"
echo "  website/versions.json"
echo "  website/versioned_docs/version-$VERSION/"
echo "  website/versioned_sidebars/version-$VERSION-sidebars.json"
