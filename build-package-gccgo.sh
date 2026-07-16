#!/bin/bash
set -e

# ============================================================
#  BUILD PACKAGE CON GCCGO PER RTU i586 SENZA MMX/SSE
#  Kernel 2.6.32.11 compatibile
# ============================================================

# ---- CONFIGURAZIONE ----
GCCGO_BIN="/usr/bin/gccgo-16"   # Percorso assoluto del compilatore
BIN_NAME="MP48WebService"
PKG_NAME="${BIN_NAME}_dist_386_gccgo"
OUTPUT_TAR="${PKG_NAME}.tar.gz"

# ---- FUNZIONI ----
print_step() {
    echo ""
    echo "========================================"
    echo "  $1"
    echo "========================================"
}

# ---- CONTROLLO GCCGO ----
if [ ! -x "$GCCGO_BIN" ]; then
    echo "❌ $GCCGO_BIN non trovato o non eseguibile."
    echo "   Installa: sudo apt install gccgo-16"
    echo "   Oppure verifica il percorso con: which gccgo-16"
    exit 1
fi

export GCCGO="$GCCGO_BIN"   # Imposta la variabile d'ambiente per go build
echo "✅ Usando $($GCCGO_BIN --version | head -1)"

# ---- 1. VENDOR CON GO 1.15 ----
print_step "Sincronizzazione vendor con Go 1.15"
export GOROOT=/usr/local/go1.15
export PATH=$GOROOT/bin:$PATH
rm -rf vendor
go mod tidy
go mod vendor

# ---- 2. COMPILAZIONE CON GCCGO ----
print_step "Compilazione per i586 con gccgo (no MMX/SSE)"

export GOOS=linux
export GOARCH=386
export CGO_ENABLED=0

go build -compiler gccgo \
    -gccgoflags="-march=i586 -mno-mmx -mno-sse -static -static-libgcc -Wl,-static" \
    -mod=vendor \
    -ldflags="-s -w" \
    -o "${BIN_NAME}" \
    .

# ---- 3. VERIFICA DEL BINARIO ----
print_step "Verifica compatibilità del binario"
echo "📄 Informazioni sul binario:"
file "${BIN_NAME}"
echo ""
echo "📄 Dipendenze dinamiche:"
ldd "${BIN_NAME}" 2>/dev/null || echo "   ✅ Binario statico (nessuna dipendenza dinamica)"
echo ""
echo "📄 Controllo istruzioni MMX/SSE:"
if command -v objdump &>/dev/null; then
    if objdump -d "${BIN_NAME}" 2>/dev/null | grep -qi "mmx\|sse"; then
        echo "   ⚠️ Sono state trovate istruzioni MMX/SSE nel binario."
    else
        echo "   ✅ Nessuna istruzione MMX/SSE rilevata."
    fi
else
    echo "   ⚠️ objdump non disponibile, salto verifica."
fi

# ---- 4. CREAZIONE DIRETTORIO PACCHETTO ----
print_step "Creazione directory pacchetto: ${PKG_NAME}"
rm -rf "${PKG_NAME}"
mkdir -p "${PKG_NAME}"

# ---- 5. COPIA FILE NEL PACCHETTO ----
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

# Copia templates e static (necessari per Go 1.15)
if [ -d templates ]; then
    cp -r templates "${PKG_NAME}/"
    echo "✅ templates copiato"
fi
if [ -d static ]; then
    cp -r static "${PKG_NAME}/"
    echo "✅ static copiato"
fi

# ---- 6. CREAZIONE ARCHIVIO ----
print_step "Creazione archivio ${OUTPUT_TAR}"
tar -czf "${OUTPUT_TAR}" -C "${PKG_NAME}" .

# ---- 7. RISULTATO ----
echo ""
echo "✅ Pacchetto creato: ${OUTPUT_TAR} ($(du -h ${OUTPUT_TAR} | cut -f1))"
echo ""
echo "Contenuto:"
tar -tzf "${OUTPUT_TAR}" | sed 's/^/   /'
echo ""
echo "▶️  Sulla RTU: tar -xzf ${OUTPUT_TAR} && cd ${PKG_NAME} && sudo ./install.sh"
echo ""
echo "⚠️  Binario compilato con $(${GCCGO_BIN} --version | head -1)"
echo "   - Flags GCC: -march=i586 -mno-mmx -mno-sse"
echo "   - Kernel 2.6.32.11 compatibile"