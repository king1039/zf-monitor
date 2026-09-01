const state = {
  hosts: [],
  selectedHostId: '',
  historyWindow: '3600',
  requestToken: 0,
  activePage: 'hosts',
};

const els = {
  hostSelect: document.getElementById('host-select'),
  hostName: document.getElementById('host-name'),
  hostStatusText: document.getElementById('host-status-text'),
  hostStatusPill: document.getElementById('host-status-pill'),
  hostLastSeen: document.getElementById('host-last-seen'),
  globalHostStatus: document.getElementById('global-host-status'),
  pageError: document.getElementById('page-error'),
  historyWindow: document.getElementById('history-window'),
  cpuChart: document.getElementById('cpu-chart'),
  processTableBody: document.getElementById('process-table-body'),
  alertList: document.getElementById('alert-list'),
  cpuValue: document.getElementById('cpu-value'),
  memoryValue: document.getElementById('memory-value'),
  diskValue: document.getElementById('disk-value'),
  networkValue: document.getElementById('network-value'),
  networkUp: document.getElementById('network-up'),
  networkDown: document.getElementById('network-down'),
  cpuProgress: document.getElementById('cpu-progress'),
  memoryProgress: document.getElementById('memory-progress'),
  diskProgress: document.getElementById('disk-progress'),
};

function setPageError(message) {
  els.pageError.textContent = message;
  els.pageError.classList.remove('hidden');
}

function clearPageError() {
  els.pageError.textContent = '';
  els.pageError.classList.add('hidden');
}

function switchPage(pageName) {
  state.activePage = pageName;

  const navItems = document.querySelectorAll('.nav-item');
  navItems.forEach((button) => {
    button.classList.toggle('active', button.dataset.page === pageName);
  });

  const pageViews = document.querySelectorAll('.page-view');
  pageViews.forEach((section) => {
    section.classList.add('hidden');
  });

  if (pageName === 'hosts') {
    document.getElementById('hosts-page').classList.remove('hidden');
    if (state.selectedHostId) {
      refreshSelectedHostData();
    }
    return;
  }

  const targetPage = document.getElementById(`${pageName}-page`);
  if (targetPage) {
    targetPage.classList.remove('hidden');
  }
}

function normalizeHostId(hostId) {
  return (hostId || '').trim();
}

function updateQueryHost(hostId) {
  const url = new URL(window.location.href);
  if (hostId) {
    url.searchParams.set('hostId', hostId);
  } else {
    url.searchParams.delete('hostId');
  }
  window.history.replaceState({}, '', url);
}

function formatPercent(value) {
  if (!Number.isFinite(value)) {
    return '0.0%';
  }
  return `${Number(value).toFixed(1)}%`;
}

function formatKbps(value) {
  if (!Number.isFinite(value)) {
    return '0.0';
  }
  return `${(Number(value) / 1024).toFixed(1)}`;
}

function formatMemoryMB(value) {
  if (!Number.isFinite(value)) {
    return '0.0 MB';
  }
  return `${Number(value).toFixed(1)} MB`;
}

function getHostById(hostId) {
  return state.hosts.find((host) => host.hostId === hostId) || null;
}

function getUrlHostId() {
  const params = new URLSearchParams(window.location.search);
  return normalizeHostId(params.get('hostId'));
}

function renderGlobalStatus() {
  const hosts = state.hosts || [];
  if (!hosts.length) {
    els.globalHostStatus.textContent = '● No monitored hosts available';
    els.globalHostStatus.className = 'global-status offline';
    return;
  }

  const offlineCount = hosts.filter((host) => String(host.status).toLowerCase() !== 'online').length;
  if (offlineCount === 0) {
    els.globalHostStatus.textContent = '● All monitored hosts online';
    els.globalHostStatus.className = 'global-status online';
    return;
  }

  const label = offlineCount === 1 ? '1 host offline' : `${offlineCount} hosts offline`;
  els.globalHostStatus.textContent = `● ${label}`;
  els.globalHostStatus.className = 'global-status offline';
}

