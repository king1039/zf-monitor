const state = {
  hosts: [],
  selectedHostId: '',
  historyWindow: '3600',
  requestToken: 0,
  activePage: 'hosts',
  selectedDatabaseId: '',
  databaseList: [],
};

const els = {
  hostSelect: document.getElementById('host-select'),
  hostName: document.getElementById('host-name'),
  hostStatusText: document.getElementById('host-status-text'),
  hostStatusPill: document.getElementById('host-status-pill'),
  hostLastSeen: document.getElementById('host-last-seen'),
  overviewTotalHosts: document.getElementById('overview-total-hosts'),
  overviewOnlineHosts: document.getElementById('overview-online-hosts'),
  overviewOfflineHosts: document.getElementById('overview-offline-hosts'),
  overviewHostTableBody: document.getElementById('overview-host-table-body'),
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
  databaseSelect: document.getElementById('database-select'),
  databaseName: document.getElementById('database-name'),
  databaseStatusPill: document.getElementById('database-status-pill'),
  databaseStatusText: document.getElementById('database-status-text'),
  databaseServerName: document.getElementById('database-server-name'),
  databaseAddress: document.getElementById('database-address'),
  databaseVersion: document.getElementById('database-version'),
  databaseEdition: document.getElementById('database-edition'),
  databaseLastSeen: document.getElementById('database-last-seen'),
  databasePageError: document.getElementById('database-page-error'),
  databaseUptime: document.getElementById('database-uptime'),
  databaseConnections: document.getElementById('database-connections'),
  databaseActiveSessions: document.getElementById('database-active-sessions'),
  databaseRunningRequests: document.getElementById('database-running-requests'),
  databaseMaxConnections: document.getElementById('database-max-connections'),
  databaseDatabases: document.getElementById('database-databases'),
  databaseSize: document.getElementById('database-size'),
  databasePort: document.getElementById('database-port'),
  databaseInstanceTableBody: document.getElementById('database-instance-table-body'),
};

function setPageError(message) {
  if (els.pageError) {
    els.pageError.textContent = message;
    els.pageError.classList.remove('hidden');
  }
}

function clearPageError() {
  if (els.pageError) {
    els.pageError.textContent = '';
    els.pageError.classList.add('hidden');
  }
}

function setDatabaseError(message) {
  if (!els.databasePageError) {
    return;
  }
  els.databasePageError.textContent = message;
  els.databasePageError.classList.remove('hidden');
}

