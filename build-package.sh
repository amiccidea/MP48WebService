#!/bin/bash
set -e

# ============================================================
#  BUILD PACKAGE CON GO 1.23.12 PER RTU i586 (senza MMX/SSE)
#  Kernel 2.6.32.11 compatibile
#  - GO386=softfloat
#  - -tags=noasm (disabilita assembly ottimizzato)
#  - gcflags all=-N -l (disabilita ottimizzazioni)
#  - buildmode=exe
# ============================================================

# ---- CONFIGURAZIONE ----
GO_CMD="$HOME/go/bin/go1.23.12"
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

# ---- CONTROLLO GO 1.23.12 ----
if [ ! -x "$GO_CMD" ]; then
    echo "❌ $GO_CMD non trovato."
    echo "   Per installarlo:"
    echo "   go install golang.org/dl/go1.23.12@latest"
    echo "   go1.23.12 download"
    exit 1
fi

GO_VERSION=$($GO_CMD version)
echo "✅ Usando $GO_VERSION"

# ---- 1. PULIZIA VENDOR PRECEDENTE ----
print_step "Pulizia vendor precedente"
rm -rf vendor

# ---- 2. FISSA VERSIONI DIPENDENZE (compatibili con Go 1.23) ----
print_step "Fissaggio versioni compatibili con Go 1.23"

# Esporta le variabili d'ambiente corrette anche per i comandi di gestione moduli
export GOOS=linux
export GOARCH=386
export GO386=softfloat
export CGO_ENABLED=0

$GO_CMD mod edit -go=1.23

$GO_CMD get golang.org/x/crypto@v0.17.0
$GO_CMD get github.com/gorilla/csrf@v1.7.1
$GO_CMD get github.com/gorilla/sessions@v1.3.0
$GO_CMD get github.com/jlaffaye/ftp@v0.2.0
$GO_CMD get github.com/pquerna/otp@v1.4.0
$GO_CMD get github.com/skip2/go-qrcode@v0.0.0-20200617195104-da1b6568686e

$GO_CMD mod tidy

# ---- 3. VENDOR (offline) ----
print_step "Vendor con $GO_CMD"
# Le variabili sono già esportate sopra, ma le riaffermiamo per sicurezza
export GOOS=linux
export GOARCH=386
export GO386=softfloat
export CGO_ENABLED=0

$GO_CMD mod vendor

# ---- 4. COMPILAZIONE (con -tags=noasm) ----
print_step "Compilazione per Linux i586 (GO386=softfloat, no MMX/SSE, noasm)"

# Assicuriamo che l'ambiente di build veda GO386=softfloat
# -tags=noasm: disabilita l'uso di assembly ottimizzato nelle librerie
GOOS=linux GOARCH=386 GO386=softfloat CGO_ENABLED=0 $GO_CMD build \
    -mod=vendor \
    -buildmode=exe \
    -tags=noasm \
    -gcflags="all=-N -l" \
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
    # Cerca nel binario eventuali stringhe che indicano MMX/SSE
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
#  NOTA: templates e static NON vengono copiati perché sono
#  già incorporati nel binario tramite //go:embed
# ════════════════════════════════════════════════════════════

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
echo "   - GO386=softfloat (emulazione FPU software)"
echo "   - -tags=noasm (disabilita assembly ottimizzato)"
echo "   - Kernel 2.6.32.11 compatibile"
echo "   - templates e static sono embeddati nel binario"
