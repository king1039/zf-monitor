const state = {
  summary: null,
  history: null,
};

function formatNumber(value, suffix = '') {
  if (value === null || value === undefined || Number.isNaN(value)) return `0${suffix}`;
  return `${value.toFixed(1)}${suffix}`;
}

function updateSummary() {
  fetch('/api/summary', { cache: 'no-store' })
    .then((res) => res.json())
    .then((data) => {
      state.summary = data;
      document.getElementById('hostname').textContent = data.hostname || 'Unknown';
      const statusBadge = document.getElementById('status-badge');
      const online = data.status === 'online';
      statusBadge.textContent = online ? 'Online' : 'Offline';
      statusBadge.className = 'status-badge ' + (online ? 'online' : 'offline');
      document.getElementById('last-seen').textContent = data.lastSeen || '-';
      document.getElementById('cpu-value').textContent = formatNumber(data.cpu, '%');
      document.getElementById('memory-value').textContent = formatNumber(data.memory, '%');
      document.getElementById('disk-value').textContent = formatNumber(data.disk, '%');
      document.getElementById('network-value').textContent = `${formatNumber(data.netUp / 1024, ' KB/s')} / ${formatNumber(data.netDown / 1024, ' KB/s')}`;

      const body = document.getElementById('process-table-body');
      body.innerHTML = '';
      (data.processes || []).slice(0, 8).forEach((p) => {
        const row = document.createElement('tr');
        row.innerHTML = `
          <td>${p.name || '-'}</td>
          <td>${p.pid || '-'}</td>
          <td>${formatNumber(p.cpu, '%')}</td>
          <td>${formatNumber(p.memoryMB, ' MB')}</td>
        `;
        body.appendChild(row);
      });

      const alertList = document.getElementById('alert-list');
      alertList.innerHTML = '';
      if (!data.alerts || data.alerts.length === 0) {
        const li = document.createElement('li');
        li.className = 'alert-normal';
        li.textContent = 'No active alerts';
        alertList.appendChild(li);
      } else {
        data.alerts.forEach((a) => {
          const li = document.createElement('li');
          const level = (a.level || 'normal').toLowerCase();
          li.className = level === 'critical' ? 'alert-critical' : level === 'warning' ? 'alert-warning' : 'alert-normal';
          li.textContent = `${a.level.toUpperCase()} - ${a.message}`;
          alertList.appendChild(li);
        });
      }
    })
    .catch((err) => console.error('summary fetch failed', err));
}

function renderChart(canvasId, points, color) {
  const canvas = document.getElementById(canvasId);
  const ctx = canvas.getContext('2d');
  const width = canvas.width;
  const height = canvas.height;
  ctx.clearRect(0, 0, width, height);

  ctx.strokeStyle = '#e5e7eb';
  ctx.beginPath();
  ctx.moveTo(30, 20);
  ctx.lineTo(30, height - 30);
  ctx.lineTo(width - 10, height - 30);
  ctx.stroke();

  if (!points || points.length === 0) {
    return;
  }

  const values = points.map((p) => p.value);
  const min = Math.min(...values, 0);
  const max = Math.max(...values, 100);
  const range = Math.max(max - min, 1);

  ctx.strokeStyle = color;
  ctx.lineWidth = 2;
  ctx.beginPath();
  points.forEach((p, index) => {
    const x = 30 + (index / Math.max(points.length - 1, 1)) * (width - 40);
    const y = height - 30 - ((p.value - min) / range) * (height - 50);
    if (index === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  });
  ctx.stroke();
}

function updateHistory() {
  fetch('/api/history?window=1800', { cache: 'no-store' })
    .then((res) => res.json())
    .then((data) => {
      state.history = data;
      renderChart('cpu-chart', data.cpu || [], '#22c55e');
      renderChart('memory-chart', data.memory || [], '#3b82f6');
    })
    .catch((err) => console.error('history fetch failed', err));
}

setInterval(updateSummary, 5000);
setInterval(updateHistory, 10000);
updateSummary();
updateHistory();