function renderHostOptions() {
  if (!els.hostSelect) {
    return;
  }

  const currentValue = normalizeHostId(state.selectedHostId);
  els.hostSelect.innerHTML = '';

  if (!state.hosts.length) {
    const option = document.createElement('option');
    option.value = '';
    option.textContent = 'No hosts available';
    els.hostSelect.appendChild(option);
    els.hostSelect.value = '';
    return;
  }

  state.hosts.forEach((host) => {
    const option = document.createElement('option');
    option.value = host.hostId;
    option.textContent = host.hostname || host.hostId;
    if (host.hostId === currentValue) {
      option.selected = true;
    }
    els.hostSelect.appendChild(option);
  });

  if (currentValue && state.hosts.some((host) => host.hostId === currentValue)) {
    els.hostSelect.value = currentValue;
  } else {
    state.selectedHostId = state.hosts[0].hostId;
    els.hostSelect.value = state.selectedHostId;
    updateQueryHost(state.selectedHostId);
  }
}

function setNoDataView() {
  els.hostName.textContent = 'Waiting for monitoring agents...';
  els.hostStatusText.textContent = 'Offline';
  els.hostStatusPill.className = 'host-status-pill offline';
  els.hostLastSeen.textContent = 'Last Seen: -';

  els.cpuValue.textContent = '0.0%';
  els.memoryValue.textContent = '0.0%';
  els.diskValue.textContent = '0.0%';
  els.networkValue.textContent = '';
  els.networkUp.textContent = '0.0 KB/s Up';
  els.networkDown.textContent = '0.0 KB/s Down';

  els.cpuProgress.style.width = '0%';
  els.memoryProgress.style.width = '0%';
  els.diskProgress.style.width = '0%';

  clearHistoryChart();
  renderProcesses({ processes: [] });
  renderAlerts([]);
}

async function loadHosts() {
  try {
    const data = await fetch('/api/hosts', { cache: 'no-store' }).then((res) => {
      if (!res.ok) {
        throw new Error('Failed to load hosts');
      }
      return res.json();
    });

    state.hosts = Array.isArray(data) ? data : [];
    renderGlobalStatus();

    if (!state.hosts.length) {
      state.selectedHostId = '';
      updateQueryHost('');
      renderHostOptions();
      setNoDataView();
      return;
    }

    const urlHostId = getUrlHostId();
    const currentSelectionExists = state.selectedHostId && getHostById(state.selectedHostId);
    if (urlHostId && getHostById(urlHostId)) {
      state.selectedHostId = urlHostId;
    } else if (!currentSelectionExists) {
      state.selectedHostId = state.hosts[0].hostId;
    }

    updateQueryHost(state.selectedHostId);
    renderHostOptions();
    if (state.selectedHostId) {
      refreshSelectedHostData();
    }
  } catch (err) {
    console.error('hosts fetch failed', err);
    renderGlobalStatus();
    setPageError('Unable to load monitoring data');
  }
}

function updateHostStatusUI(summary) {
  const status = String(summary.status || 'offline').toLowerCase();
  const isOnline = status === 'online';
  const hostName = summary.hostname || state.selectedHostId || 'Unknown host';

  els.hostName.textContent = hostName;
  els.hostStatusText.textContent = isOnline ? 'Online' : 'Offline';
  els.hostStatusPill.className = 'host-status-pill ' + (isOnline ? 'online' : 'offline');
  els.hostLastSeen.textContent = summary.lastSeen ? `Last Seen: ${summary.lastSeen}` : 'Last Seen: -';
}

