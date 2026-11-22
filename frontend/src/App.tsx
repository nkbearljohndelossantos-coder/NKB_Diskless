import React, { useState, useEffect } from 'react';
import {
  LayoutDashboard,
  Monitor,
  HardDrive,
  Layers,
  RefreshCw,
  ShieldAlert,
  Server,
  Activity,
  Cpu,
  Clock,
  Plus,
  Power,
  RotateCcw,
  Wrench,
  CheckCircle2,
  AlertTriangle,
  Copy,
  Trash2,
  DownloadCloud
} from 'lucide-react';

interface SystemOverview {
  server_hostname: string;
  server_time: string;
  total_clients: number;
  online_clients: number;
  offline_clients: number;
  booting_clients: number;
  super_clients: number;
  cpu_usage_pct: number;
  ram_usage_pct: number;
  ram_used_gb: number;
  ram_total_gb: number;
  storage_usage_pct: number;
  storage_used_gb: number;
  storage_total_gb: number;
  network_rx_mbs: number;
  network_tx_mbs: number;
  active_iscsi_sessions: number;
  system_alerts: string[];
}

interface Client {
  id: string;
  mac_address: string;
  ip_address: string;
  hostname: string;
  group_id?: string;
  assigned_disk_id?: string;
  status: string;
  is_super_client: boolean;
  boot_mode: string;
  write_cache_mode: string;
  group_name?: string;
  assigned_disk_name?: string;
  created_at: string;
}

interface VirtualDisk {
  id: string;
  name: string;
  description: string;
  file_path: string;
  size_bytes: number;
  disk_format: string;
  is_golden: boolean;
  is_locked: boolean;
  os_type: string;
  created_at: string;
}

interface ClientGroup {
  id: string;
  name: string;
  description: string;
  default_disk_id?: string;
  boot_timeout_seconds: number;
  write_cache_policy: string;
  allow_super_client: boolean;
  client_count?: number;
}

interface AuditLog {
  id: string;
  username: string;
  action: string;
  target_type: string;
  target_id: string;
  details: string;
  ip_address: string;
  created_at: string;
}

