const state = {
  summary: null,
  history: null,
  hosts: [],
  selectedHostId: null,
};

function getSelectedHostId() {
  const params = new URLSearchParams(window.location.search);
  const hostId = params.get('hostId');
  if (hostId) {
    state.selectedHostId = hostId;
    return hostId;
  }
  return state.selectedHostId || null;
}

function updateQueryHost(hostId) {
  const url = new URL(window.location.href);
  url.searchParams.set('hostId', hostId);
  window.history.replaceState({}, '', url);
}

function formatNumber(value, suffix = '') {
  if (value === null || value === undefined || Number.isNaN(value)) return `0${suffix}`;
  return `${value.toFixed(1)}${suffix}`;
}

function renderHostOptions() {
  const select = document.getElementById('host-select');
  if (!select) return;

  select.innerHTML = '';
  if (!state.hosts || state.hosts.length === 0) {
    const option = document.createElement('option');
    option.value = '';
    option.textContent = 'No hosts';
    select.appendChild(option);
    return;
  }

  const selectedId = getSelectedHostId();
  state.hosts.forEach((host) => {
    const option = document.createElement('option');
    option.value = host.hostId;
    option.textContent = `${host.hostname || host.hostId} • ${host.status === 'online' ? 'Online' : 'Offline'}`;
    if (host.hostId === selectedId) {
      option.selected = true;
    }
    select.appendChild(option);
  });
}

function loadHosts() {
  fetch('/api/hosts', { cache: 'no-store' })
    .then((res) => res.json())
    .then((data) => {
      state.hosts = data || [];
      const selectedId = getSelectedHostId();
      if (!selectedId && state.hosts.length > 0) {
        state.selectedHostId = state.hosts[0].hostId;
      }
      if (state.selectedHostId && !state.hosts.some((h) => h.hostId === state.selectedHostId)) {
        state.selectedHostId = state.hosts[0]?.hostId || null;
      }
      if (state.selectedHostId) {
        updateQueryHost(state.selectedHostId);
      }
      renderHostOptions();
      if (state.selectedHostId) {
        updateSummary();
        updateHistory();
      }
    })
    .catch((err) => console.error('hosts fetch failed', err));
}

function updateSummary() {
  const hostId = getSelectedHostId();
  if (!hostId) {
    document.getElementById('hostname').textContent = 'Unknown';
    document.getElementById('status-badge').textContent = 'Offline';
    document.getElementById('status-badge').className = 'status-badge offline';
    document.getElementById('last-seen').textContent = '-';
    document.getElementById('cpu-value').textContent = '0%';
    document.getElementById('memory-value').textContent = '0%';
    document.getElementById('disk-value').textContent = '0%';
    document.getElementById('network-value').textContent = '0 KB/s / 0 KB/s';
    return;
  }

  fetch(`/api/summary?hostId=${encodeURIComponent(hostId)}`, { cache: 'no-store' })
    .then((res) => {
      if (!res.ok) {
        throw new Error('summary not found');
      }
      return res.json();
    })
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
          li.textContent = `${(a.level || 'INFO').toUpperCase()} - ${a.message}`;
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
  const hostId = getSelectedHostId();
  if (!hostId) return;

  fetch(`/api/history?hostId=${encodeURIComponent(hostId)}&window=1800`, { cache: 'no-store' })
    .then((res) => res.json())
    .then((data) => {
      state.history = data;
      renderChart('cpu-chart', data.cpu || [], '#22c55e');
      renderChart('memory-chart', data.memory || [], '#3b82f6');
    })
    .catch((err) => console.error('history fetch failed', err));
}

document.getElementById('host-select').addEventListener('change', (event) => {
  const hostId = event.target.value;
  if (!hostId) return;
  state.selectedHostId = hostId;
  updateQueryHost(hostId);
  updateSummary();
  updateHistory();
});

setInterval(() => {
  loadHosts();
}, 10000);
setInterval(() => {
  updateSummary();
  updateHistory();
}, 5000);
loadHosts();