function renderMetrics(summary) {
  const cpu = Number(summary.cpu || 0);
  const memory = Number(summary.memory || 0);
  const disk = Number(summary.disk || 0);
  const netUp = Number(summary.netUp || 0);
  const netDown = Number(summary.netDown || 0);

  els.cpuValue.textContent = formatPercent(cpu);
  els.memoryValue.textContent = formatPercent(memory);
  els.diskValue.textContent = formatPercent(disk);
  els.networkValue.textContent = '';
  els.networkUp.textContent = `${formatKbps(netUp)} KB/s Up`;
  els.networkDown.textContent = `${formatKbps(netDown)} KB/s Down`;

  els.cpuProgress.style.width = `${Math.min(Math.max(cpu, 0), 100)}%`;
  els.memoryProgress.style.width = `${Math.min(Math.max(memory, 0), 100)}%`;
  els.diskProgress.style.width = `${Math.min(Math.max(disk, 0), 100)}%`;
}

function drawHistoryChart(points, color) {
  const canvas = els.cpuChart;
  const ctx = canvas.getContext('2d');
  const width = canvas.width;
  const height = canvas.height;

  ctx.clearRect(0, 0, width, height);
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, width, height);

  ctx.strokeStyle = '#e5e7eb';
  ctx.lineWidth = 1;
  ctx.beginPath();
  ctx.moveTo(40, 18);
  ctx.lineTo(40, height - 28);
  ctx.lineTo(width - 12, height - 28);
  ctx.stroke();

  if (!Array.isArray(points) || points.length === 0) {
    ctx.fillStyle = '#64748b';
    ctx.font = '14px Segoe UI';
    ctx.fillText('No history data', 46, height / 2);
    return;
  }

  const values = points.map((item) => Number(item.value || 0));
  const min = 0;
  const max = 100;

  const lineColors = ['#2563eb'];
  ctx.strokeStyle = lineColors[0];
  ctx.lineWidth = 2;
  ctx.beginPath();

  points.forEach((point, index) => {
    const x = 40 + (index / Math.max(points.length - 1, 1)) * (width - 52);
    const y = height - 28 - ((Number(point.value || 0) - min) / Math.max(max - min, 1)) * (height - 60);

    if (index === 0) {
      ctx.moveTo(x, y);
    } else {
      ctx.lineTo(x, y);
    }
  });

  ctx.stroke();

  ctx.fillStyle = '#64748b';
  ctx.font = '11px Segoe UI';
  for (let i = 0; i <= 4; i += 1) {
    const value = Math.round((max - i * (max / 4)));
    const y = 20 + ((i / 4) * (height - 48));
    ctx.fillText(`${value}%`, 0, y + 4);
  }

  const sample = points.slice(-6);
  sample.forEach((point, index) => {
    const x = 40 + (index / Math.max(sample.length - 1, 1)) * (width - 52);
    const y = height - 28;
    const ts = new Date(point.timestamp);
    const label = `${String(ts.getHours()).padStart(2, '0')}:${String(ts.getMinutes()).padStart(2, '0')}`;
    ctx.fillStyle = '#64748b';
    ctx.fillText(label, x - 12, y + 16);
  });

  ctx.strokeStyle = color;
}

function clearHistoryChart() {
  const canvas = els.cpuChart;
  const ctx = canvas.getContext('2d');
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = '#64748b';
  ctx.font = '14px Segoe UI';
  ctx.fillText('No history data', 46, canvas.height / 2);
}

function renderHistory(data) {
  const points = Array.isArray(data && data.cpu) ? data.cpu : [];
  drawHistoryChart(points, '#2563eb');
}

function renderProcesses(payload) {
  const processes = Array.isArray(payload && payload.processes) ? payload.processes : [];
  els.processTableBody.innerHTML = '';

  if (!processes.length) {
    const row = document.createElement('tr');
    const cell = document.createElement('td');
    cell.colSpan = 4;
    cell.textContent = 'No process data';
    cell.style.color = '#64748b';
    cell.style.padding = '14px 8px';
    row.appendChild(cell);
    els.processTableBody.appendChild(row);
    return;
  }

  processes.slice(0, 8).forEach((process) => {
    const row = document.createElement('tr');
    row.innerHTML = `
      <td>${process.name || '-'}</td>
      <td>${formatPercent(process.cpu || 0)}</td>
      <td>${formatMemoryMB(process.memoryMB || 0)}</td>
      <td>${process.pid || '-'}</td>
    `;
    els.processTableBody.appendChild(row);
  });
}

