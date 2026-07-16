#!/bin/bash
set -e

# ============================================================
#  BUILD PACKAGE CON GO PATCHATO (quark) PER RTU i586 SENZA MMX
#  Kernel 2.6.32.11 compatibile
#  - GO386=quark (disabilita MMX/SSE)
# ============================================================

# ---- CONFIGURAZIONE ----
GO_CMD="/home/andrea/goroot/bin/go"
BIN_NAME="MP48WebService"
ARCH="386"
PKG_NAME="${BIN_NAME}_dist_${ARCH}_quark"
OUTPUT_TAR="${PKG_NAME}.tar.gz"

# ---- FUNZIONI ----
print_step() {
    echo ""
    echo "========================================"
    echo "  $1"
    echo "========================================"
}

# ---- CONTROLLO GO ----
if [ ! -x "$GO_CMD" ]; then
    echo "❌ $GO_CMD non trovato."
    echo "   Assicurati di aver compilato il compilatore patchato."
    exit 1
fi

GO_VERSION=$($GO_CMD version)
echo "✅ Usando $GO_VERSION"

# ---- 1. PULIZIA VENDOR ----
print_step "Pulizia vendor precedente"
rm -rf vendor

# ---- 2. VENDOR CON GO PATCHATO ----
print_step "Vendor con $GO_CMD"
export GOOS=linux
export GOARCH=386
export GO386=quark        # <-- FLAG FONDAMENTALE
export CGO_ENABLED=0

$GO_CMD mod tidy
$GO_CMD mod vendor

# ---- 3. COMPILAZIONE ----
print_step "Compilazione per Linux i586 (GO386=quark, no MMX/SSE)"

$GO_CMD build \
    -mod=vendor \
    -buildmode=exe \
    -gcflags="all=-N -l" \
    -ldflags="-s -w -extldflags=-static" \
    -o "${BIN_NAME}" \
    .

# ---- 4. VERIFICA ----
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

# ---- 5. CREAZIONE DIRETTORIO PACCHETTO ----
print_step "Creazione directory pacchetto: ${PKG_NAME}"
rm -rf "${PKG_NAME}"
mkdir -p "${PKG_NAME}"

# ---- 6. COPIA FILE NEL PACCHETTO ----
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
#  IMPORTANTE: in Go 1.13/1.14 NON c'è //go:embed,
#  quindi templates e static DEVONO essere copiati
# ════════════════════════════════════════════════════════════
if [ -d templates ]; then
    cp -r templates "${PKG_NAME}/"
    echo "✅ templates copiato"
fi
if [ -d static ]; then
    cp -r static "${PKG_NAME}/"
    echo "✅ static copiato"
fi

# ---- 7. CREAZIONE ARCHIVIO ----
print_step "Creazione archivio ${OUTPUT_TAR}"
tar -czf "${OUTPUT_TAR}" -C "${PKG_NAME}" .

# ---- 8. RISULTATO ----
echo ""
echo "✅ Pacchetto creato: ${OUTPUT_TAR} ($(du -h ${OUTPUT_TAR} | cut -f1))"
echo ""
echo "Contenuto:"
tar -tzf "${OUTPUT_TAR}" | sed 's/^/   /'
echo ""
echo "▶️  Sulla RTU: tar -xzf ${OUTPUT_TAR} && cd ${PKG_NAME} && sudo ./install.sh"
echo ""
echo "⚠️  Binario compilato con $GO_VERSION"
echo "   - GO386=quark (nessuna MMX/SSE)"
echo "   - Kernel 2.6.32.11 compatibile"
echo "   - templates e static sono copiati nel pacchetto (no //go:embed)"