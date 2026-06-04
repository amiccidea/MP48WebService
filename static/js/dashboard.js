async function fetchSystemInfo() {
    try {
        const response = await fetch('/api/dashboard');
        if (!response.ok) {
            if (response.status === 401) window.location.href = '/login';
            throw new Error('Errore nel recupero dati');
        }
        const data = await response.json();
        updateUI(data);
    } catch (err) {
        console.error(err);
        const elem = document.getElementById('system-info');
        if (elem) elem.innerHTML = '<p>❌ Errore connessione</p>';
    }
}

function updateUI(data) {
    // Aggiorna solo gli elementi che esistono nel DOM
    const elements = {
        'system-info': `
            <ul class="info-list">
                <li><strong>SO:</strong> <span>${data.GOOS || 'N/A'}</span></li>
                <li><strong>Architettura:</strong> <span>${data.GOARCH || 'N/A'}</span></li>
                <li><strong>Versione Go:</strong> <span>${data.GoVersion || 'N/A'}</span></li>
                <li><strong>GOMAXPROCS:</strong> <span>${data.GOMAXPROCS || '?'}</span></li>
                <li><strong>GOGC:</strong> <span>${data.GOGC || '100'}</span></li>
            </ul>
        `,
        'memory-info': `
            <ul class="info-list">
                <li><strong>Totale:</strong> <span>${data.MemTotal || 'N/A'}</span></li>
                <li><strong>Disponibile:</strong> <span>${data.MemAvailable || 'N/A'}</span></li>
                <li><strong>Swap totale:</strong> <span>${data.SwapTotal || 'N/A'}</span></li>
                <li><strong>Swap libero:</strong> <span>${data.SwapFree || 'N/A'}</span></li>
            </ul>
        `,
        'disk-info': `
            <ul class="info-list">
                <li><strong>Totale:</strong> <span>${data.DiskTotal || 'N/A'}</span></li>
                <li><strong>Usato:</strong> <span>${data.DiskUsed || 'N/A'}</span></li>
                <li><strong>Libero:</strong> <span>${data.DiskFree || 'N/A'}</span></li>
            </ul>
        `,
        'load-info': `
            <ul class="info-list">
                <li><strong>1 minuto:</strong> <span>${data.Load1 || '0'}</span></li>
                <li><strong>5 minuti:</strong> <span>${data.Load5 || '0'}</span></li>
                <li><strong>15 minuti:</strong> <span>${data.Load15 || '0'}</span></li>
            </ul>
        `,
        'uptime-info': `
            <ul class="info-list">
                <li><strong>Uptime:</strong> <span>${data.Uptime || 'N/D'}</span></li>
            </ul>
        `,
        'go-info': `
            <ul class="info-list">
                <li><strong>Go versione:</strong> <span>${data.GoVersion || 'N/A'}</span></li>
                <li><strong>GOMAXPROCS:</strong> <span>${data.GOMAXPROCS || '?'}</span></li>
                <li><strong>GOGC:</strong> <span>${data.GOGC || '100'}</span></li>
            </ul>
        `
    };

    for (const [id, html] of Object.entries(elements)) {
        const el = document.getElementById(id);
        if (el) el.innerHTML = html;
    }
}

fetchSystemInfo();
setInterval(fetchSystemInfo, 10000);