function renderAlerts(alerts) {
  els.alertList.innerHTML = '';

  if (!Array.isArray(alerts) || alerts.length === 0) {
    const item = document.createElement('li');
    item.className = 'alert-item normal';
    item.innerHTML = '<span class="alert-icon">✓</span><span>No recent alerts. All clear!</span>';
    els.alertList.appendChild(item);
    return;
  }

  alerts.slice(0, 5).forEach((alert) => {
    const level = String(alert.level || 'warning').toLowerCase();
    const item = document.createElement('li');
    item.className = `alert-item ${level === 'critical' ? 'critical' : 'warning'}`;
    item.innerHTML = `<span class="alert-icon">${level === 'critical' ? '!' : '⚠'}</span><span>${(level || 'WARNING').toUpperCase()} - ${alert.message || 'Alert'}</span>`;
    els.alertList.appendChild(item);
  });
}

function updateHostSelection(hostId) {
  state.selectedHostId = normalizeHostId(hostId);
  updateQueryHost(state.selectedHostId);
  renderHostOptions();
  refreshSelectedHostData();
}

async function refreshSelectedHostData() {
  const hostId = normalizeHostId(state.selectedHostId);
  if (!hostId) {
    setNoDataView();
    return;
  }

  const token = ++state.requestToken;
  const currentHostId = hostId;

  try {
    const [summary, history, processes] = await Promise.all([
      fetch(`/api/summary?hostId=${encodeURIComponent(currentHostId)}`, { cache: 'no-store' }).then((res) => {
        if (!res.ok) {
          throw new Error('summary failed');
        }
        return res.json();
      }),
      fetch(`/api/history?hostId=${encodeURIComponent(currentHostId)}&window=${encodeURIComponent(state.historyWindow)}`, { cache: 'no-store' }).then((res) => {
        if (!res.ok) {
          throw new Error('history failed');
        }
        return res.json();
      }),
      fetch(`/api/processes?hostId=${encodeURIComponent(currentHostId)}`, { cache: 'no-store' }).then((res) => {
        if (!res.ok) {
          throw new Error('processes failed');
        }
        return res.json();
      }),
    ]);

    if (token !== state.requestToken || currentHostId !== normalizeHostId(state.selectedHostId)) {
      return;
    }

    clearPageError();
    updateHostStatusUI(summary);
    renderMetrics(summary);
    renderHistory(history);
    renderProcesses(processes);
    renderAlerts(summary.alerts || []);
  } catch (err) {
    console.error('monitoring data fetch failed', err);
    if (token !== state.requestToken || currentHostId !== normalizeHostId(state.selectedHostId)) {
      return;
    }
    setPageError('Unable to load monitoring data');
    setNoDataView();
  }
}

els.hostSelect.addEventListener('change', (event) => {
  updateHostSelection(event.target.value);
});

els.historyWindow.addEventListener('change', (event) => {
  state.historyWindow = event.target.value;
  if (state.selectedHostId) {
    refreshSelectedHostData();
  }
});

document.querySelectorAll('.nav-item').forEach((button) => {
  button.addEventListener('click', () => {
    const pageName = button.dataset.page;
    switchPage(pageName);
    if (pageName === 'hosts') {
      loadHosts();
    }
  });
});

window.addEventListener('load', () => {
  switchPage('hosts');
  loadHosts();
  setInterval(() => {
    if (state.activePage === 'hosts') {
      loadHosts();
    }
  }, 12000);
  setInterval(() => {
    if (state.activePage === 'hosts' && state.selectedHostId) {
      refreshSelectedHostData();
    }
  }, 5000);
});

setNoDataView();
