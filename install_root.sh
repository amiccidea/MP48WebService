#!/bin/bash
set -e

echo "📦 Installazione MP48WebService..."

# ---------- VARIABILI ----------
BIN_NAME="MP48WebService"
INSTALL_DIR="/local/etc/web"
BIN_PATH="/local/bin/$BIN_NAME"
CONFIG_DIR="/local/etc/MP48WebService"
DATA_DIR="/local/var/lib/MP48WebService"
LOG_DIR="/local/var/log/MP48WebService"
SERVICE_USER="root"

# ---------- 1. CREAZIONE DIRECTORY /local (se non esiste) ----------
if [ ! -d /local ]; then
    echo "📁 Creazione directory /local..."
    mkdir -p /local
fi

# ---------- 2. CREAZIONE DIRECTORY APPLICAZIONE ----------
echo "📁 Creazione directory applicazione..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"/{data,config_history,uploads}
mkdir -p "$LOG_DIR"

# ---------- 3. COPIA BINARIO ----------
echo "📄 Copia binario in $INSTALL_DIR"
cp "$BIN_NAME" "$INSTALL_DIR/"
chmod 755 "$INSTALL_DIR/$BIN_NAME"

# ---------- 4. COPIA CONFIGURAZIONE ----------
echo "📄 Copia configurazione in $CONFIG_DIR"
cp config.json "$CONFIG_DIR/"

# Link simbolico per la configurazione
ln -sf "$CONFIG_DIR/config.json" "$INSTALL_DIR/config.json"

# ---------- 5. CHIAVE DI CRITTOGRAFIA ----------
if [ -f encryption.key ]; then
    cp encryption.key "$CONFIG_DIR/"
    chmod 600 "$CONFIG_DIR/encryption.key"
else
    echo "⚠️ encryption.key non trovato. Verrà generato al primo avvio."
fi

# ---------- 6. PERMESSI ----------
echo "🔐 Impostazione permessi..."
#chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_DIR" "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR" 2>/dev/null || true
chmod 755 "$INSTALL_DIR" "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
chmod 700 "$DATA_DIR/data"

# Link simbolico in /local/bin
if [ -d /local/bin ]; then
    ln -sf "$INSTALL_DIR/$BIN_NAME" "/local/bin/$BIN_NAME"
else
    mkdir -p /local/bin
    ln -sf "$INSTALL_DIR/$BIN_NAME" "/local/bin/$BIN_NAME"
fi

# ---------- 7. INSTALLAZIONE SERVIZIO ----------
if command -v systemctl &>/dev/null && [ -f mp48webservice.service ]; then
    echo "✅ Rilevato systemd, installo il servizio..."
    sed "s|/etc/MP48WebService|$INSTALL_DIR|g" mp48webservice.service | tee /etc/systemd/system/mp48webservice.service > /dev/null
    systemctl daemon-reload
    systemctl enable mp48webservice
    echo "✅ Servizio systemd installato e abilitato."
    START_CMD="systemctl start mp48webservice"
    LOG_CMD="journalctl -u mp48webservice -f"

elif [ -d /etc/init.d ]; then
    echo "✅ Rilevato init.d, creo script di avvio..."
    tee /etc/init.d/mp48webservice > /dev/null <<'EOF'
#!/bin/sh
### BEGIN INIT INFO
# Provides:          mp48webservice
# Required-Start:    $network $local_fs
# Required-Stop:     $network $local_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: MP48 Web Service
### END INIT INFO

NAME=mp48webservice
DAEMON=/local/opt/MP48WebService/MP48WebService
PIDFILE=/var/run/$NAME.pid
USER=root
WORKDIR=/local/opt/MP48WebService

. /lib/lsb/init-functions

case "$1" in
    start)
        log_daemon_msg "Avvio $NAME"
        start-stop-daemon --start --background --make-pidfile --pidfile $PIDFILE --user $USER --chdir $WORKDIR --exec $DAEMON
        log_end_msg $?
        ;;
    stop)
        log_daemon_msg "Fermo $NAME"
        start-stop-daemon --stop --pidfile $PIDFILE --user $USER
        rm -f $PIDFILE
        log_end_msg $?
        ;;
    restart)
        $0 stop
        sleep 2
        $0 start
        ;;
    status)
        status_of_proc -p $PIDFILE $DAEMON $NAME && exit 0 || exit $?
        ;;
    *)
        echo "Uso: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
exit 0
EOF
    chmod 755 /etc/init.d/mp48webservice
    if command -v update-rc.d &>/dev/null; then
        update-rc.d mp48webservice defaults
        echo "✅ Script init.d installato e abilitato."
    else
        echo "✅ Script init.d installato (ma update-rc.d non trovato)."
    fi
    START_CMD="service mp48webservice start"
    LOG_CMD="tail -f $LOG_DIR/*.log"

else
    echo "⚠️ systemd e init.d non rilevati."
    echo "   Per avviare manualmente: cd $INSTALL_DIR && ./$BIN_NAME &"
    START_CMD="cd $INSTALL_DIR && ./$BIN_NAME &"
    LOG_CMD="tail -f $LOG_DIR/*.log"
fi

# ---------- 8. MESSAGGIO FINALE ----------
echo ""
echo "✅ Installazione completata!"
echo "▶️ Per avviare il servizio: $START_CMD"
[ -n "$LOG_CMD" ] && echo "📋 Per vedere i log: $LOG_CMD"
echo ""
echo "📁 Applicazione: $INSTALL_DIR"
echo "📁 Configurazione: $CONFIG_DIR"
echo "📁 Dati: $DATA_DIR"
echo "📁 Log: $LOG_DIR"
echo ""
echo "⚠️  templates e static sono incorporati nel binario (//go:embed)"