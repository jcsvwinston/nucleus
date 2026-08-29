#!/usr/bin/env bash
# check_versioned_docs_markers.sh — un snapshot anuncia SU propia versión.
#
# El snapshot congela la doc tal cual está en main, y en main el marcador
# `x-release-please-version` todavía dice la versión anterior: es
# release-please quien lo sube, y lo hace en su propia rama, después. El
# resultado era que cada snapshot afirmaba una versión que no era la suya
# —la doc archivada de 1.14.0 decía «the current release is v1.13.0»—, y en
# la página que el sitio publica en la RAÍZ, porque la última versión
# archivada es la que se sirve por defecto. Cinco salieron así.
#
# cut_docs_snapshot.sh lo fija ahora al cortar; esto lo vigila.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

[ -d website/versioned_docs ] || { echo "OK: este sitio no versiona documentación."; exit 0; }

status=0
for dir in website/versioned_docs/version-*; do
  version="${dir##*/version-}"
  while IFS= read -r file; do
    while IFS= read -r line; do
      claimed=$(printf '%s\n' "$line" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)
      if [ -z "$claimed" ]; then
        echo "FAIL: $file: la línea del marcador no lleva versión: $line" >&2
        status=1
      elif [ "$claimed" != "v$version" ]; then
        echo "FAIL: $file afirma $claimed, pero es el snapshot de v$version — la doc archivada de una versión no puede anunciar otra" >&2
        status=1
      fi
    done < <(grep "x-release-please-version" "$file")
  done < <(grep -rl "x-release-please-version" "$dir" 2>/dev/null || true)
done

[ $status -eq 0 ] && echo "OK: cada snapshot de documentación anuncia su propia versión ($(ls -d website/versioned_docs/version-* | wc -l | tr -d ' ') snapshots)"
exit $status