function clearDatabaseError() {
  if (!els.databasePageError) {
    return;
  }
  els.databasePageError.textContent = '';
  els.databasePageError.classList.add('hidden');
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

  if (pageName === 'overview') {
    document.getElementById('overview-page').classList.remove('hidden');
    loadOverview();
    return;
  }

  if (pageName === 'database') {
    document.getElementById('database-page').classList.remove('hidden');
    loadDatabases();
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

function formatRelativeTime(timestamp) {
  if (!timestamp) {
    return '-';
  }

  const elapsedSeconds = Math.max(0, Math.floor((Date.now() - new Date(timestamp).getTime()) / 1000));
  if (!Number.isFinite(elapsedSeconds)) {
    return '-';
  }
  if (elapsedSeconds < 60) {
    return `${elapsedSeconds} sec ago`;
  }

  const elapsedMinutes = Math.floor(elapsedSeconds / 60);
  if (elapsedMinutes < 60) {
    return `${elapsedMinutes} min ago`;
  }

  const elapsedHours = Math.floor(elapsedMinutes / 60);
  return `${elapsedHours} hour${elapsedHours === 1 ? '' : 's'} ago`;
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

function renderOverviewRows(hosts, summaries) {
  if (!els.overviewHostTableBody) {
    return;
  }

  els.overviewHostTableBody.innerHTML = '';

  if (!hosts.length) {
    const row = document.createElement('tr');
    const cell = document.createElement('td');
    cell.colSpan = 6;
    cell.className = 'overview-empty-state';
    cell.textContent = 'No monitored hosts available. Waiting for monitoring agents...';
    row.appendChild(cell);
    els.overviewHostTableBody.appendChild(row);
    return;
  }

  hosts.forEach((host, index) => {
    const summary = summaries[index];
    const row = document.createElement('tr');
    const hostnameCell = document.createElement('td');
    const hostLink = document.createElement('button');
    hostLink.type = 'button';
    hostLink.className = 'overview-host-link';
    hostLink.textContent = host.hostname || host.hostId || '-';
    hostLink.addEventListener('click', () => {
      state.selectedHostId = normalizeHostId(host.hostId);
      updateQueryHost(state.selectedHostId);
      switchPage('hosts');
      loadHosts();
    });
    hostnameCell.appendChild(hostLink);
    row.appendChild(hostnameCell);

    const statusCell = document.createElement('td');
    const isOnline = String(host.status || '').toLowerCase() === 'online';
    statusCell.innerHTML = `<span class="overview-status ${isOnline ? 'online' : 'offline'}"><span class="status-dot"></span>${isOnline ? 'Online' : 'Offline'}</span>`;
    row.appendChild(statusCell);

    [summary && summary.cpu, summary && summary.memory, summary && summary.disk].forEach((value) => {
      const cell = document.createElement('td');
      cell.textContent = value !== null && value !== undefined && Number.isFinite(Number(value))
        ? formatPercent(Number(value))
        : '-';
      row.appendChild(cell);
    });

    const lastSeenCell = document.createElement('td');
    lastSeenCell.textContent = formatRelativeTime((summary && summary.lastSeen) || host.lastSeen);
    row.appendChild(lastSeenCell);
    els.overviewHostTableBody.appendChild(row);
  });
}

async function loadOverview() {
  if (state.activePage !== 'overview') {
    return;
  }

  try {
    const hostsResponse = await fetch('/api/hosts', { cache: 'no-store' });
    if (!hostsResponse.ok) {
      throw new Error('Failed to load host overview');
    }

    const hosts = await hostsResponse.json();
    const overviewHosts = Array.isArray(hosts) ? hosts : [];
    const onlineCount = overviewHosts.filter((host) => String(host.status || '').toLowerCase() === 'online').length;
    if (els.overviewTotalHosts) {
      els.overviewTotalHosts.textContent = overviewHosts.length;
      els.overviewOnlineHosts.textContent = onlineCount;
      els.overviewOfflineHosts.textContent = overviewHosts.length - onlineCount;
    }

    const summaryResults = await Promise.allSettled(overviewHosts.map((host) =>
      fetch(`/api/summary?hostId=${encodeURIComponent(host.hostId)}`, { cache: 'no-store' }).then((res) => {
        if (!res.ok) {
          throw new Error(`Summary failed for ${host.hostId}`);
        }
        return res.json();
      })
    ));
    const summaries = summaryResults.map((result) => result.status === 'fulfilled' ? result.value : null);
    renderOverviewRows(overviewHosts, summaries);
  } catch (err) {
    console.error('overview fetch failed', err);
    if (els.overviewTotalHosts) {
      els.overviewTotalHosts.textContent = '0';
      els.overviewOnlineHosts.textContent = '0';
      els.overviewOfflineHosts.textContent = '0';
    }
    if (els.overviewHostTableBody) {
      els.overviewHostTableBody.innerHTML = '';
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 6;
      cell.className = 'overview-empty-state';
      cell.textContent = 'Unable to load host overview.';
      row.appendChild(cell);
      els.overviewHostTableBody.appendChild(row);
    }
  }
}

function setNoDataView() {
  if (els.hostName) {
    els.hostName.textContent = 'Waiting for monitoring agents...';
  }
  if (els.hostStatusText) {
    els.hostStatusText.textContent = 'Offline';
  }
  if (els.hostStatusPill) {
    els.hostStatusPill.className = 'host-status-pill offline';
  }
  if (els.hostLastSeen) {
    els.hostLastSeen.textContent = 'Last Seen: -';
  }

  if (els.cpuValue) {
    els.cpuValue.textContent = '0.0%';
  }
  if (els.memoryValue) {
    els.memoryValue.textContent = '0.0%';
  }
  if (els.diskValue) {
    els.diskValue.textContent = '0.0%';
  }
  if (els.networkValue) {
    els.networkValue.textContent = '';
  }
  if (els.networkUp) {
    els.networkUp.textContent = '0.0 KB/s Up';
  }
  if (els.networkDown) {
    els.networkDown.textContent = '0.0 KB/s Down';
  }

  if (els.cpuProgress) {
    els.cpuProgress.style.width = '0%';
  }
  if (els.memoryProgress) {
    els.memoryProgress.style.width = '0%';
  }
  if (els.diskProgress) {
    els.diskProgress.style.width = '0%';
  }

  if (els.cpuChart) {
    clearHistoryChart();
  }
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
    setPageError('Unable to load monitoring data');
  }
}

function updateHostStatusUI(summary) {
  const status = String(summary.status || 'offline').toLowerCase();
  const isOnline = status === 'online';
  const hostName = summary.hostname || state.selectedHostId || 'Unknown host';

  if (els.hostName) {
    els.hostName.textContent = hostName;
  }
  if (els.hostStatusText) {
    els.hostStatusText.textContent = isOnline ? 'Online' : 'Offline';
  }
  if (els.hostStatusPill) {
    els.hostStatusPill.className = 'host-status-pill ' + (isOnline ? 'online' : 'offline');
  }
  if (els.hostLastSeen) {
    els.hostLastSeen.textContent = summary.lastSeen ? `Last Seen: ${summary.lastSeen}` : 'Last Seen: -';
  }
}

function renderMetrics(summary) {
  const cpu = Number(summary.cpu || 0);
  const memory = Number(summary.memory || 0);
  const disk = Number(summary.disk || 0);
  const netUp = Number(summary.netUp || 0);
  const netDown = Number(summary.netDown || 0);

  if (els.cpuValue) {
    els.cpuValue.textContent = formatPercent(cpu);
  }
  if (els.memoryValue) {
    els.memoryValue.textContent = formatPercent(memory);
  }
  if (els.diskValue) {
    els.diskValue.textContent = formatPercent(disk);
  }
  if (els.networkValue) {
    els.networkValue.textContent = '';
  }
  if (els.networkUp) {
    els.networkUp.textContent = `${formatKbps(netUp)} KB/s Up`;
  }
  if (els.networkDown) {
    els.networkDown.textContent = `${formatKbps(netDown)} KB/s Down`;
  }

  if (els.cpuProgress) {
    els.cpuProgress.style.width = `${Math.min(Math.max(cpu, 0), 100)}%`;
  }
  if (els.memoryProgress) {
    els.memoryProgress.style.width = `${Math.min(Math.max(memory, 0), 100)}%`;
  }
  if (els.diskProgress) {
    els.diskProgress.style.width = `${Math.min(Math.max(disk, 0), 100)}%`;
  }
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

  const min = 0;
  const max = 100;

  ctx.strokeStyle = color;
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
  if (!els.processTableBody) {
    return;
  }

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
  if (!els.alertList) {
    return;
  }

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

function hasValidNumber(value) {
  return value !== null && value !== undefined && Number.isFinite(Number(value));
}

function formatUptime(seconds) {
  if (!hasValidNumber(seconds)) {
    return '-';
  }

  const totalSeconds = Math.max(0, Math.floor(Number(seconds)));
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const secs = totalSeconds % 60;

  if (days > 0) {
    return `${days}d ${hours}h`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${secs}s`;
  }
  return `${secs}s`;
}

function formatDatabaseSizeMB(value) {
  if (!hasValidNumber(value)) {
    return '-';
  }
  const mb = Number(value);
  if (mb < 1024) {
    return `${mb.toFixed(1)} MB`;
  }
  return `${(mb / 1024).toFixed(2)} GB`;
}

function formatIntegerValue(value) {
  if (!hasValidNumber(value)) {
    return '-';
  }
  const number = Number(value);
  if (Number.isInteger(number)) {
    return String(number);
  }
  return Number(number).toFixed(1);
}

function renderDatabaseSelection(instances) {
  if (!els.databaseSelect) {
    return;
  }

  els.databaseSelect.innerHTML = '';

  if (!instances.length) {
    const option = document.createElement('option');
    option.value = '';
    option.textContent = 'No database instances available';
    els.databaseSelect.appendChild(option);
    els.databaseSelect.value = '';
    return;
  }

  instances.forEach((instance) => {
    const option = document.createElement('option');
    option.value = instance.instanceId;
    option.textContent = instance.name || instance.instanceId;
    if (instance.instanceId === state.selectedDatabaseId) {
      option.selected = true;
    }
    els.databaseSelect.appendChild(option);
  });

  if (state.selectedDatabaseId && instances.some((instance) => instance.instanceId === state.selectedDatabaseId)) {
    els.databaseSelect.value = state.selectedDatabaseId;
  } else {
    state.selectedDatabaseId = instances[0].instanceId;
    els.databaseSelect.value = state.selectedDatabaseId;
  }
}

function renderDatabaseInstanceTable(instances) {
  if (!els.databaseInstanceTableBody) {
    return;
  }

  els.databaseInstanceTableBody.innerHTML = '';

  if (!instances.length) {
    const row = document.createElement('tr');
    const cell = document.createElement('td');
    cell.colSpan = 6;
    cell.textContent = 'Waiting for database controller...';
    cell.className = 'database-empty-state';
    row.appendChild(cell);
    els.databaseInstanceTableBody.appendChild(row);
    return;
  }

  instances.forEach((instance) => {
    const row = document.createElement('tr');
    row.className = 'database-instance-row';
    if (instance.instanceId === state.selectedDatabaseId) {
      row.classList.add('selected');
    }
    row.addEventListener('click', () => {
      state.selectedDatabaseId = instance.instanceId;
      renderDatabaseSelection(state.databaseList);
      renderDatabaseInstanceTable(state.databaseList);
      loadDatabaseSummary(instance.instanceId);
    });

    const cells = [
      instance.name || '-',
      instance.type || '-',
      instance.serverName || instance.host || '-',
      instance.host ? `${instance.host}:${instance.port || '-'}` : '-',
      String(instance.status || 'offline').toLowerCase() === 'online' ? 'Online' : 'Offline',
      formatRelativeTime(instance.lastSeen),
    ];

    cells.forEach((cellText) => {
      const cell = document.createElement('td');
      cell.textContent = cellText;
      row.appendChild(cell);
    });

    els.databaseInstanceTableBody.appendChild(row);
  });
}

function setNoDatabaseData() {
  if (els.databaseName) {
    els.databaseName.textContent = 'Waiting for database controller...';
  }
  if (els.databaseStatusText) {
    els.databaseStatusText.textContent = 'Offline';
  }
  if (els.databaseStatusPill) {
    els.databaseStatusPill.className = 'database-status-pill offline';
  }
  if (els.databaseServerName) {
    els.databaseServerName.textContent = '-';
  }
  if (els.databaseAddress) {
    els.databaseAddress.textContent = '-';
  }
  if (els.databaseVersion) {
    els.databaseVersion.textContent = '-';
  }
  if (els.databaseEdition) {
    els.databaseEdition.textContent = '-';
  }
  if (els.databaseLastSeen) {
    els.databaseLastSeen.textContent = '-';
  }

  const emptyValues = [
    els.databaseUptime,
    els.databaseConnections,
    els.databaseActiveSessions,
    els.databaseRunningRequests,
    els.databaseMaxConnections,
    els.databaseDatabases,
    els.databaseSize,
    els.databasePort,
  ];
  emptyValues.forEach((element) => {
    if (element) {
      element.textContent = '-';
    }
  });
}

function renderDatabaseSummary(summary) {
  const isOnline = String(summary.status || 'offline').toLowerCase() === 'online';

  if (els.databaseName) {
    els.databaseName.textContent = summary.name || summary.instanceId || 'Unknown database';
  }
  if (els.databaseStatusText) {
    els.databaseStatusText.textContent = isOnline ? 'Online' : 'Offline';
  }
  if (els.databaseStatusPill) {
    els.databaseStatusPill.className = `database-status-pill ${isOnline ? 'online' : 'offline'}`;
  }
  if (els.databaseServerName) {
    els.databaseServerName.textContent = summary.serverName || '-';
  }
  if (els.databaseAddress) {
    const hasHost = typeof summary.host === 'string' && summary.host.trim() !== '';
    const hasPort = summary.port !== null && summary.port !== undefined && Number.isInteger(Number(summary.port));
    els.databaseAddress.textContent = hasHost && hasPort ? `${summary.host}:${summary.port}` : '-';
  }
  if (els.databaseVersion) {
    els.databaseVersion.textContent = summary.version || '-';
  }
  if (els.databaseEdition) {
    els.databaseEdition.textContent = summary.edition || '-';
  }
  if (els.databaseLastSeen) {
    els.databaseLastSeen.textContent = summary.lastSeen ? formatRelativeTime(summary.lastSeen) : '-';
  }

  if (els.databaseUptime) {
    els.databaseUptime.textContent = hasValidNumber(summary.uptimeSeconds) ? formatUptime(summary.uptimeSeconds) : '-';
  }
  if (els.databaseConnections) {
    els.databaseConnections.textContent = hasValidNumber(summary.connections) ? formatIntegerValue(summary.connections) : '0';
  }
  if (els.databaseActiveSessions) {
    els.databaseActiveSessions.textContent = hasValidNumber(summary.activeSessions) ? formatIntegerValue(summary.activeSessions) : '0';
  }
  if (els.databaseRunningRequests) {
    els.databaseRunningRequests.textContent = hasValidNumber(summary.runningRequests) ? formatIntegerValue(summary.runningRequests) : '0';
  }
  if (els.databaseMaxConnections) {
    els.databaseMaxConnections.textContent = hasValidNumber(summary.maxConnections) ? formatIntegerValue(summary.maxConnections) : '0';
  }
  if (els.databaseDatabases) {
    els.databaseDatabases.textContent = hasValidNumber(summary.databaseCount) ? formatIntegerValue(summary.databaseCount) : '0';
  }
  if (els.databaseSize) {
    els.databaseSize.textContent = hasValidNumber(summary.totalDatabaseSizeMB) ? formatDatabaseSizeMB(summary.totalDatabaseSizeMB) : '0.0 MB';
  }
  if (els.databasePort) {
    els.databasePort.textContent = summary.port !== null && summary.port !== undefined && Number.isInteger(Number(summary.port)) ? String(summary.port) : '-';
  }
}

async function loadDatabases() {
  if (!els.databaseSelect && !els.databaseInstanceTableBody) {
    return;
  }

  try {
    const response = await fetch('/api/databases', { cache: 'no-store' });
    if (!response.ok) {
      throw new Error('Failed to load database list');
    }

    const instances = await response.json();
    state.databaseList = Array.isArray(instances) ? instances : [];
    renderDatabaseSelection(state.databaseList);
    renderDatabaseInstanceTable(state.databaseList);

    if (!state.databaseList.length) {
      state.selectedDatabaseId = '';
      clearDatabaseError();
      setNoDatabaseData();
      return;
    }

    if (!state.selectedDatabaseId || !state.databaseList.some((instance) => instance.instanceId === state.selectedDatabaseId)) {
      state.selectedDatabaseId = state.databaseList[0].instanceId;
      renderDatabaseSelection(state.databaseList);
    }

    clearDatabaseError();
    await loadDatabaseSummary(state.selectedDatabaseId);
  } catch (err) {
    console.error('database list fetch failed', err);
    setDatabaseError('Unable to load database monitoring data');
    setNoDatabaseData();
  }
}

async function loadDatabaseSummary(instanceId) {
  if (!instanceId) {
    setNoDatabaseData();
    return;
  }

  try {
    const response = await fetch(`/api/database/summary?instanceId=${encodeURIComponent(instanceId)}`, { cache: 'no-store' });
    if (!response.ok) {
      throw new Error('Failed to load database summary');
    }

    const summary = await response.json();
    clearDatabaseError();
    renderDatabaseSummary(summary);
  } catch (err) {
    console.error('database summary fetch failed', err);
    setDatabaseError('Unable to load database monitoring data');
    setNoDatabaseData();
  }
}

if (els.databaseSelect) {
  els.databaseSelect.addEventListener('change', (event) => {
    state.selectedDatabaseId = event.target.value;
    renderDatabaseInstanceTable(state.databaseList);
    if (state.selectedDatabaseId) {
      loadDatabaseSummary(state.selectedDatabaseId);
    }
  });
}

if (els.hostSelect) {
  els.hostSelect.addEventListener('change', (event) => {
    updateHostSelection(event.target.value);
  });
}

if (els.historyWindow) {
  els.historyWindow.addEventListener('change', (event) => {
    state.historyWindow = event.target.value;
    if (state.selectedHostId) {
      refreshSelectedHostData();
    }
  });
}

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
  setInterval(() => {
    if (state.activePage === 'overview') {
      loadOverview();
    }
  }, 10000);
  setInterval(() => {
    if (state.activePage === 'database') {
      loadDatabases();
    }
  }, 10000);
});

setNoDataView();
if (els.databasePageError) {
  els.databasePageError.classList.add('hidden');
}
