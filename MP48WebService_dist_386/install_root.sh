#!/bin/bash
set -e

echo "📦 Installazione MP48WebService (rootless)"

# ---------- VARIABILI ----------
BIN_NAME="MP48WebService"
INSTALL_BASE="${HOME}/MP48WebService"           # Directory base dell'installazione
BIN_PATH="${INSTALL_BASE}/bin/$BIN_NAME"
CONFIG_DIR="${INSTALL_BASE}/etc"
DATA_DIR="${INSTALL_BASE}/var/data"
LOG_DIR="${INSTALL_BASE}/var/log"
SERVICE_USER="${USER}"                          # Usa l'utente corrente

# ---------- 1. CREAZIONE DIRECTORY ----------
echo "📁 Creazione directory..."
mkdir -p "${INSTALL_BASE}"
mkdir -p "${CONFIG_DIR}"
mkdir -p "${DATA_DIR}"/{data,config_history,uploads}
mkdir -p "${LOG_DIR}"
mkdir -p "$(dirname "$BIN_PATH")"

# ---------- 2. COPIA BINARIO ----------
echo "📄 Copia binario in $BIN_PATH"
cp "$BIN_NAME" "$BIN_PATH"
chmod 755 "$BIN_PATH"

# ---------- 3. COPIA CONFIGURAZIONE ----------
echo "📄 Copia configurazione in $CONFIG_DIR"
cp config.json "$CONFIG_DIR/"
ln -sf "$CONFIG_DIR/config.json" "${INSTALL_BASE}/config.json"

# ---------- 4. COPIA TEMPLATE E STATICI (necessari in Go 1.15) ----------
if [ -d templates ]; then
    echo "📄 Copia templates in $INSTALL_BASE"
    cp -r templates "$INSTALL_BASE/"
fi
if [ -d static ]; then
    echo "📄 Copia static in $INSTALL_BASE"
    cp -r static "$INSTALL_BASE/"
fi

# ---------- 5. CHIAVE DI CRITTOGRAFIA ----------
if [ -f encryption.key ]; then
    cp encryption.key "$CONFIG_DIR/"
    chmod 600 "$CONFIG_DIR/encryption.key"
else
    echo "⚠️ encryption.key non trovato. Verrà generato al primo avvio."
fi

# ---------- 6. PERMESSI ----------
chmod -R 755 "$INSTALL_BASE"
chmod 700 "$DATA_DIR/data"

# ---------- 7. MESSAGGIO FINALE ----------
echo ""
echo "✅ Installazione completata!"
echo ""
echo "📁 Tutti i file sono stati installati in: $INSTALL_BASE"
echo ""
echo "▶️ Per avviare il servizio in primo piano:"
echo "   $BIN_PATH"
echo ""
echo "▶️ Per avviarlo in background (nohup):"
echo "   nohup $BIN_PATH > ${LOG_DIR}/output.log 2>&1 &"
echo ""
echo "📋 Per vedere i log:"
echo "   tail -f ${LOG_DIR}/*.log"
echo ""
echo "📁 Configurazione: $CONFIG_DIR"
echo "📁 Dati: $DATA_DIR"
echo "📁 Log: $LOG_DIR"
echo ""
echo "⚠️  Attenzione: se vuoi eseguire il servizio in background permanentemente,"
echo "   aggiungi al crontab: @reboot cd $INSTALL_BASE && ./bin/MP48WebService &"