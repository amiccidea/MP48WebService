document.addEventListener('DOMContentLoaded', () => {
    const form = document.getElementById('logFilterForm');
    const categorySelect = document.getElementById('logCategory');
    const startDateInput = document.getElementById('startDate');
    const endDateInput = document.getElementById('endDate');
    const tbody = document.getElementById('logsTableBody');
    const paginationDiv = document.getElementById('pagination');
    const resetBtn = document.getElementById('resetFiltersBtn');

    let currentFilters = { category: 'all', start_date: '', end_date: '' };
    let currentPage = 1;
    let totalPages = 1;
    let isAdmin = false;
    const pageSize = 50;

    function escapeHtml(str) {
        if (!str) return '';
        return str.replace(/[&<>]/g, function(m) {
            if (m === '&') return '&amp;';
            if (m === '<') return '&lt;';
            if (m === '>') return '&gt;';
            return m;
        });
    }

    function loadCategories() {
        fetch('/api/logs?category=all')
            .then(res => res.json())
            .then(data => {
                if (data.categories && Array.isArray(data.categories)) {
                    categorySelect.innerHTML = '';
                    data.categories.forEach(cat => {
                        const option = document.createElement('option');
                        option.value = cat;
                        option.textContent = cat === 'all' ? 'Tutte le categorie' : cat;
                        categorySelect.appendChild(option);
                    });
                }
            })
            .catch(err => console.error('Errore categorie:', err));
    }

    function loadLogs(page) {
        currentPage = page;
        let url = `/api/logs?category=${encodeURIComponent(currentFilters.category)}&page=${page}&pageSize=${pageSize}`;
        if (currentFilters.start_date) url += `&start_date=${currentFilters.start_date}`;
        if (currentFilters.end_date) url += `&end_date=${currentFilters.end_date}`;

        tbody.innerHTML = '<tr><td colspan="5">Caricamento...</td></tr>';
        fetch(url)
            .then(res => res.json())
            .then(data => {
                const logs = data.logs;
                totalPages = data.totalPages;
                isAdmin = data.isAdmin;
                if (!logs || logs.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="5">Nessun log trovato</td></tr>';
                    renderPagination();
                    return;
                }
                let html = '';
                logs.forEach(log => {
                    html += `
                        <tr>
                            <td>${escapeHtml(log.category)}</td>
                            <td><strong>${escapeHtml(log.name)}</strong><br><small>${escapeHtml(log.path)}</small></td>
                            <td>${escapeHtml(log.size)}</td>
                            <td>${escapeHtml(log.mod_time)}</td>
                            <td>
                                <a href="/logs/download?path=${encodeURIComponent(log.path)}">📥 Scarica</a>
                                ${isAdmin ? ` <button class="delete-log-btn" data-path="${encodeURIComponent(log.path)}">🗑️ Elimina</button>` : ''}
                            </td>
                        </tr>
                    `;
                });
                tbody.innerHTML = html;
                if (isAdmin) {
                    document.querySelectorAll('.delete-log-btn').forEach(btn => {
                        btn.addEventListener('click', (e) => {
                            const encodedPath = btn.dataset.path;
                            const path = decodeURIComponent(encodedPath);
                            if (confirm('Sei sicuro di voler eliminare questo file di log?')) {
                                fetch('/logs/delete', {
                                    method: 'POST',
                                    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                                    body: 'path=' + encodeURIComponent(path)
                                }).then(resp => {
                                    if (resp.ok) {
                                        alert('File eliminato');
                                        loadLogs(currentPage);
                                    } else {
                                        alert('Errore durante l\'eliminazione');
                                    }
                                }).catch(err => alert('Errore di rete'));
                            }
                        });
                    });
                }
                renderPagination();
            })
            .catch(err => {
                console.error(err);
                tbody.innerHTML = '<tr><td colspan="5">❌ Errore nel caricamento dei log</td></tr>';
            });
    }

    function renderPagination() {
        if (!paginationDiv) return;
        if (totalPages <= 1) {
            paginationDiv.innerHTML = '';
            return;
        }
        let html = '<div style="display: flex; gap: 8px; justify-content: center; margin-top: 20px; flex-wrap: wrap;">';
        if (currentPage > 1) {
            html += `<button onclick="window.loadPage(${currentPage - 1})">◀ Precedente</button>`;
        }
        for (let i = 1; i <= totalPages; i++) {
            if (i === currentPage) {
                html += `<button disabled style="background: #0070c0; color: white;">${i}</button>`;
            } else if (i === 1 || i === totalPages || (i >= currentPage - 2 && i <= currentPage + 2)) {
                html += `<button onclick="window.loadPage(${i})">${i}</button>`;
            } else if (i === currentPage - 3 || i === currentPage + 3) {
                html += `<span style="padding: 0 4px;">...</span>`;
            }
        }
        if (currentPage < totalPages) {
            html += `<button onclick="window.loadPage(${currentPage + 1})">Successivo ▶</button>`;
        }
        html += '</div>';
        paginationDiv.innerHTML = html;
    }

    window.loadPage = function(page) {
        if (page >= 1 && page <= totalPages) loadLogs(page);
    };

    if (form) {
        form.addEventListener('submit', (e) => {
            e.preventDefault();
            currentFilters = {
                category: categorySelect.value,
                start_date: startDateInput.value,
                end_date: endDateInput.value
            };
            loadLogs(1);
        });
    }
    if (resetBtn) {
        resetBtn.addEventListener('click', () => {
            categorySelect.value = 'all';
            startDateInput.value = '';
            endDateInput.value = '';
            currentFilters = { category: 'all', start_date: '', end_date: '' };
            loadLogs(1);
        });
    }

    loadCategories();
    loadLogs(1);
});