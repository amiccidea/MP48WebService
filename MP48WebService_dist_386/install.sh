#!/bin/bash
set -e

echo "📦 Installazione MP48WebService..."

# 1. Crea le directory necessarie
sudo mkdir -p /etc/MP48WebService
sudo mkdir -p /var/lib/MP48WebService/{data,config_history,uploads}
sudo mkdir -p /var/log/MP48WebService

# 2. Copia il binario
sudo cp MP48WebService /usr/local/bin/
sudo chmod 755 /usr/local/bin/MP48WebService

# 3. Copia la configurazione
sudo cp config.json /etc/MP48WebService/

# 4. Copia la chiave di crittografia (se esiste)
if [ -f encryption.key ]; then
    sudo cp encryption.key /etc/MP48WebService/
    sudo chmod 600 /etc/MP48WebService/encryption.key
else
    echo "⚠️ encryption.key non trovato. Verrà generato al primo avvio."
fi

# 5. Imposta i permessi
sudo chown -R www-data:www-data /etc/MP48WebService /var/lib/MP48WebService /var/log/MP48WebService
sudo chmod 755 /etc/MP48WebService /var/lib/MP48WebService /var/log/MP48WebService
sudo chmod 700 /var/lib/MP48WebService/data

# 6. Installa il servizio systemd (se esiste)
if [ -f mp48webservice.service ]; then
    sudo cp mp48webservice.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable mp48webservice
    echo "✅ Servizio systemd installato e abilitato."
fi

echo "✅ Installazione completata!"
echo "▶️ Per avviare il servizio: sudo systemctl start mp48webservice"
echo "📋 Per vedere i log: sudo journalctl -u mp48webservice -f"