#!/bin/bash
set -e

# ---- CONFIGURAZIONE ----
BIN_NAME="MP48WebService"
PKG_NAME="${BIN_NAME}_dist_386_purego"
OUTPUT_TAR="${PKG_NAME}.tar.gz"

echo "✅ Usando $(go version)"

# ---- VENDOR ----
rm -rf vendor
export GOOS=linux GOARCH=386 GO386=387 CGO_ENABLED=0
go mod tidy
go mod vendor

# ---- COMPILAZIONE CON TAG ----
go build \
    -mod=vendor \
    -tags="purego noasm" \
    -gcflags="all=-N -l" \
    -ldflags="-s -w -extldflags=-static" \
    -o "${BIN_NAME}" \
    .

# ---- VERIFICA ----
echo "📄 Binario generato: ${BIN_NAME}"
if command -v objdump &>/dev/null; then
    if objdump -d "${BIN_NAME}" 2>/dev/null | grep -qi "mmx\|sse"; then
        echo "⚠️ Attenzione: sono state trovate istruzioni MMX/SSE."
    else
        echo "✅ Nessuna istruzione MMX/SSE rilevata."
    fi
else
    echo "⚠️ objdump non disponibile, salto verifica MMX/SSE."
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