#!/bin/bash
set -e

# ============================================================
#  BUILD PACKAGE CON GCCGO-10 DENTRO IL CONTAINER
# ============================================================

GCCGO_BIN="gccgo-10"
BIN_NAME="MP48WebService"
PKG_NAME="${BIN_NAME}_dist_386_gccgo"
OUTPUT_TAR="${PKG_NAME}.tar.gz"

echo "✅ Usando $($GCCGO_BIN --version | head -1)"

# ---- VENDOR ----
rm -rf vendor
go mod tidy
go mod vendor

# ---- COMPILAZIONE CON GCCGO ----
export GOOS=linux
export GOARCH=386
export CGO_ENABLED=0
export GCCGO="$GCCGO_BIN"   # <-- IMPOSTA IL COMPILATORE

go build -compiler gccgo \
    -gccgoflags="-march=i586 -mno-mmx -mno-sse -static -static-libgcc -Wl,-static" \
    -mod=vendor \
    -ldflags="-s -w" \
    -o "${BIN_NAME}" \
    .

# ---- VERIFICA ----
echo "📄 Verifica binario:"
file "${BIN_NAME}"
if objdump -d "${BIN_NAME}" 2>/dev/null | grep -qi "mmx\|sse"; then
    echo "⚠️ Attenzione: sono state trovate istruzioni MMX/SSE."
else
    echo "✅ Nessuna istruzione MMX/SSE rilevata."
fi

# ---- PACCHETTO ----
rm -rf "$PKG_NAME"
mkdir -p "$PKG_NAME"
cp "${BIN_NAME}" "$PKG_NAME/"
[ -f install.sh ] && cp install.sh "$PKG_NAME/" && chmod +x "$PKG_NAME/install.sh"
[ -f install_root.sh ] && cp install_root.sh "$PKG_NAME/" && chmod +x "$PKG_NAME/install_root.sh"
[ -f config_prod.json ] && cp config_prod.json "$PKG_NAME/config.json" || cp config.json "$PKG_NAME/"
cp -r templates "$PKG_NAME/"
cp -r static "$PKG_NAME/"

tar -czf "$OUTPUT_TAR" -C "$PKG_NAME" .

echo "✅ Pacchetto creato: $OUTPUT_TAR"
echo "Contenuto:"
tar -tzf "$OUTPUT_TAR" | sed 's/^/   /'