export default function App() {
  const [activeTab, setActiveTab] = useState<'overview' | 'clients' | 'disks' | 'groups' | 'updates' | 'audit'>('overview');
  const [overview, setOverview] = useState<SystemOverview | null>(null);
  const [clients, setClients] = useState<Client[]>([]);
  const [disks, setDisks] = useState<VirtualDisk[]>([]);
  const [groups, setGroups] = useState<ClientGroup[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(false);

  // Modals
  const [showAddClient, setShowAddClient] = useState(false);
  const [showAddDisk, setShowAddDisk] = useState(false);
  const [newClient, setNewClient] = useState({ mac: '', hostname: '', group_id: '', disk_id: '' });
  const [newDisk, setNewDisk] = useState({ name: '', size_gb: 50, format: 'VHDX', os_type: 'Windows 11 x64', description: '' });

  // Update manager state
  const [updateVersion, setUpdateVersion] = useState('1.1.0');
  const [updateStatus, setUpdateStatus] = useState<string | null>(null);

  const fetchOverview = async () => {
    try {
      const res = await fetch('/api/v1/system/overview');
      if (res.ok) setOverview(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchClients = async () => {
    try {
      const res = await fetch('/api/v1/clients');
      if (res.ok) setClients(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchDisks = async () => {
    try {
      const res = await fetch('/api/v1/disks');
      if (res.ok) setDisks(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchGroups = async () => {
    try {
      const res = await fetch('/api/v1/groups');
      if (res.ok) setGroups(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchAuditLogs = async () => {
    try {
      const res = await fetch('/api/v1/audit-logs');
      if (res.ok) setAuditLogs(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  useEffect(() => {
    fetchOverview();
    fetchClients();
    fetchDisks();
    fetchGroups();
    fetchAuditLogs();

    const interval = setInterval(() => {
      fetchOverview();
      if (activeTab === 'clients') fetchClients();
    }, 3000);
    return () => clearInterval(interval);
  }, [activeTab]);

  const handleCreateClient = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await fetch('/api/v1/clients', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          mac_address: newClient.mac,
          hostname: newClient.hostname,
          group_id: newClient.group_id || undefined,
          assigned_disk_id: newClient.disk_id || undefined,
          status: 'offline',
          boot_mode: 'UEFI',
          write_cache_mode: 'volatile',
        })
      });
      if (res.ok) {
        setShowAddClient(false);
        setNewClient({ mac: '', hostname: '', group_id: '', disk_id: '' });
        fetchClients();
        fetchOverview();
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleCreateDisk = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await fetch('/api/v1/disks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: newDisk.name,
          size_bytes: newDisk.size_gb * 1024 * 1024 * 1024,
          format: newDisk.format,
          os_type: newDisk.os_type,
          description: newDisk.description,
        })
      });
      if (res.ok) {
        setShowAddDisk(false);
        setNewDisk({ name: '', size_gb: 50, format: 'VHDX', os_type: 'Windows 11 x64', description: '' });
        fetchDisks();
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleToggleSuperClient = async (clientId: string, current: boolean) => {
    try {
      const res = await fetch(`/api/v1/clients/${clientId}/super-client`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enable: !current, expire_minutes: 60 })
      });
      if (res.ok) {
        fetchClients();
        fetchOverview();
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleRebootClient = async (clientId: string) => {
    try {
      await fetch(`/api/v1/clients/${clientId}/reboot`, { method: 'POST' });
      fetchClients();
    } catch (e) {
      console.error(e);
    }
  };

  const handleDeleteClient = async (clientId: string) => {
    if (!confirm('Are you sure you want to remove this client from NKB Diskless management?')) return;
    try {
      await fetch(`/api/v1/clients/${clientId}`, { method: 'DELETE' });
      fetchClients();
      fetchOverview();
    } catch (e) {
      console.error(e);
    }
  };

  const handleDuplicateDisk = async (diskId: string, diskName: string) => {
    const newName = prompt(`Enter name for the cloned disk:`, `${diskName} - Clone`);
    if (!newName) return;
    try {
      const res = await fetch(`/api/v1/disks/${diskId}/duplicate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ new_name: newName })
      });
      if (res.ok) {
        fetchDisks();
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleApplyUpdate = async () => {
    if (!confirm(`Apply update to NKB Diskless v${updateVersion}? An automatic database backup will be created first.`)) return;
    setLoading(true);
    setUpdateStatus('Creating pre-update backup and applying migrations...');
    try {
      const res = await fetch('/api/v1/updates/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version: updateVersion })
      });
      const data = await res.json();
      if (res.ok) {
        setUpdateStatus(`Successfully updated platform to v${data.version}!`);
        fetchAuditLogs();
      } else {
        setUpdateStatus(`Update failed: ${data.error}`);
      }
    } catch (e: any) {
      setUpdateStatus(`Update error: ${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex h-screen bg-slate-100 font-sans text-slate-900">
      {/* 1. Sidebar - Dark Navy Palette */}
      <aside className="w-64 bg-[#0B132B] flex flex-col justify-between shrink-0 select-none">
        <div>
          {/* Brand Header */}
          <div className="p-6 border-b border-slate-800">
            <div className="flex items-center gap-3">
              <div className="w-9 h-9 bg-blue-600 rounded flex items-center justify-center font-bold text-white text-lg tracking-wider">
                NKB
              </div>
              <div>
                <h1 className="text-white font-bold text-sm tracking-wide">DISKLESS SYSTEM</h1>
                <p className="text-slate-400 text-xs font-medium">Manufacturing Platform</p>
              </div>
            </div>
          </div>

          {/* Navigation Items */}
          <nav className="p-3 space-y-1">
            <button
              onClick={() => setActiveTab('overview')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded text-sm font-medium transition-colors ${
                activeTab === 'overview'
                  ? 'bg-blue-600 text-white shadow-sm'
                  : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
              }`}
            >
              <LayoutDashboard className="w-4 h-4 shrink-0" />
              <span>Fleet & Health</span>
            </button>

            <button
              onClick={() => setActiveTab('clients')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded text-sm font-medium transition-colors ${
                activeTab === 'clients'
                  ? 'bg-blue-600 text-white shadow-sm'
                  : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
              }`}
            >
              <Monitor className="w-4 h-4 shrink-0" />
              <div className="flex items-center justify-between w-full">
                <span>Client Workstations</span>
                {overview && (
                  <span className="text-xs px-2 py-0.5 rounded bg-slate-800 text-slate-300 font-mono">
                    {overview.total_clients}
                  </span>
                )}
              </div>
            </button>

            <button
              onClick={() => setActiveTab('disks')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded text-sm font-medium transition-colors ${
                activeTab === 'disks'
                  ? 'bg-blue-600 text-white shadow-sm'
                  : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
              }`}
            >
              <HardDrive className="w-4 h-4 shrink-0" />
              <span>Virtual Disks</span>
            </button>

            <button
              onClick={() => setActiveTab('groups')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded text-sm font-medium transition-colors ${
                activeTab === 'groups'
                  ? 'bg-blue-600 text-white shadow-sm'
                  : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
              }`}
            >
              <Layers className="w-4 h-4 shrink-0" />
              <span>Client Groups</span>
            </button>

            <button
              onClick={() => setActiveTab('updates')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded text-sm font-medium transition-colors ${
                activeTab === 'updates'
                  ? 'bg-blue-600 text-white shadow-sm'
                  : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
              }`}
            >
              <RefreshCw className="w-4 h-4 shrink-0" />
              <span>Update Manager</span>
            </button>

            <button
              onClick={() => setActiveTab('audit')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded text-sm font-medium transition-colors ${
                activeTab === 'audit'
                  ? 'bg-blue-600 text-white shadow-sm'
                  : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
              }`}
            >
              <ShieldAlert className="w-4 h-4 shrink-0" />
              <span>Audit Trail</span>
            </button>
          </nav>
        </div>

        {/* Node Status Footer */}
        <div className="p-4 border-t border-slate-800 text-xs text-slate-400 space-y-2">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
            <span className="text-slate-200 font-medium">Node: Master Primary</span>
          </div>
          <div className="flex justify-between text-slate-400 font-mono text-[11px]">
            <span>PXE: Active (UDP 69)</span>
            <span>SAN: iSCSI</span>
          </div>
        </div>
      </aside>

      {/* 2. Main Content Area */}
      <main className="flex-1 flex flex-col overflow-hidden bg-slate-50">
        {/* Top Navbar */}
        <header className="h-14 bg-white border-b border-slate-200 px-6 flex items-center justify-between shrink-0">
          <div className="flex items-center gap-4">
            <h2 className="text-base font-semibold text-slate-900 capitalize">
              {activeTab === 'overview' && 'System Health & Fleet Telemetry'}
              {activeTab === 'clients' && 'Workstation Fleet Administration'}
              {activeTab === 'disks' && 'Virtual Disk & Master Golden Images'}
              {activeTab === 'groups' && 'Workstation Department Groups'}
              {activeTab === 'updates' && 'NKB Platform Update & Migration Engine'}
              {activeTab === 'audit' && 'Security Audit & Compliance Logs'}
            </h2>
          </div>

          <div className="flex items-center gap-4 text-xs font-medium text-slate-600">
            {overview && (
              <div className="flex items-center gap-2 bg-slate-100 px-3 py-1.5 rounded border border-slate-200">
                <Clock className="w-3.5 h-3.5 text-slate-500" />
                <span>{new Date(overview.server_time).toLocaleTimeString()}</span>
              </div>
            )}
            <div className="flex items-center gap-2">
              <span className="px-2.5 py-1 rounded bg-blue-50 text-blue-700 border border-blue-200 font-semibold">
                NKB v1.0.0 Enterprise
              </span>
            </div>
          </div>
        </header>

        {/* Alerts Banner */}
        {overview && overview.system_alerts.length > 0 && (
          <div className="bg-amber-50 border-b border-amber-200 px-6 py-2.5 flex items-center gap-3 text-amber-800 text-xs font-medium">
            <AlertTriangle className="w-4 h-4 shrink-0 text-amber-600" />
            <div className="flex-1 space-y-0.5">
              {overview.system_alerts.map((alert, idx) => (
                <div key={idx}>{alert}</div>
              ))}
            </div>
          </div>
        )}

        {/* Tab Views */}
        <div className="flex-1 overflow-auto p-6">
          {/* TAB 1: OVERVIEW */}
          {activeTab === 'overview' && overview && (
            <div className="space-y-6">
              {/* Metric Card Row 1: Fleet Status */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>Registered Workstations</span>
                    <Monitor className="w-4 h-4 text-slate-400" />
                  </div>
                  <div className="text-3xl font-bold text-slate-900 font-mono">{overview.total_clients}</div>
                  <div className="mt-2 text-xs text-slate-500 flex items-center gap-2">
                    <span className="text-emerald-600 font-medium">Ready for PXE</span>
                  </div>
                </div>

                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>Online Stations</span>
                    <CheckCircle2 className="w-4 h-4 text-emerald-500" />
                  </div>
                  <div className="text-3xl font-bold text-emerald-600 font-mono">{overview.online_clients}</div>
                  <div className="mt-2 text-xs text-slate-500">Active Windows sessions</div>
                </div>

                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>Offline Stations</span>
                    <Power className="w-4 h-4 text-slate-400" />
                  </div>
                  <div className="text-3xl font-bold text-slate-500 font-mono">{overview.offline_clients}</div>
                  <div className="mt-2 text-xs text-slate-500">Powered off / standby</div>
                </div>

                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>Super-Clients</span>
                    <Wrench className="w-4 h-4 text-blue-500" />
                  </div>
                  <div className="text-3xl font-bold text-blue-600 font-mono">{overview.super_clients}</div>
                  <div className="mt-2 text-xs text-slate-500">Maintenance mode active</div>
                </div>
              </div>

              {/* Metric Card Row 2: Host Hardware Telemetry */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                {/* CPU */}
                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>Server CPU Load</span>
                    <Cpu className="w-4 h-4 text-slate-400" />
                  </div>
                  <div className="text-2xl font-bold text-slate-900 font-mono">
                    {overview.cpu_usage_pct.toFixed(1)}%
                  </div>
                  <div className="w-full bg-slate-100 h-2 rounded mt-3 overflow-hidden">
                    <div
                      className={`h-full ${overview.cpu_usage_pct > 80 ? 'bg-red-500' : 'bg-blue-600'}`}
                      style={{ width: `${Math.min(overview.cpu_usage_pct, 100)}%` }}
                    ></div>
                  </div>
                </div>

                {/* RAM */}
                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>Server RAM Usage</span>
                    <Server className="w-4 h-4 text-slate-400" />
                  </div>
                  <div className="text-2xl font-bold text-slate-900 font-mono">
                    {overview.ram_used_gb.toFixed(1)} / {overview.ram_total_gb.toFixed(0)} GB
                  </div>
                  <div className="w-full bg-slate-100 h-2 rounded mt-3 overflow-hidden">
                    <div
                      className={`h-full ${overview.ram_usage_pct > 85 ? 'bg-amber-500' : 'bg-blue-600'}`}
                      style={{ width: `${Math.min(overview.ram_usage_pct, 100)}%` }}
                    ></div>
                  </div>
                </div>

                {/* Storage */}
                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>SSD Storage Usage</span>
                    <HardDrive className="w-4 h-4 text-slate-400" />
                  </div>
                  <div className="text-2xl font-bold text-slate-900 font-mono">
                    {overview.storage_used_gb.toFixed(0)} / {overview.storage_total_gb.toFixed(0)} GB
                  </div>
                  <div className="w-full bg-slate-100 h-2 rounded mt-3 overflow-hidden">
                    <div
                      className={`h-full ${overview.storage_usage_pct > 80 ? 'bg-amber-500' : 'bg-blue-600'}`}
                      style={{ width: `${Math.min(overview.storage_usage_pct, 100)}%` }}
                    ></div>
                  </div>
                </div>

                {/* Network */}
                <div className="bg-white p-5 rounded border border-slate-200 shadow-sm">
                  <div className="flex items-center justify-between text-slate-500 text-xs font-semibold uppercase tracking-wider mb-2">
                    <span>SAN I/O Throughput</span>
                    <Activity className="w-4 h-4 text-slate-400" />
                  </div>
                  <div className="text-2xl font-bold text-slate-900 font-mono">
                    {overview.network_rx_mbs.toFixed(1)} MB/s
                  </div>
                  <div className="mt-2 text-xs text-slate-500">
                    TX: {overview.network_tx_mbs.toFixed(1)} MB/s | {overview.active_iscsi_sessions} LUN Sessions
                  </div>
                </div>
              </div>

              {/* Quick Action Banner */}
              <div className="bg-white p-6 rounded border border-slate-200 shadow-sm flex items-center justify-between">
                <div>
                  <h3 className="font-semibold text-slate-900 text-sm">Provision Client Workstations</h3>
                  <p className="text-slate-500 text-xs mt-1">
                    Connect factory floor computers via Gigabit Ethernet. Computers automatically appear for assignment upon PXE power-on.
                  </p>
                </div>
                <button
                  onClick={() => setShowAddClient(true)}
                  className="bg-blue-600 hover:bg-blue-700 text-white text-xs font-medium px-4 py-2 rounded flex items-center gap-2"
                >
                  <Plus className="w-4 h-4" />
                  <span>Register Station</span>
                </button>
              </div>
            </div>
          )}

          {/* TAB 2: CLIENT WORKSTATIONS */}
          {activeTab === 'clients' && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-semibold text-slate-900 text-sm">Registered Diskless Workstations</h3>
                  <p className="text-slate-500 text-xs mt-0.5">Manage MAC-to-disk mappings, write-cache modes, and super-client sessions.</p>
                </div>
                <button
                  onClick={() => setShowAddClient(true)}
                  className="bg-blue-600 hover:bg-blue-700 text-white text-xs font-medium px-3.5 py-2 rounded flex items-center gap-1.5"
                >
                  <Plus className="w-4 h-4" />
                  <span>Register Station</span>
                </button>
              </div>

              {/* Workstation Table */}
              <div className="bg-white rounded border border-slate-200 overflow-hidden shadow-sm">
                <table className="w-full text-left text-xs">
                  <thead className="bg-slate-50 border-b border-slate-200 text-slate-600 font-semibold uppercase text-[11px] tracking-wider">
                    <tr>
                      <th className="py-3 px-4">Station Hostname</th>
                      <th className="py-3 px-4">MAC Address</th>
                      <th className="py-3 px-4">IP Address</th>
                      <th className="py-3 px-4">Department / Group</th>
                      <th className="py-3 px-4">Assigned Disk</th>
                      <th className="py-3 px-4">Cache Mode</th>
                      <th className="py-3 px-4">Status</th>
                      <th className="py-3 px-4 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 font-mono">
                    {clients.length === 0 ? (
                      <tr>
                        <td colSpan={8} className="py-8 text-center text-slate-400 font-sans">
                          No workstations registered yet. Boot a client with PXE enabled or click "Register Station".
                        </td>
                      </tr>
                    ) : (
                      clients.map((c) => (
                        <tr key={c.id} className="hover:bg-slate-50/80 transition-colors">
                          <td className="py-3 px-4 font-sans font-medium text-slate-900 flex items-center gap-2">
                            <Monitor className="w-4 h-4 text-slate-400" />
                            <span>{c.hostname}</span>
                            {c.is_super_client && (
                              <span className="px-1.5 py-0.5 rounded text-[10px] bg-blue-100 text-blue-700 font-semibold uppercase">
                                Super-Client
                              </span>
                            )}
                          </td>
                          <td className="py-3 px-4 text-slate-600">{c.mac_address}</td>
                          <td className="py-3 px-4 text-slate-600">{c.ip_address || '—'}</td>
                          <td className="py-3 px-4 font-sans text-slate-700">{c.group_name || 'Standard'}</td>
                          <td className="py-3 px-4 font-sans text-slate-700">{c.assigned_disk_name || 'Unassigned'}</td>
                          <td className="py-3 px-4 font-sans text-slate-600 capitalize">{c.write_cache_mode}</td>
                          <td className="py-3 px-4 font-sans">
                            <span
                              className={`px-2 py-0.5 rounded text-[11px] font-semibold ${
                                c.status === 'online'
                                  ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
                                  : c.status === 'discovered'
                                  ? 'bg-amber-50 text-amber-700 border border-amber-200'
                                  : 'bg-slate-100 text-slate-600'
                              }`}
                            >
                              {c.status}
                            </span>
                          </td>
                          <td className="py-3 px-4 text-right font-sans space-x-1.5">
                            <button
                              onClick={() => handleToggleSuperClient(c.id, c.is_super_client)}
                              title={c.is_super_client ? 'Disable Super-Client' : 'Enter Super-Client Mode'}
                              className={`px-2 py-1 rounded text-xs font-medium border ${
                                c.is_super_client
                                  ? 'bg-amber-50 text-amber-700 border-amber-300'
                                  : 'bg-slate-50 text-slate-700 border-slate-300 hover:bg-slate-100'
                              }`}
                            >
                              <Wrench className="w-3.5 h-3.5 inline mr-1" />
                              {c.is_super_client ? 'Exit Super' : 'Super'}
                            </button>

                            <button
                              onClick={() => handleRebootClient(c.id)}
                              title="Reboot & Purge Volatile Cache"
                              className="px-2 py-1 rounded text-xs font-medium bg-slate-50 hover:bg-slate-100 text-slate-700 border border-slate-300"
                            >
                              <RotateCcw className="w-3.5 h-3.5 inline mr-1" />
                              Reboot
                            </button>

                            <button
                              onClick={() => handleDeleteClient(c.id)}
                              title="Delete Client"
                              className="px-2 py-1 rounded text-xs font-medium bg-red-50 hover:bg-red-100 text-red-700 border border-red-200"
                            >
                              <Trash2 className="w-3.5 h-3.5 inline" />
                            </button>
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 3: VIRTUAL DISKS */}
          {activeTab === 'disks' && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-semibold text-slate-900 text-sm">Virtual Disk Images (SAN Block Storage)</h3>
                  <p className="text-slate-500 text-xs mt-0.5">Centralized master Golden VHDX/RAW images shared across workstations.</p>
                </div>
                <button
                  onClick={() => setShowAddDisk(true)}
                  className="bg-blue-600 hover:bg-blue-700 text-white text-xs font-medium px-3.5 py-2 rounded flex items-center gap-1.5"
                >
                  <Plus className="w-4 h-4" />
                  <span>Create Virtual Disk</span>
                </button>
              </div>

              <div className="bg-white rounded border border-slate-200 overflow-hidden shadow-sm">
                <table className="w-full text-left text-xs">
                  <thead className="bg-slate-50 border-b border-slate-200 text-slate-600 font-semibold uppercase text-[11px] tracking-wider">
                    <tr>
                      <th className="py-3 px-4">Disk Name</th>
                      <th className="py-3 px-4">Format</th>
                      <th className="py-3 px-4">Operating System</th>
                      <th className="py-3 px-4">Virtual Size</th>
                      <th className="py-3 px-4">Status</th>
                      <th className="py-3 px-4">Path</th>
                      <th className="py-3 px-4 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {disks.length === 0 ? (
                      <tr>
                        <td colSpan={7} className="py-8 text-center text-slate-400 font-sans">
                          No virtual disks created yet. Click "Create Virtual Disk" to add an OS image.
                        </td>
                      </tr>
                    ) : (
                      disks.map((d) => (
                        <tr key={d.id} className="hover:bg-slate-50/80 transition-colors">
                          <td className="py-3 px-4 font-semibold text-slate-900 flex items-center gap-2">
                            <HardDrive className="w-4 h-4 text-blue-600" />
                            <span>{d.name}</span>
                          </td>
                          <td className="py-3 px-4 font-mono font-medium text-slate-600">{d.disk_format}</td>
                          <td className="py-3 px-4 text-slate-700">{d.os_type}</td>
                          <td className="py-3 px-4 font-mono text-slate-700">
                            {(d.size_bytes / (1024 * 1024 * 1024)).toFixed(0)} GB
                          </td>
                          <td className="py-3 px-4">
                            <span className="px-2 py-0.5 rounded text-[11px] font-semibold bg-blue-50 text-blue-700 border border-blue-200">
                              {d.is_golden ? 'Master Golden' : 'Standard'}
                            </span>
                          </td>
                          <td className="py-3 px-4 font-mono text-slate-500 text-[11px] truncate max-w-xs" title={d.file_path}>
                            {d.file_path}
                          </td>
                          <td className="py-3 px-4 text-right space-x-1.5">
                            <button
                              onClick={() => handleDuplicateDisk(d.id, d.name)}
                              className="px-2 py-1 rounded text-xs font-medium bg-slate-50 hover:bg-slate-100 text-slate-700 border border-slate-300"
                              title="Duplicate / Clone Disk"
                            >
                              <Copy className="w-3.5 h-3.5 inline mr-1" />
                              Clone
                            </button>
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 4: CLIENT GROUPS */}
          {activeTab === 'groups' && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-semibold text-slate-900 text-sm">Workstation Groups</h3>
                  <p className="text-slate-500 text-xs mt-0.5">Organize workstations by department or factory line.</p>
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                {groups.map((g) => (
                  <div key={g.id} className="bg-white p-5 rounded border border-slate-200 shadow-sm space-y-3">
                    <div className="flex items-center justify-between">
                      <h4 className="font-semibold text-slate-900 text-sm">{g.name}</h4>
                      <span className="text-xs font-mono bg-slate-100 px-2 py-0.5 rounded text-slate-600">
                        {g.client_count || 0} Clients
                      </span>
                    </div>
                    <p className="text-xs text-slate-500">{g.description || 'No description provided.'}</p>
                    <div className="pt-2 border-t border-slate-100 text-xs text-slate-600 space-y-1">
                      <div className="flex justify-between">
                        <span>Write-Cache Policy:</span>
                        <span className="font-medium capitalize">{g.write_cache_policy}</span>
                      </div>
                      <div className="flex justify-between">
                        <span>Boot Timeout:</span>
                        <span className="font-medium">{g.boot_timeout_seconds}s</span>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* TAB 5: UPDATE MANAGER */}
          {activeTab === 'updates' && (
            <div className="space-y-6 max-w-3xl">
              <div className="bg-white p-6 rounded border border-slate-200 shadow-sm space-y-4">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded bg-blue-50 text-blue-600 flex items-center justify-center">
                    <DownloadCloud className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="font-semibold text-slate-900 text-base">NKB Platform Update & Migration Engine</h3>
                    <p className="text-xs text-slate-500">Atomic software updates with automatic pre-backup and instant rollback recovery.</p>
                  </div>
                </div>

                <div className="bg-slate-50 p-4 rounded border border-slate-200 text-xs space-y-2">
                  <div className="flex justify-between">
                    <span className="text-slate-500">Installed Core Version:</span>
                    <span className="font-mono font-bold text-slate-900">v1.0.0 Enterprise</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-500">Database Schema:</span>
                    <span className="text-emerald-700 font-medium">Up to Date (Migration 001)</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-500">Safety Guarantee:</span>
                    <span className="text-slate-700">Pre-update snapshot mandatory (No silent overwrites)</span>
                  </div>
                </div>

                {updateStatus && (
                  <div className="p-3 bg-blue-50 border border-blue-200 text-blue-800 text-xs rounded font-medium">
                    {updateStatus}
                  </div>
                )}

                <div className="pt-2 flex items-center gap-3">
                  <input
                    type="text"
                    value={updateVersion}
                    onChange={(e) => setUpdateVersion(e.target.value)}
                    placeholder="e.g. 1.1.0"
                    className="border border-slate-300 rounded px-3 py-1.5 text-xs font-mono w-32 focus:outline-blue-600"
                  />
                  <button
                    disabled={loading}
                    onClick={handleApplyUpdate}
                    className="bg-blue-600 hover:bg-blue-700 text-white text-xs font-medium px-4 py-2 rounded flex items-center gap-2 disabled:opacity-50"
                  >
                    <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
                    <span>Apply System Update</span>
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* TAB 6: AUDIT LOGS */}
          {activeTab === 'audit' && (
            <div className="space-y-4">
              <div>
                <h3 className="font-semibold text-slate-900 text-sm">Security & Administrative Audit Logs</h3>
                <p className="text-slate-500 text-xs mt-0.5">Immutable record of disk modifications, super-client maintenance, and fleet actions.</p>
              </div>

              <div className="bg-white rounded border border-slate-200 overflow-hidden shadow-sm">
                <table className="w-full text-left text-xs">
                  <thead className="bg-slate-50 border-b border-slate-200 text-slate-600 font-semibold uppercase text-[11px] tracking-wider">
                    <tr>
                      <th className="py-3 px-4">Timestamp</th>
                      <th className="py-3 px-4">Operator</th>
                      <th className="py-3 px-4">Action</th>
                      <th className="py-3 px-4">Target</th>
                      <th className="py-3 px-4">Details</th>
                      <th className="py-3 px-4">IP Address</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 font-mono">
                    {auditLogs.length === 0 ? (
                      <tr>
                        <td colSpan={6} className="py-8 text-center text-slate-400 font-sans">
                          No audit entries recorded yet.
                        </td>
                      </tr>
                    ) : (
                      auditLogs.map((log) => (
                        <tr key={log.id} className="hover:bg-slate-50/80 transition-colors">
                          <td className="py-2.5 px-4 text-slate-500 text-[11px]">
                            {new Date(log.created_at).toLocaleString()}
                          </td>
                          <td className="py-2.5 px-4 font-sans font-medium text-slate-800">{log.username || 'System'}</td>
                          <td className="py-2.5 px-4">
                            <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-slate-100 text-slate-700">
                              {log.action}
                            </span>
                          </td>
                          <td className="py-2.5 px-4 text-slate-600 capitalize">{log.target_type}</td>
                          <td className="py-2.5 px-4 font-sans text-slate-700 text-xs">{log.details}</td>
                          <td className="py-2.5 px-4 text-slate-500 text-[11px]">{log.ip_address || 'local'}</td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      </main>

      {/* MODAL: REGISTER CLIENT */}
      {showAddClient && (
        <div className="fixed inset-0 bg-slate-900/50 flex items-center justify-center p-4 z-50">
          <div className="bg-white rounded-lg border border-slate-200 shadow-xl max-w-md w-full p-6 space-y-4">
            <h3 className="font-semibold text-slate-900 text-sm">Register Workstation</h3>
            <form onSubmit={handleCreateClient} className="space-y-3 text-xs">
              <div>
                <label className="block font-medium text-slate-700 mb-1">Hostname</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. NKB-LINE1-01"
                  value={newClient.hostname}
                  onChange={(e) => setNewClient({ ...newClient, hostname: e.target.value })}
                  className="w-full border border-slate-300 rounded px-3 py-1.5 focus:outline-blue-600"
                />
              </div>

              <div>
                <label className="block font-medium text-slate-700 mb-1">MAC Address</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. 00:1A:2B:3C:4D:5E"
                  value={newClient.mac}
                  onChange={(e) => setNewClient({ ...newClient, mac: e.target.value })}
                  className="w-full border border-slate-300 rounded px-3 py-1.5 font-mono focus:outline-blue-600"
                />
              </div>

              <div>
                <label className="block font-medium text-slate-700 mb-1">Department / Group</label>
                <select
                  value={newClient.group_id}
                  onChange={(e) => setNewClient({ ...newClient, group_id: e.target.value })}
                  className="w-full border border-slate-300 rounded px-3 py-1.5 focus:outline-blue-600"
                >
                  <option value="">Default (Standard Workstations)</option>
                  {groups.map((g) => (
                    <option key={g.id} value={g.id}>{g.name}</option>
                  ))}
                </select>
              </div>

              <div>
                <label className="block font-medium text-slate-700 mb-1">Assigned Virtual Disk</label>
                <select
                  value={newClient.disk_id}
                  onChange={(e) => setNewClient({ ...newClient, disk_id: e.target.value })}
                  className="w-full border border-slate-300 rounded px-3 py-1.5 focus:outline-blue-600"
                >
                  <option value="">Group Default Image</option>
                  {disks.map((d) => (
                    <option key={d.id} value={d.id}>{d.name} ({d.os_type})</option>
                  ))}
                </select>
              </div>

              <div className="pt-3 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setShowAddClient(false)}
                  className="px-3 py-1.5 rounded border border-slate-300 text-slate-700 hover:bg-slate-50"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-3 py-1.5 rounded bg-blue-600 hover:bg-blue-700 text-white font-medium"
                >
                  Save Station
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* MODAL: CREATE VIRTUAL DISK */}
      {showAddDisk && (
        <div className="fixed inset-0 bg-slate-900/50 flex items-center justify-center p-4 z-50">
          <div className="bg-white rounded-lg border border-slate-200 shadow-xl max-w-md w-full p-6 space-y-4">
            <h3 className="font-semibold text-slate-900 text-sm">Create Master Virtual Disk</h3>
            <form onSubmit={handleCreateDisk} className="space-y-3 text-xs">
              <div>
                <label className="block font-medium text-slate-700 mb-1">Image Name</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. Windows 11 Enterprise LTSC 2024"
                  value={newDisk.name}
                  onChange={(e) => setNewDisk({ ...newDisk, name: e.target.value })}
                  className="w-full border border-slate-300 rounded px-3 py-1.5 focus:outline-blue-600"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block font-medium text-slate-700 mb-1">Disk Format</label>
                  <select
                    value={newDisk.format}
                    onChange={(e) => setNewDisk({ ...newDisk, format: e.target.value })}
                    className="w-full border border-slate-300 rounded px-3 py-1.5 focus:outline-blue-600"
                  >
                    <option value="VHDX">VHDX (Dynamic Sparse)</option>
                    <option value="VHD">VHD (Legacy)</option>
                    <option value="RAW">RAW Image</option>
                  </select>
                </div>

                <div>
                  <label className="block font-medium text-slate-700 mb-1">Virtual Size (GB)</label>
                  <input
                    type="number"
                    min="20"
                    max="500"
                    value={newDisk.size_gb}
                    onChange={(e) => setNewDisk({ ...newDisk, size_gb: parseInt(e.target.value) || 50 })}
                    className="w-full border border-slate-300 rounded px-3 py-1.5 font-mono focus:outline-blue-600"
                  />
                </div>
              </div>

              <div>
                <label className="block font-medium text-slate-700 mb-1">Target Operating System</label>
                <input
                  type="text"
                  value={newDisk.os_type}
                  onChange={(e) => setNewDisk({ ...newDisk, os_type: e.target.value })}
                  className="w-full border border-slate-300 rounded px-3 py-1.5 focus:outline-blue-600"
                />
              </div>

              <div>
                <label className="block font-medium text-slate-700 mb-1">Description / Notes</label>
                <textarea
                  rows={2}
                  value={newDisk.description}
                  onChange={(e) => setNewDisk({ ...newDisk, description: e.target.value })}
                  placeholder="e.g. Golden image with manufacturing CAD drivers preloaded"
                  className="w-full border border-slate-300 rounded px-3 py-1.5 focus:outline-blue-600"
                ></textarea>
              </div>

              <div className="pt-3 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setShowAddDisk(false)}
                  className="px-3 py-1.5 rounded border border-slate-300 text-slate-700 hover:bg-slate-50"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-3 py-1.5 rounded bg-blue-600 hover:bg-blue-700 text-white font-medium"
                >
                  Provision Disk
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
