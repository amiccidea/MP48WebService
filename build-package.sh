#!/bin/bash
set -e

# ============================================================
#  BUILD PACKAGE CON GO 1.15 PER RTU i586 (senza MMX/SSE)
#  Kernel 2.6.32.11 compatibile
#  - GO386=387 (usa FPU x87, no MMX/SSE)
#  - gcflags all=-N -l (disabilita ottimizzazioni)
# ============================================================

# ---- CONFIGURAZIONE ----
GO_CMD="/usr/local/go1.15/bin/go"
BIN_NAME="MP48WebService"
ARCH="386"
PKG_NAME="${BIN_NAME}_dist_${ARCH}"
OUTPUT_TAR="${PKG_NAME}.tar.gz"

# ---- FUNZIONI ----
print_step() {
    echo ""
    echo "========================================"
    echo "  $1"
    echo "========================================"
}

# ---- CONTROLLO GO 1.15 ----
if [ ! -x "$GO_CMD" ]; then
    echo "❌ $GO_CMD non trovato."
    echo "   Installa Go 1.15.15:"
    echo "   wget https://go.dev/dl/go1.15.15.linux-amd64.tar.gz"
    echo "   sudo tar -C /usr/local -xzf go1.15.15.linux-amd64.tar.gz"
    echo "   sudo mv /usr/local/go /usr/local/go1.15"
    exit 1
fi

GO_VERSION=$($GO_CMD version)
echo "✅ Usando $GO_VERSION"

# ---- 1. PULIZIA VENDOR PRECEDENTE ----
print_step "Pulizia vendor precedente"
rm -rf vendor

# ---- 2. FISSA VERSIONI DIPENDENZE (compatibili con Go 1.15) ----
print_step "Fissaggio versioni compatibili con Go 1.15"

export GOOS=linux
export GOARCH=386
export GO386=387
export CGO_ENABLED=0

$GO_CMD mod edit -go=1.15

$GO_CMD get golang.org/x/crypto@v0.0.0-20211215153901-e495a2d5b3d3
$GO_CMD get github.com/gorilla/csrf@v1.7.1
$GO_CMD get github.com/gorilla/sessions@v1.2.1
$GO_CMD get github.com/jlaffaye/ftp@v0.1.0
$GO_CMD get github.com/pquerna/otp@v1.3.0
$GO_CMD get github.com/skip2/go-qrcode@v0.0.0-20200617195104-da1b6568686e

$GO_CMD mod tidy

# ---- 3. VENDOR (offline) ----
print_step "Vendor con $GO_CMD"
export GOOS=linux
export GOARCH=386
export GO386=387
export CGO_ENABLED=0

$GO_CMD mod vendor

# ---- 4. COMPILAZIONE ----
print_step "Compilazione per Linux i586 (GO386=387, no MMX/SSE)"

GOOS=linux GOARCH=386 GO386=387 CGO_ENABLED=0 $GO_CMD build \
    -mod=vendor \
    -gcflags="all=-N -l" \
    -tags="purego noasm" \
    -ldflags="-s -w -extldflags=-static" \
    -o "${BIN_NAME}" \
    .

# ---- 5. VERIFICA ----
print_step "Verifica compatibilità del binario"
file "${BIN_NAME}"
echo ""
echo "📄 Dipendenze dinamiche (dovrebbe essere statico):"
ldd "${BIN_NAME}" 2>/dev/null || echo "   ✅ Binario statico (nessuna dipendenza dinamica)"
echo ""
echo "📄 Verifica presenza di istruzioni MMX/SSE (readelf):"
if command -v readelf &>/dev/null; then
    readelf -S "${BIN_NAME}" 2>/dev/null | grep -i "mmx\|sse" || echo "   ✅ Nessuna sezione MMX/SSE rilevata"
else
    echo "   ⚠️ readelf non disponibile, salto verifica"
fi

# ---- 6. CREAZIONE DIRETTORIO PACCHETTO ----
print_step "Creazione directory pacchetto: ${PKG_NAME}"
rm -rf "${PKG_NAME}"
mkdir -p "${PKG_NAME}"

# ---- 7. COPIA FILE NEL PACCHETTO ----
cp "${BIN_NAME}" "${PKG_NAME}/"

[ -f install.sh ] && cp install.sh "${PKG_NAME}/" && chmod +x "${PKG_NAME}/install.sh"
[ -f install_root.sh ] && cp install_root.sh "${PKG_NAME}/" && chmod +x "${PKG_NAME}/install_root.sh"

if [ -f config_prod.json ]; then
    cp config_prod.json "${PKG_NAME}/config.json"
elif [ -f config.json ]; then
    cp config.json "${PKG_NAME}/"
else
    echo "❌ config.json non trovato!"
    exit 1
fi

[ -f mp48webservice.service ] && cp mp48webservice.service "${PKG_NAME}/"

# ════════════════════════════════════════════════════════════
#  IMPORTANTE: In Go 1.15 NON c'è //go:embed,
#  quindi templates e static DEVONO essere copiati nel pacchetto
# ════════════════════════════════════════════════════════════
if [ -d templates ]; then
    cp -r templates "${PKG_NAME}/"
    echo "✅ templates copiato"
fi
if [ -d static ]; then
    cp -r static "${PKG_NAME}/"
    echo "✅ static copiato"
fi

# ---- 8. CREAZIONE ARCHIVIO ----
print_step "Creazione archivio ${OUTPUT_TAR}"
tar -czf "${OUTPUT_TAR}" -C "${PKG_NAME}" .

# ---- 9. RISULTATO ----
echo ""
echo "✅ Pacchetto creato: ${OUTPUT_TAR} ($(du -h ${OUTPUT_TAR} | cut -f1))"
echo ""
echo "Contenuto:"
tar -tzf "${OUTPUT_TAR}" | sed 's/^/   /'
echo ""
echo "▶️  Sulla RTU: tar -xzf ${OUTPUT_TAR} && cd ${PKG_NAME} && sudo ./install.sh"
echo ""
echo "⚠️  Binario compilato con $GO_VERSION"
echo "   - GO386=387 (FPU x87, nessuna MMX/SSE)"
echo "   - Kernel 2.6.32.11 compatibile"
echo "   - templates e static sono copiati nel pacchetto (no //go:embed)"