#!/usr/bin/env bash
#
# Generate a development PKI for the Nucleus admin observability subsystem.
#
# Produces (under the chosen output directory):
#
#   ca.crt + ca.key          —  self-signed root certificate authority
#   server.crt + server.key  —  TLS cert for admin-server's agent listener,
#                               with SANs for "127.0.0.1", "::1", "localhost"
#                               and any extra hostnames passed via
#                               --server-hostname (repeatable)
#   agent.crt + agent.key    —  client cert agents present when mTLS is on,
#                               with CN=nucleus-agent and SANs for the
#                               agents' own hostnames if configured
#
# All certificates use ECDSA P-256 keys, are valid for 365 days, and are
# emitted in PEM format. They are intended ONLY for development and
# integration testing; production deployments MUST use real certificates
# from the operator's CA.
#
# Usage:
#   scripts/gen-dev-certs.sh [--out DIR] [--server-hostname HOST]... \
#                            [--agent-hostname HOST]... [--days N] [--force]
#
# Examples:
#   scripts/gen-dev-certs.sh                            # localhost-only
#   scripts/gen-dev-certs.sh --out ./certs/dev
#   scripts/gen-dev-certs.sh --server-hostname admin.internal --days 30

set -euo pipefail

OUT_DIR="./certs/dev"
SERVER_HOSTNAMES=()
AGENT_HOSTNAMES=()
DAYS=365
FORCE=0

print_usage() {
  sed -n '2,/^# Usage:/{/^# Usage:/!p;};/^#$/q' "$0" | sed 's/^# \{0,1\}//'
  echo
  echo "Run with --help for the rest of the comment block."
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out)              OUT_DIR="${2:?missing arg}"; shift 2 ;;
    --server-hostname)  SERVER_HOSTNAMES+=("${2:?missing arg}"); shift 2 ;;
    --agent-hostname)   AGENT_HOSTNAMES+=("${2:?missing arg}"); shift 2 ;;
    --days)             DAYS="${2:?missing arg}"; shift 2 ;;
    --force)            FORCE=1; shift ;;
    -h|--help)          print_usage; exit 0 ;;
    *) echo "unknown flag: $1" >&2; print_usage; exit 2 ;;
  esac
done

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required but not on PATH" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

if [[ -e "$OUT_DIR/ca.crt" && $FORCE -ne 1 ]]; then
  echo "$OUT_DIR/ca.crt already exists; pass --force to regenerate" >&2
  exit 1
fi

# ---- helpers ----------------------------------------------------------------

write_v3_ext() {
  local out="$1" cn="$2" purpose="$3"
  shift 3
  local sans=("$@")

  {
    echo "[req]"
    echo "distinguished_name = req_distinguished_name"
    echo "prompt = no"
    echo
    echo "[req_distinguished_name]"
    echo "CN = $cn"
    echo
    echo "[v3]"
    echo "subjectKeyIdentifier = hash"
    echo "authorityKeyIdentifier = keyid,issuer"
    echo "basicConstraints = CA:FALSE"
    case "$purpose" in
      server)
        echo "keyUsage = digitalSignature, keyEncipherment"
        echo "extendedKeyUsage = serverAuth, clientAuth"
        ;;
      client)
        echo "keyUsage = digitalSignature"
        echo "extendedKeyUsage = clientAuth"
        ;;
    esac

    if (( ${#sans[@]} > 0 )); then
      echo "subjectAltName = @alt_names"
      echo
      echo "[alt_names]"
      local i=1 dns_i=1 ip_i=1
      for s in "${sans[@]}"; do
        if [[ "$s" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ || "$s" == *":"* ]]; then
          echo "IP.${ip_i} = $s"
          ip_i=$((ip_i + 1))
        else
          echo "DNS.${dns_i} = $s"
          dns_i=$((dns_i + 1))
        fi
        i=$((i + 1))
      done
    fi
  } > "$out"
}

# ---- CA ---------------------------------------------------------------------

CA_CRT="$OUT_DIR/ca.crt"
CA_KEY="$OUT_DIR/ca.key"

openssl ecparam -name prime256v1 -genkey -noout -out "$CA_KEY"
openssl req -new -x509 -days "$DAYS" -key "$CA_KEY" -out "$CA_CRT" \
  -subj "/CN=Nucleus Dev Admin CA"

# ---- server cert ------------------------------------------------------------

SERVER_KEY="$OUT_DIR/server.key"
SERVER_CSR="$OUT_DIR/server.csr"
SERVER_CRT="$OUT_DIR/server.crt"
SERVER_EXT="$OUT_DIR/server.ext"

server_sans=("127.0.0.1" "::1" "localhost")
for h in "${SERVER_HOSTNAMES[@]:-}"; do
  if [[ -n "$h" ]]; then
    server_sans+=("$h")
  fi
done

openssl ecparam -name prime256v1 -genkey -noout -out "$SERVER_KEY"
write_v3_ext "$SERVER_EXT" "nucleus-admin-server" "server" "${server_sans[@]}"
openssl req -new -key "$SERVER_KEY" -out "$SERVER_CSR" -config "$SERVER_EXT"
openssl x509 -req -in "$SERVER_CSR" -CA "$CA_CRT" -CAkey "$CA_KEY" \
  -CAcreateserial -out "$SERVER_CRT" -days "$DAYS" \
  -extfile "$SERVER_EXT" -extensions v3
rm -f "$SERVER_CSR" "$SERVER_EXT"

# ---- agent client cert ------------------------------------------------------

AGENT_KEY="$OUT_DIR/agent.key"
AGENT_CSR="$OUT_DIR/agent.csr"
AGENT_CRT="$OUT_DIR/agent.crt"
AGENT_EXT="$OUT_DIR/agent.ext"

agent_sans=("nucleus-agent")
for h in "${AGENT_HOSTNAMES[@]:-}"; do
  if [[ -n "$h" ]]; then
    agent_sans+=("$h")
  fi
done

openssl ecparam -name prime256v1 -genkey -noout -out "$AGENT_KEY"
write_v3_ext "$AGENT_EXT" "nucleus-agent" "client" "${agent_sans[@]}"
openssl req -new -key "$AGENT_KEY" -out "$AGENT_CSR" -config "$AGENT_EXT"
openssl x509 -req -in "$AGENT_CSR" -CA "$CA_CRT" -CAkey "$CA_KEY" \
  -CAcreateserial -out "$AGENT_CRT" -days "$DAYS" \
  -extfile "$AGENT_EXT" -extensions v3
rm -f "$AGENT_CSR" "$AGENT_EXT" "$OUT_DIR/ca.srl"

chmod 600 "$OUT_DIR"/*.key

cat <<NOTE
Wrote dev PKI under $OUT_DIR:

  $CA_CRT      $CA_KEY
  $SERVER_CRT  $SERVER_KEY
  $AGENT_CRT   $AGENT_KEY

Wire-up cheat-sheet:

  admin-server (with mTLS):
    bin/admin-server \\
      --agent-cert $SERVER_CRT \\
      --agent-key  $SERVER_KEY \\
      --ui-cert    $SERVER_CRT \\
      --ui-key     $SERVER_KEY

  agent (in your nucleus.yml):
    state_dir: ./.nucleus-state
    admin:
      endpoints: ["https://admin.internal:9090"]
      tls:
        ca_file:   $CA_CRT
        cert_file: $AGENT_CRT
        key_file:  $AGENT_KEY

These certificates are for development and integration testing only.
Do not deploy them.
NOTE
