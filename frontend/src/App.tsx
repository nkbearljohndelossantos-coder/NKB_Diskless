import React, { useState, useEffect } from 'react';
import {
  LayoutDashboard,
  Monitor,
  Radio,
  HardDrive,
  Shield,
  Layers,
  Server,
  Activity,
  Database,
  Wrench,
  GitBranch,
  Network,
  Cpu,
  ShieldAlert,
  Settings,
  RefreshCw,
  Power,
  RotateCcw,
  Lock,
  Search,
  Plus,
  Trash2,
  Copy,
  DownloadCloud,
  CheckCircle2,
  AlertTriangle,
  Moon,
  Sun,
  Terminal,
  ExternalLink,
  FileText,
  Play,
  Check,
  X,
  Users,
  LogOut
} from 'lucide-react';
import NetworkManagement from './components/NetworkManagement';

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
  hardware_info: string;
  group_name?: string;
  assigned_disk_name?: string;
  created_at: string;
  last_seen?: string;

  // Telemetry
  logged_in_user?: string;
  os_version?: string;
  uptime_seconds?: number;
  agent_status?: string;
  agent_version?: string;
  cpu_usage_pct?: number;
  ram_usage_pct?: number;
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
  checksum_sha256?: string;
  created_at: string;
}

interface DiskVersion {
  id: string;
  disk_id: string;
  version_number: number;
  version_tag: string;
  snapshot_path: string;
  description: string;
  checksum_sha256: string;
  created_by?: string;
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

interface RemoteCommand {
  id: string;
  client_id: string;
  issuer_username: string;
  command_type: string;
  status: string;
  created_at: string;
  result?: {
    exit_code: number;
    output: string;
    error_message: string;
  };
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

type NavTab =
  | 'overview'
  | 'clients'
  | 'remote'
  | 'disks'
  | 'golden'
  | 'assignments'
  | 'pxe'
  | 'iscsi'
  | 'cache'
  | 'super'
  | 'versions'
  | 'network'
  | 'hardware'
  | 'groups'
  | 'audit'
  | 'updates'
  | 'settings';

export default function App() {
  const [activeTab, setActiveTab] = useState<NavTab>('overview');
  const [overview, setOverview] = useState<SystemOverview | null>(null);
  const [clients, setClients] = useState<Client[]>([]);
  const [disks, setDisks] = useState<VirtualDisk[]>([]);
  const [groups, setGroups] = useState<ClientGroup[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [diskVersions, setDiskVersions] = useState<{ [diskId: string]: DiskVersion[] }>({});
  const [theme, setTheme] = useState<'dark' | 'light'>('light');
  const [wsConnected, setWsConnected] = useState<boolean>(false);

  // Search & Filter State
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedGroup, setSelectedGroup] = useState('');
  const [selectedStatus, setSelectedStatus] = useState('');

  // Modals & Selected items
  const [showAddClient, setShowAddClient] = useState(false);
  const [showAddDisk, setShowAddDisk] = useState(false);
  const [selectedClientForRemote, setSelectedClientForRemote] = useState<Client | null>(null);
  const [commandHistory, setCommandHistory] = useState<RemoteCommand[]>([]);
  const [confirmAction, setConfirmAction] = useState<{ client: Client; action: 'shutdown' | 'restart' | 'lock' | 'wol' } | null>(null);
  const [actionNotice, setActionNotice] = useState<string | null>(null);

  // Super-Client Form
  const [commitModal, setCommitModal] = useState<Client | null>(null);
  const [commitVersionTag, setCommitVersionTag] = useState('v1.1');
  const [commitReleaseNotes, setCommitReleaseNotes] = useState('');

  // Disk Assignment Modal
  const [assignModal, setAssignModal] = useState<{ client: Client; diskId: string; mode: string } | null>(null);

  // OS Image Builder State
  const [showApplyOSModal, setShowApplyOSModal] = useState<VirtualDisk | null>(null);
  const [installerList, setInstallerList] = useState<{ name: string; path: string; size_bytes: number; type: string; editions?: { index: number; name: string; description: string }[] }[]>([]);
  const [selectedInstallerPath, setSelectedInstallerPath] = useState('');
  const [availableEditions, setAvailableEditions] = useState<{ index: number; name: string; description: string }[]>([]);
  const [wimIndex, setWimIndex] = useState(1);
  const [isApplyingOS, setIsApplyingOS] = useState(false);

  // Forms State
  const [newClient, setNewClient] = useState({ mac: '', hostname: '', group_id: '', disk_id: '' });
  const [newDisk, setNewDisk] = useState({ name: '', size_gb: 50, format: 'VHDX', os_type: 'Windows 11 Pro x64', description: '' });

  // Update manager state
  const [updateStatus, setUpdateStatus] = useState<string | null>(null);
  const [installedVersion, setInstalledVersion] = useState<string>('v1.0.0');
  const [updateLoading, setUpdateLoading] = useState<boolean>(false);

  // Authentication State
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(true);
  const [loginUsername, setLoginUsername] = useState('admin');
  const [loginPassword, setLoginPassword] = useState('');
  const [loginError, setLoginError] = useState<string | null>(null);
  const [isLoggingIn, setIsLoggingIn] = useState(false);
  const [currentUser, setCurrentUser] = useState<{ id?: string; username: string; role: string } | null>(null);

  const showNotification = (msg: string) => {
    setActionNotice(msg);
    setTimeout(() => setActionNotice(null), 4000);
  };

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoggingIn(true);
    setLoginError(null);
    try {
      const res = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: loginUsername, password: loginPassword })
      });
      const data = await res.json();
      if (res.ok) {
        setIsAuthenticated(true);
        setCurrentUser(data.user || { username: loginUsername, role: 'admin' });
        setLoginPassword('');
        fetchOverview();
        fetchClients();
        fetchDisks();
        fetchGroups();
        fetchAuditLogs();
      } else {
        setLoginError(data.error || 'Authentication failed');
      }
    } catch (err: any) {
      setLoginError(err.message || 'Network error connecting to management server');
    } finally {
      setIsLoggingIn(false);
    }
  };

  const handleLogout = async () => {
    try {
      await fetch('/api/v1/auth/logout', { method: 'POST' });
    } catch (e) {
      console.error(e);
    }
    setIsAuthenticated(false);
    setCurrentUser(null);
  };

  const fetchOverview = async () => {
    try {
      const res = await fetch('/api/v1/system/overview');
      if (res.status === 401) {
        setIsAuthenticated(false);
        return;
      }
      if (res.ok) setOverview(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchClients = async () => {
    try {
      const params = new URLSearchParams();
      if (searchQuery) params.append('search', searchQuery);
      if (selectedGroup) params.append('group_id', selectedGroup);
      if (selectedStatus) params.append('status', selectedStatus);

      const res = await fetch(`/api/v1/workstations?${params.toString()}`);
      if (res.status === 401) {
        setIsAuthenticated(false);
        return;
      }
      if (res.ok) setClients(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchDisks = async () => {
    try {
      const res = await fetch('/api/v1/disks');
      if (res.status === 401) {
        setIsAuthenticated(false);
        return;
      }
      if (res.ok) {
        const data: VirtualDisk[] = await res.json();
        setDisks(data);
        data.forEach(d => fetchVersions(d.id));
      }
    } catch (e) {
      console.error(e);
    }
  };

  const fetchVersions = async (diskId: string) => {
    try {
      const res = await fetch(`/api/v1/disks/${diskId}/versions`);
      if (res.status === 401) {
        setIsAuthenticated(false);
        return;
      }
      if (res.ok) {
        const versions: DiskVersion[] = await res.json();
        setDiskVersions(prev => ({ ...prev, [diskId]: versions }));
      }
    } catch (e) {
      console.error(e);
    }
  };

  const fetchGroups = async () => {
    try {
      const res = await fetch('/api/v1/groups');
      if (res.status === 401) {
        setIsAuthenticated(false);
        return;
      }
      if (res.ok) setGroups(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchAuditLogs = async () => {
    try {
      const res = await fetch('/api/v1/audit-logs');
      if (res.status === 401) {
        setIsAuthenticated(false);
        return;
      }
      if (res.ok) setAuditLogs(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchCommandHistory = async (clientId: string) => {
    try {
      const res = await fetch(`/api/v1/workstations/${clientId}/commands`);
      if (res.status === 401) {
        setIsAuthenticated(false);
        return;
      }
      if (res.ok) setCommandHistory(await res.json());
    } catch (e) {
      console.error(e);
    }
  };

  const fetchVersion = async () => {
    try {
      const res = await fetch('/api/v1/updates/version');
      if (res.ok) {
        const data = await res.json();
        if (data.current_version) {
          setInstalledVersion(data.current_version);
        }
      }
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
    fetchVersion();

    const interval = setInterval(() => {
      fetchOverview();
      if (activeTab === 'clients' || activeTab === 'remote' || activeTab === 'super') fetchClients();
    }, 3000);
    return () => clearInterval(interval);
  }, [activeTab, searchQuery, selectedGroup, selectedStatus]);

  // WebSocket Live Telemetry Connection
  useEffect(() => {
    let ws: WebSocket | null = null;
    let reconnectTimer: any = null;
    let isUnmounted = false;

    const connectWS = () => {
      try {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host || 'localhost:8443';
        const url = `${proto}//${host}/api/v1/ws`;

        ws = new WebSocket(url);

        ws.onopen = () => {
          if (!isUnmounted) setWsConnected(true);
        };

        ws.onmessage = (event) => {
          try {
            const lines = event.data.split('\n');
            for (const line of lines) {
              if (!line.trim()) continue;
              const data = JSON.parse(line);
              if (data.type === 'telemetry.updated') {
                const tel = data.payload;
                setClients(prev => prev.map(c => {
                  if (c.id === tel.client_id) {
                    return {
                      ...c,
                      status: 'online',
                      logged_in_user: tel.logged_in_user || c.logged_in_user,
                      uptime_seconds: tel.uptime_seconds ?? c.uptime_seconds,
                      cpu_usage_pct: tel.cpu_usage_pct ?? c.cpu_usage_pct,
                      ram_usage_pct: tel.ram_usage_pct ?? c.ram_usage_pct,
                      agent_status: tel.agent_status || c.agent_status,
                      last_seen: tel.last_seen || new Date().toISOString()
                    };
                  }
                  return c;
                }));
              } else if (data.type === 'workstation.registered' || data.type === 'workstation.updated' || data.type === 'workstation.offline') {
                fetchClients();
                fetchOverview();
              } else if (data.type === 'command.updated') {
                if (selectedClientForRemote) {
                  fetchCommandHistory(selectedClientForRemote.id);
                }
              }
            }
          } catch (e) {
            console.error('Error parsing WS message', e);
          }
        };

        ws.onclose = () => {
          if (!isUnmounted) {
            setWsConnected(false);
            reconnectTimer = setTimeout(connectWS, 3000);
          }
        };

        ws.onerror = () => {
          if (ws) ws.close();
        };
      } catch (e) {
        if (!isUnmounted) {
          setWsConnected(false);
          reconnectTimer = setTimeout(connectWS, 5000);
        }
      }
    };

    connectWS();

    return () => {
      isUnmounted = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      if (ws) ws.close();
    };
  }, [selectedClientForRemote]);

  // Handlers
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
        showNotification(`Workstation ${newClient.hostname} successfully registered.`);
      } else {
        const err = await res.json().catch(() => ({ error: 'Registration failed' }));
        showNotification(`Registration Error: ${err.error || 'Failed to register client'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while registering workstation.');
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
        setNewDisk({ name: '', size_gb: 50, format: 'VHDX', os_type: 'Windows 11 Pro x64', description: '' });
        fetchDisks();
        showNotification(`Virtual Disk ${newDisk.name} created.`);
      } else {
        const err = await res.json().catch(() => ({ error: 'Creation failed' }));
        showNotification(`Disk Creation Error: ${err.error || 'Failed to create disk'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while creating virtual disk.');
    }
  };

  const handleSendRemoteCommand = async (clientId: string, commandType: string) => {
    try {
      const res = await fetch(`/api/v1/workstations/${clientId}/command`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command_type: commandType })
      });
      if (res.ok) {
        setConfirmAction(null);
        fetchClients();
        fetchAuditLogs();
        if (selectedClientForRemote && selectedClientForRemote.id === clientId) {
          fetchCommandHistory(clientId);
        }
        showNotification(`Dispatched remote command [${commandType.toUpperCase()}] to workstation.`);
      } else {
        setConfirmAction(null);
        const err = await res.json().catch(() => ({ error: 'Command rejected' }));
        showNotification(`Command Error: ${err.error || 'Failed to dispatch command'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while dispatching command.');
    }
  };

  const handleSendWOL = async (clientId: string, hostname: string) => {
    try {
      const res = await fetch(`/api/v1/workstations/${clientId}/wol`, { method: 'POST' });
      if (res.ok) {
        showNotification(`Wake-on-LAN magic packet sent to ${hostname}.`);
        fetchAuditLogs();
      } else {
        const err = await res.json().catch(() => ({ error: 'Operation rejected' }));
        showNotification(`Wake-on-LAN Blocked: ${err.error || 'DISABLED_BY_SAFETY_POLICY'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while sending Wake-on-LAN.');
    }
  };

  const handleDownloadRDP = (clientId: string) => {
    window.open(`/api/v1/workstations/${clientId}/rdp`, '_blank');
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
        showNotification(!current ? 'Super-Client maintenance mode activated with exclusive lock.' : 'Super-Client session discarded.');
      } else {
        const err = await res.json();
        alert(`Super-Client Error: ${err.error || 'Failed to toggle mode'}`);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleCommitSuperClient = async () => {
    if (!commitModal) return;
    try {
      const res = await fetch('/api/v1/super-client/commit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          client_id: commitModal.id,
          version_tag: commitVersionTag,
          release_notes: commitReleaseNotes
        })
      });
      if (res.ok) {
        const ver = await res.json();
        setCommitModal(null);
        setCommitVersionTag('v1.1');
        setCommitReleaseNotes('');
        fetchClients();
        fetchDisks();
        fetchAuditLogs();
        showNotification(`Sealed version ${ver.version_tag} published to Golden Repository.`);
      } else {
        const err = await res.json();
        alert(`Commit failed: ${err.error}`);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleAssignDisk = async (clientId: string, diskId: string, deploymentType: string) => {
    try {
      const res = await fetch(`/api/v1/disks/${diskId}/assign`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          client_id: clientId,
          deployment_type: deploymentType,
          mount_point: deploymentType === 'local_vhdx' ? 'C:\\NKB_Disks' : ''
        })
      });
      if (res.ok) {
        setAssignModal(null);
        fetchClients();
        fetchDisks();
        fetchAuditLogs();
        showNotification('Virtual disk successfully assigned to workstation.');
      } else {
        const err = await res.json().catch(() => ({ error: 'Assignment failed' }));
        showNotification(`Assignment Error: ${err.error || 'Failed to assign disk'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while assigning disk.');
    }
  };

  const handleUnassignDisk = async (clientId: string) => {
    if (!confirm('Unassign virtual disk from this workstation?')) return;
    try {
      const res = await fetch(`/api/v1/workstations/${clientId}/unassign-disk`, { method: 'POST' });
      if (res.ok) {
        fetchClients();
        showNotification('Workstation unassigned from disk.');
      } else {
        const err = await res.json().catch(() => ({ error: 'Unassign failed' }));
        showNotification(`Unassign Error: ${err.error || 'Failed to unassign disk'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while unassigning disk.');
    }
  };

  const handleRollbackDisk = async (diskId: string, versionId: string, tag: string) => {
    if (!confirm(`Are you sure you want to rollback to version ${tag}?`)) return;
    try {
      const res = await fetch(`/api/v1/disks/${diskId}/rollback`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version_id: versionId })
      });
      if (res.ok) {
        fetchDisks();
        fetchAuditLogs();
        showNotification(`Rolled back to verified version ${tag}.`);
      } else {
        const err = await res.json();
        alert(`Rollback failed: ${err.error}`);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleValidateDisk = async (diskId: string) => {
    try {
      const res = await fetch(`/api/v1/disks/${diskId}/validate`, { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        alert(`Integrity Check Result:\nValid: ${data.valid ? 'YES (SHA-256 Verified)' : 'CHECKSUM MISMATCH'}\nHash: ${data.checksum}`);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleOpenApplyOS = async (disk: VirtualDisk) => {
    setShowApplyOSModal(disk);
    try {
      const res = await fetch('/api/v1/installers', { credentials: 'include' });
      if (res.ok) {
        const data = await res.json();
        setInstallerList(data);
        if (data.length > 0) {
          setSelectedInstallerPath(data[0].path);
          if (data[0].editions && data[0].editions.length > 0) {
            setAvailableEditions(data[0].editions);
            setWimIndex(data[0].editions[0].index);
          } else {
            setAvailableEditions([{ index: 1, name: 'Default OS Image Index 1', description: '' }]);
            setWimIndex(1);
          }
        } else {
          setAvailableEditions([]);
        }
      }
    } catch (e) {
      console.error('Failed to load installers', e);
    }
  };

  const handleSelectInstaller = (path: string) => {
    setSelectedInstallerPath(path);
    const found = installerList.find(i => i.path === path);
    if (found && found.editions && found.editions.length > 0) {
      setAvailableEditions(found.editions);
      setWimIndex(found.editions[0].index);
    } else {
      setAvailableEditions([{ index: 1, name: 'Default OS Image Index 1', description: '' }]);
      setWimIndex(1);
    }
  };

  const handleExecuteApplyOS = async () => {
    if (!showApplyOSModal || !selectedInstallerPath) return;
    setIsApplyingOS(true);
    showNotification(`Applying OS from ${selectedInstallerPath.split(/[\\/]/).pop()} to ${showApplyOSModal.name}...`);
    try {
      const res = await fetch(`/api/v1/disks/${showApplyOSModal.id}/apply-os`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          installer_path: selectedInstallerPath,
          wim_index: wimIndex
        })
      });
      if (res.ok) {
        const data = await res.json();
        showNotification(data.message || 'OS image deployment started successfully!');
        setShowApplyOSModal(null);
        fetchDisks();
      } else {
        const err = await res.json().catch(() => ({ error: 'Deployment failed' }));
        showNotification(`Deploy Error: ${err.error || 'Failed to start OS deployment'}`);
      }
    } catch (err) {
      showNotification('Network error starting OS image deployment.');
    } finally {
      setIsApplyingOS(false);
    }
  };

  const handleRebootClient = async (clientId: string) => {
    try {
      const res = await fetch(`/api/v1/clients/${clientId}/reboot`, { method: 'POST' });
      if (res.ok) {
        fetchClients();
        showNotification('Triggered remote reboot and volatile write-cache purge.');
      } else {
        const err = await res.json().catch(() => ({ error: 'Operation rejected' }));
        showNotification(`Remote Reboot Blocked: ${err.error || 'DISABLED_BY_SAFETY_POLICY'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error executing reboot command.');
    }
  };

  const handleDeleteClient = async (clientId: string) => {
    if (!confirm('Are you sure you want to remove this client from NKB Diskless management?')) return;
    try {
      const res = await fetch(`/api/v1/clients/${clientId}`, { method: 'DELETE' });
      if (res.ok) {
        fetchClients();
        fetchOverview();
        showNotification('Workstation removed from management.');
      } else {
        const err = await res.json().catch(() => ({ error: 'Delete failed' }));
        showNotification(`Delete Error: ${err.error || 'Failed to remove workstation'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while removing workstation.');
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
        showNotification(`Cloned disk to ${newName}.`);
      } else {
        const err = await res.json().catch(() => ({ error: 'Cloning failed' }));
        showNotification(`Clone Error: ${err.error || 'Failed to duplicate disk'}`);
      }
    } catch (e) {
      console.error(e);
      showNotification('Network error while cloning disk.');
    }
  };

  const handleApplyUpgrade = async () => {
    setUpdateLoading(true);
    setUpdateStatus('Applying update: 1. Taking pre-update DB snapshot... 2. Migrating schema...');
    try {
      const res = await fetch('/api/v1/updates/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version: '1.1.0' })
      });
      if (res.ok) {
        const data = await res.json();
        setUpdateStatus(`Update successfully applied! System running version ${data.version || '1.1.0'}.`);
        fetchVersion();
        fetchAuditLogs();
        showNotification('Platform update successfully verified and applied.');
      } else {
        const err = await res.json().catch(() => ({ error: 'Update failed' }));
        setUpdateStatus(`Update Failed: ${err.error || 'Upgrade failed'}`);
        showNotification(`Update Failed: ${err.error || 'Upgrade failed'}`);
      }
    } catch (e) {
      setUpdateStatus('Update failed: Network connection error.');
      showNotification('Update failed: Network connection error.');
    } finally {
      setUpdateLoading(false);
    }
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  const formatUptime = (seconds?: number) => {
    if (!seconds) return '—';
    const days = Math.floor(seconds / (3600 * 24));
    const hours = Math.floor((seconds % (3600 * 24)) / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${mins}m`;
    return `${mins}m`;
  };

  if (!isAuthenticated) {
    return (
      <div className={`min-h-screen flex items-center justify-center p-4 font-sans ${theme === 'dark' ? 'bg-slate-950 text-slate-100' : 'bg-slate-100 text-slate-900'}`}>
        <div className={`w-full max-w-md p-8 rounded-xl border shadow-2xl ${theme === 'dark' ? 'bg-slate-900 border-slate-800' : 'bg-white border-slate-200'}`}>
          <div className="relative flex flex-col items-center mb-6">
            <button
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
              className="absolute top-0 right-0 text-slate-400 hover:text-white p-1 rounded z-10"
              title="Toggle Theme"
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </button>
            <img src="/nkb-logo.png" alt="NKB Diskless Enterprise Platform" className="w-full max-w-[320px] h-auto object-contain drop-shadow-2xl mb-2" />
          </div>

          <div className="mb-6">
            <h2 className="text-base font-semibold text-slate-800 dark:text-slate-100">Administrator Sign In</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">Authenticate to manage workstations, virtual disks, and network boot infrastructure.</p>
          </div>

          {loginError && (
            <div className="mb-4 p-3 rounded-lg bg-red-50 dark:bg-red-950/50 border border-red-200 dark:border-red-800 text-xs text-red-600 dark:text-red-400 flex items-center gap-2">
              <AlertTriangle className="w-4 h-4 shrink-0" />
              <span>{loginError}</span>
            </div>
          )}

          <form onSubmit={handleLogin} className="space-y-4">
            <div>
              <label className="block text-xs font-medium text-slate-600 dark:text-slate-300 mb-1">Username</label>
              <input
                type="text"
                value={loginUsername}
                onChange={(e) => setLoginUsername(e.target.value)}
                required
                className="w-full px-3 py-2 text-sm rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500 text-slate-900 dark:text-slate-100"
                placeholder="admin"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-slate-600 dark:text-slate-300 mb-1">Password</label>
              <input
                type="password"
                value={loginPassword}
                onChange={(e) => setLoginPassword(e.target.value)}
                required
                className="w-full px-3 py-2 text-sm rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500 text-slate-900 dark:text-slate-100"
                placeholder="••••••••"
              />
            </div>

            <button
              type="submit"
              disabled={isLoggingIn}
              className="w-full py-2.5 px-4 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-semibold rounded-lg shadow transition flex items-center justify-center gap-2"
            >
              {isLoggingIn ? (
                <>
                  <RefreshCw className="w-4 h-4 animate-spin" />
                  <span>Authenticating...</span>
                </>
              ) : (
                <>
                  <Lock className="w-4 h-4" />
                  <span>Sign In to Platform</span>
                </>
              )}
            </button>
          </form>

          <div className="mt-6 pt-4 border-t border-slate-200 dark:border-slate-800 text-center">
            <p className="text-[11px] text-slate-400 font-mono">
              Role-Based Access Control • Session Encrypted (HTTPS/Cookie)
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={`flex h-screen font-sans ${theme === 'dark' ? 'bg-slate-900 text-slate-100' : 'bg-slate-100 text-slate-900'}`}>
      {/* 1. Sidebar - Dark Navy Enterprise Palette */}
      <aside className="w-64 bg-[#0B132B] flex flex-col justify-between shrink-0 select-none border-r border-slate-800">
        <div className="overflow-y-auto">
          {/* Brand Header */}
          <div className="relative p-3 border-b border-slate-800 flex flex-col items-center">
            <button
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
              className="absolute top-2 right-2 text-slate-400 hover:text-white p-1 rounded z-10 transition-colors"
              title="Toggle Theme"
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </button>
            <div className="w-full flex items-center justify-center pt-1 pb-1">
              <img
                src="/nkb-logo.png"
                alt="NKB Diskless Enterprise Platform"
                className="w-full max-w-[220px] h-auto object-contain drop-shadow-xl"
              />
            </div>
          </div>

          {/* Grouped Navigation */}
          <nav className="p-3 space-y-4 text-xs font-medium">
            <div>
              <div className="px-3 mb-1 text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
                Workspace
              </div>
              <div className="space-y-0.5">
                <button
                  onClick={() => setActiveTab('overview')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'overview' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <LayoutDashboard className="w-4 h-4 shrink-0" />
                  <span>Overview</span>
                </button>
                <button
                  onClick={() => setActiveTab('clients')}
                  className={`w-full flex items-center justify-between px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'clients' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <div className="flex items-center gap-2.5">
                    <Monitor className="w-4 h-4 shrink-0" />
                    <span>PC Manager</span>
                  </div>
                  {overview && (
                    <span className="text-[11px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-300 font-mono">
                      {overview.total_clients}
                    </span>
                  )}
                </button>
                <button
                  onClick={() => setActiveTab('remote')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'remote' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Radio className="w-4 h-4 shrink-0" />
                  <span>Remote Control</span>
                </button>
                <button
                  onClick={() => setActiveTab('hardware')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'hardware' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Cpu className="w-4 h-4 shrink-0" />
                  <span>Hardware Monitor</span>
                </button>
              </div>
            </div>

            <div>
              <div className="px-3 mb-1 text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
                Storage & Images
              </div>
              <div className="space-y-0.5">
                <button
                  onClick={() => setActiveTab('disks')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'disks' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <HardDrive className="w-4 h-4 shrink-0" />
                  <span>Virtual Disks</span>
                </button>
                <button
                  onClick={() => setActiveTab('golden')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'golden' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Shield className="w-4 h-4 shrink-0" />
                  <span>Golden Images</span>
                </button>
                <button
                  onClick={() => setActiveTab('assignments')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'assignments' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Layers className="w-4 h-4 shrink-0" />
                  <span>Disk Assignment</span>
                </button>
                <button
                  onClick={() => setActiveTab('super')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'super' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Wrench className="w-4 h-4 shrink-0" />
                  <span>Super-Client</span>
                </button>
                <button
                  onClick={() => setActiveTab('versions')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'versions' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <GitBranch className="w-4 h-4 shrink-0" />
                  <span>Version Control</span>
                </button>
                <button
                  onClick={() => setActiveTab('cache')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'cache' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Database className="w-4 h-4 shrink-0" />
                  <span>Cache Manager</span>
                </button>
              </div>
            </div>

            <div>
              <div className="px-3 mb-1 text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
                Network Boot
              </div>
              <div className="space-y-0.5">
                <button
                  onClick={() => setActiveTab('pxe')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'pxe' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Server className="w-4 h-4 shrink-0" />
                  <span>PXE Manager</span>
                </button>
                <button
                  onClick={() => setActiveTab('iscsi')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'iscsi' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Activity className="w-4 h-4 shrink-0" />
                  <span>iSCSI Manager</span>
                </button>
                <button
                  onClick={() => setActiveTab('network')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'network' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Network className="w-4 h-4 shrink-0" />
                  <span>Hardware & Network</span>
                </button>
              </div>
            </div>

            <div>
              <div className="px-3 mb-1 text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
                Governance
              </div>
              <div className="space-y-0.5">
                <button
                  onClick={() => setActiveTab('groups')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'groups' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Users className="w-4 h-4 shrink-0" />
                  <span>Client Groups</span>
                </button>
                <button
                  onClick={() => setActiveTab('audit')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'audit' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <ShieldAlert className="w-4 h-4 shrink-0" />
                  <span>Audit Logs</span>
                </button>
                <button
                  onClick={() => setActiveTab('updates')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'updates' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <RefreshCw className="w-4 h-4 shrink-0" />
                  <span>Update Manager</span>
                </button>
                <button
                  onClick={() => setActiveTab('settings')}
                  className={`w-full flex items-center gap-2.5 px-3 py-2 rounded text-sm transition-colors ${
                    activeTab === 'settings' ? 'bg-blue-600 text-white font-semibold' : 'text-slate-300 hover:bg-[#1C2541] hover:text-white'
                  }`}
                >
                  <Settings className="w-4 h-4 shrink-0" />
                  <span>Settings</span>
                </button>
              </div>
            </div>
          </nav>
        </div>

        {/* Node Status Footer */}
        <div className="p-4 border-t border-slate-800 text-xs text-slate-400 space-y-2 shrink-0">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
            <span className="text-slate-200 font-medium">Node: Master Primary</span>
          </div>
          <div className="flex justify-between text-slate-400 font-mono text-[11px]">
            <span>DB: SQLite3 POC</span>
            <span>v1.0.0</span>
          </div>
        </div>
      </aside>

      {/* 2. Main Workspace */}
      <main className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {/* Top App Bar */}
        <header className={`h-14 border-b flex items-center justify-between px-6 shrink-0 ${
          theme === 'dark' ? 'bg-slate-900 border-slate-800' : 'bg-white border-slate-200'
        }`}>
          <div className="flex items-center gap-3">
            <h2 className="font-semibold text-base text-slate-800 dark:text-slate-100 capitalize">
              {activeTab.replace('_', ' ')}
            </h2>
            <span className="text-xs px-2 py-0.5 rounded bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300 border border-blue-200 dark:border-blue-800 font-mono">
              NKB-DISKLESS-V1
            </span>
          </div>

          {actionNotice && (
            <div className="text-xs bg-emerald-500 text-white px-3 py-1 rounded shadow flex items-center gap-1.5 animate-fadeIn">
              <Check className="w-3.5 h-3.5" />
              <span>{actionNotice}</span>
            </div>
          )}

          <div className="flex items-center gap-4 text-xs font-mono text-slate-500 dark:text-slate-400">
            <div className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-sans font-medium border transition-colors ${
              wsConnected
                ? 'bg-emerald-50 text-emerald-700 border-emerald-300 dark:bg-emerald-950/60 dark:text-emerald-300 dark:border-emerald-700'
                : 'bg-amber-50 text-amber-700 border-amber-300 dark:bg-amber-950/60 dark:text-amber-300 dark:border-amber-700'
            }`}>
              <span className={`w-2 h-2 rounded-full ${wsConnected ? 'bg-emerald-500 animate-pulse' : 'bg-amber-500'}`}></span>
              <span>{wsConnected ? 'Live WS Telemetry' : 'HTTP Polling (3s)'}</span>
            </div>
            {overview && (
              <>
                <span>CPU: {overview.cpu_usage_pct.toFixed(0)}%</span>
                <span>•</span>
                <span>RAM: {overview.ram_usage_pct.toFixed(0)}%</span>
                <span>•</span>
                <span>SAN: {overview.active_iscsi_sessions} sessions</span>
              </>
            )}
            <div className="flex items-center gap-2 pl-3 border-l border-slate-200 dark:border-slate-800">
              <span className="text-xs font-sans font-medium text-slate-700 dark:text-slate-300">
                {currentUser ? `${currentUser.username} (${currentUser.role})` : 'admin'}
              </span>
              <button
                onClick={handleLogout}
                className="p-1 rounded text-slate-400 hover:text-red-500 hover:bg-red-50 dark:hover:bg-red-950/30 transition-colors"
                title="Sign Out"
              >
                <LogOut className="w-4 h-4" />
              </button>
            </div>
          </div>
        </header>

        {/* Content View Container */}
        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {/* TAB 1: OVERVIEW */}
          {activeTab === 'overview' && overview && (
            <div className="space-y-6">
              {/* Alert Banner if present */}
              {overview.system_alerts && overview.system_alerts.length > 0 && (
                <div className="p-4 bg-amber-50 dark:bg-amber-950/40 border border-amber-200 dark:border-amber-800 rounded-lg flex items-start gap-3">
                  <AlertTriangle className="w-5 h-5 text-amber-600 shrink-0 mt-0.5" />
                  <div className="text-xs space-y-1">
                    <p className="font-semibold text-amber-800 dark:text-amber-300">System Notification</p>
                    {overview.system_alerts.map((a, i) => (
                      <p key={i} className="text-amber-700 dark:text-amber-400 font-mono">{a}</p>
                    ))}
                  </div>
                </div>
              )}

              {/* KPI Cards */}
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <div className="flex items-center justify-between text-slate-500 text-xs font-medium">
                    <span>REGISTERED WORKSTATIONS</span>
                    <Monitor className="w-4 h-4 text-blue-600" />
                  </div>
                  <div className="mt-3 flex items-baseline gap-2">
                    <span className="text-3xl font-bold font-mono">{overview.total_clients}</span>
                    <span className="text-xs text-slate-400">licensed units</span>
                  </div>
                  <div className="mt-3 pt-3 border-t border-slate-100 dark:border-slate-700 flex justify-between text-xs">
                    <span className="text-emerald-600 dark:text-emerald-400 font-medium">{overview.online_clients} Online</span>
                    <span className="text-slate-400">{overview.offline_clients} Offline</span>
                    {overview.super_clients > 0 && (
                      <span className="text-amber-500 font-medium">{overview.super_clients} Super-Client</span>
                    )}
                  </div>
                </div>

                <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <div className="flex items-center justify-between text-slate-500 text-xs font-medium">
                    <span>HOST CPU UTILIZATION</span>
                    <Cpu className="w-4 h-4 text-indigo-600" />
                  </div>
                  <div className="mt-3 flex items-baseline gap-2">
                    <span className="text-3xl font-bold font-mono">{overview.cpu_usage_pct.toFixed(1)}%</span>
                    <span className="text-xs text-slate-400">Core i7-8750H</span>
                  </div>
                  <div className="mt-3 w-full bg-slate-100 dark:bg-slate-700 h-2 rounded-full overflow-hidden">
                    <div className="bg-indigo-600 h-full" style={{ width: `${Math.min(overview.cpu_usage_pct, 100)}%` }}></div>
                  </div>
                </div>

                <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <div className="flex items-center justify-between text-slate-500 text-xs font-medium">
                    <span>RAM ALLOCATION</span>
                    <Activity className="w-4 h-4 text-emerald-600" />
                  </div>
                  <div className="mt-3 flex items-baseline gap-2">
                    <span className="text-3xl font-bold font-mono">{overview.ram_used_gb.toFixed(1)} GB</span>
                    <span className="text-xs text-slate-400">/ {overview.ram_total_gb.toFixed(1)} GB</span>
                  </div>
                  <div className="mt-3 w-full bg-slate-100 dark:bg-slate-700 h-2 rounded-full overflow-hidden">
                    <div className="bg-emerald-600 h-full" style={{ width: `${Math.min(overview.ram_usage_pct, 100)}%` }}></div>
                  </div>
                </div>

                <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <div className="flex items-center justify-between text-slate-500 text-xs font-medium">
                    <span>STORAGE & CACHE</span>
                    <HardDrive className="w-4 h-4 text-sky-600" />
                  </div>
                  <div className="mt-3 flex items-baseline gap-2">
                    <span className="text-3xl font-bold font-mono">{overview.storage_used_gb.toFixed(1)} GB</span>
                    <span className="text-xs text-slate-400">/ {overview.storage_total_gb.toFixed(1)} GB</span>
                  </div>
                  <div className="mt-3 w-full bg-slate-100 dark:bg-slate-700 h-2 rounded-full overflow-hidden">
                    <div className="bg-sky-600 h-full" style={{ width: `${Math.min(overview.storage_usage_pct, 100)}%` }}></div>
                  </div>
                </div>
              </div>

              {/* Workstation Quick Matrix */}
              <div className={`rounded-lg border overflow-hidden ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <div className="p-4 border-b border-slate-200 dark:border-slate-700 flex justify-between items-center">
                  <h3 className="font-semibold text-sm">Workstation Fleet Quick Status</h3>
                  <button onClick={() => setActiveTab('clients')} className="text-xs text-blue-600 hover:underline">
                    View Complete Fleet →
                  </button>
                </div>
                <div className="divide-y divide-slate-100 dark:divide-slate-700">
                  {clients.slice(0, 5).map(c => (
                    <div key={c.id} className="p-3.5 flex items-center justify-between text-xs">
                      <div className="flex items-center gap-3">
                        <span className={`w-2.5 h-2.5 rounded-full ${c.status === 'online' ? 'bg-emerald-500' : 'bg-slate-400'}`}></span>
                        <div>
                          <span className="font-semibold">{c.hostname}</span>
                          <span className="text-slate-400 ml-2 font-mono">{c.ip_address || 'DHCP Pending'}</span>
                        </div>
                      </div>
                      <div className="flex items-center gap-4 text-slate-500">
                        <span>MAC: <span className="font-mono">{c.mac_address}</span></span>
                        <span>Disk: <span className="font-semibold text-slate-700 dark:text-slate-300">{c.assigned_disk_name || 'None'}</span></span>
                        {c.is_super_client && (
                          <span className="px-2 py-0.5 rounded bg-amber-100 dark:bg-amber-900 text-amber-700 dark:text-amber-300 font-bold">
                            SUPER-CLIENT
                          </span>
                        )}
                      </div>
                    </div>
                  ))}
                  {clients.length === 0 && (
                    <div className="p-8 text-center text-slate-400 text-xs">
                      No workstations registered yet. Click PC Manager to register your first client computer.
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* TAB 2: PC MANAGER */}
          {activeTab === 'clients' && (
            <div className="space-y-4">
              {/* Filter & Action Toolbar */}
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2 flex-1 max-w-md">
                  <div className="relative w-full">
                    <Search className="w-4 h-4 absolute left-3 top-2.5 text-slate-400" />
                    <input
                      type="text"
                      placeholder="Search hostname or MAC address..."
                      value={searchQuery}
                      onChange={e => setSearchQuery(e.target.value)}
                      className={`w-full pl-9 pr-4 py-2 text-xs rounded border focus:outline-none focus:ring-1 focus:ring-blue-600 ${
                        theme === 'dark' ? 'bg-slate-800 border-slate-700 text-white' : 'bg-white border-slate-300'
                      }`}
                    />
                  </div>
                  <select
                    value={selectedStatus}
                    onChange={e => setSelectedStatus(e.target.value)}
                    className={`text-xs py-2 px-3 rounded border focus:outline-none ${
                      theme === 'dark' ? 'bg-slate-800 border-slate-700 text-white' : 'bg-white border-slate-300'
                    }`}
                  >
                    <option value="">All Statuses</option>
                    <option value="online">Online</option>
                    <option value="offline">Offline</option>
                  </select>
                </div>
                <button
                  onClick={() => setShowAddClient(true)}
                  className="px-3 py-2 bg-blue-600 text-white text-xs font-medium rounded hover:bg-blue-700 flex items-center gap-1.5 shadow"
                >
                  <Plus className="w-3.5 h-3.5" />
                  <span>Register Workstation</span>
                </button>
              </div>

              {/* Workstation Fleet Table */}
              <div className={`rounded-lg border overflow-hidden ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <table className="w-full text-left text-xs">
                  <thead className={`border-b font-semibold text-slate-500 uppercase tracking-wider ${
                    theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-slate-50 border-slate-200'
                  }`}>
                    <tr>
                      <th className="p-3.5">Status</th>
                      <th className="p-3.5">Hostname</th>
                      <th className="p-3.5">MAC Address</th>
                      <th className="p-3.5">IP Address</th>
                      <th className="p-3.5">Assigned Virtual Disk</th>
                      <th className="p-3.5">Boot Mode</th>
                      <th className="p-3.5">Telemetry</th>
                      <th className="p-3.5 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-700">
                    {clients.map(c => (
                      <tr key={c.id} className="hover:bg-slate-50/50 dark:hover:bg-slate-700/30">
                        <td className="p-3.5">
                          <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full font-medium ${
                            c.status === 'online'
                              ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
                              : 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-400'
                          }`}>
                            <span className={`w-1.5 h-1.5 rounded-full ${c.status === 'online' ? 'bg-emerald-500' : 'bg-slate-400'}`}></span>
                            {c.status.toUpperCase()}
                          </span>
                        </td>
                        <td className="p-3.5 font-bold">
                          {c.hostname}
                          {c.is_super_client && (
                            <span className="ml-2 text-[10px] px-1.5 py-0.5 bg-amber-500 text-white rounded font-mono font-normal">
                              SUPER
                            </span>
                          )}
                        </td>
                        <td className="p-3.5 font-mono text-slate-500">{c.mac_address}</td>
                        <td className="p-3.5 font-mono">{c.ip_address || '—'}</td>
                        <td className="p-3.5">
                          {c.assigned_disk_name ? (
                            <span className="font-medium text-blue-600 dark:text-blue-400">{c.assigned_disk_name}</span>
                          ) : (
                            <span className="text-slate-400 italic">Unassigned</span>
                          )}
                        </td>
                        <td className="p-3.5 font-mono">{c.boot_mode}</td>
                        <td className="p-3.5 font-mono text-[11px] text-slate-500">
                          {c.cpu_usage_pct !== undefined ? `CPU: ${c.cpu_usage_pct.toFixed(0)}% | RAM: ${c.ram_usage_pct?.toFixed(0)}%` : 'No Agent'}
                        </td>
                        <td className="p-3.5 text-right space-x-1.5">
                          <button
                            onClick={() => {
                              setSelectedClientForRemote(c);
                              setActiveTab('remote');
                            }}
                            className="p-1 text-blue-600 hover:bg-blue-50 dark:hover:bg-blue-900/40 rounded"
                            title="Remote Management"
                          >
                            <Radio className="w-4 h-4 inline" />
                          </button>
                          <button
                            onClick={() => handleToggleSuperClient(c.id, c.is_super_client)}
                            className={`p-1 rounded ${c.is_super_client ? 'text-amber-500 bg-amber-50' : 'text-slate-400 hover:text-amber-500'}`}
                            title={c.is_super_client ? 'Exit Super-Client' : 'Enter Super-Client'}
                          >
                            <Wrench className="w-4 h-4 inline" />
                          </button>
                          <button
                            onClick={() => handleDeleteClient(c.id)}
                            className="p-1 text-slate-400 hover:text-red-500 rounded"
                            title="Remove Workstation"
                          >
                            <Trash2 className="w-4 h-4 inline" />
                          </button>
                        </td>
                      </tr>
                    ))}
                    {clients.length === 0 && (
                      <tr>
                        <td colSpan={8} className="p-8 text-center text-slate-400">
                          No workstations found.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 3: REMOTE CONTROL */}
          {activeTab === 'remote' && (
            <div className="space-y-6">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-bold">Workstation Remote Management Cockpit</h3>
                  <p className="text-xs text-slate-500">Authorized administrative commands with audit trail.</p>
                </div>
                <div className="flex items-center gap-2">
                  <select
                    value={selectedClientForRemote ? selectedClientForRemote.id : ''}
                    onChange={e => {
                      const found = clients.find(c => c.id === e.target.value);
                      setSelectedClientForRemote(found || null);
                      if (found) fetchCommandHistory(found.id);
                    }}
                    className={`text-xs py-1.5 px-3 rounded border ${
                      theme === 'dark' ? 'bg-slate-800 border-slate-700 text-white' : 'bg-white border-slate-300'
                    }`}
                  >
                    <option value="">Select Workstation...</option>
                    {clients.map(c => (
                      <option key={c.id} value={c.id}>{c.hostname} ({c.mac_address})</option>
                    ))}
                  </select>
                </div>
              </div>

              {selectedClientForRemote ? (
                <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                  {/* Selected Client Card */}
                  <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                    <div className="flex items-center gap-2.5 mb-4">
                      <Monitor className="w-5 h-5 text-blue-600" />
                      <div>
                        <h4 className="font-bold text-sm">{selectedClientForRemote.hostname}</h4>
                        <p className="text-[11px] font-mono text-slate-400">{selectedClientForRemote.mac_address}</p>
                      </div>
                    </div>
                    <div className="space-y-2 text-xs border-t border-slate-100 dark:border-slate-700 pt-3">
                      <div className="flex justify-between"><span className="text-slate-400">IP Address:</span><span className="font-mono">{selectedClientForRemote.ip_address || '—'}</span></div>
                      <div className="flex justify-between"><span className="text-slate-400">Status:</span><span className="font-bold uppercase text-emerald-600">{selectedClientForRemote.status}</span></div>
                      <div className="flex justify-between"><span className="text-slate-400">Assigned Disk:</span><span className="font-semibold">{selectedClientForRemote.assigned_disk_name || 'None'}</span></div>
                      <div className="flex justify-between"><span className="text-slate-400">Uptime:</span><span>{formatUptime(selectedClientForRemote.uptime_seconds)}</span></div>
                      <div className="flex justify-between"><span className="text-slate-400">Logged User:</span><span>{selectedClientForRemote.logged_in_user || '—'}</span></div>
                    </div>
                  </div>

                  {/* Remote Operations Grid */}
                  <div className={`md:col-span-2 p-5 rounded-lg border space-y-4 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                    <h4 className="font-bold text-xs uppercase tracking-wider text-slate-500">Authorized Actions</h4>
                    <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
                      <button
                        onClick={() => handleSendWOL(selectedClientForRemote.id, selectedClientForRemote.hostname)}
                        className="p-3 border rounded text-xs font-semibold flex flex-col items-center gap-2 hover:bg-blue-50 dark:hover:bg-blue-900/30 border-blue-200 dark:border-blue-800 text-blue-700 dark:text-blue-300"
                      >
                        <Power className="w-5 h-5" />
                        <span>Wake-on-LAN</span>
                      </button>
                      <button
                        onClick={() => handleDownloadRDP(selectedClientForRemote.id)}
                        className="p-3 border rounded text-xs font-semibold flex flex-col items-center gap-2 hover:bg-indigo-50 dark:hover:bg-indigo-900/30 border-indigo-200 dark:border-indigo-800 text-indigo-700 dark:text-indigo-300"
                      >
                        <ExternalLink className="w-5 h-5" />
                        <span>Remote Desktop</span>
                      </button>
                      <button
                        onClick={() => handleSendRemoteCommand(selectedClientForRemote.id, 'lock')}
                        className="p-3 border rounded text-xs font-semibold flex flex-col items-center gap-2 hover:bg-amber-50 dark:hover:bg-amber-900/30 border-amber-200 dark:border-amber-800 text-amber-700 dark:text-amber-300"
                      >
                        <Lock className="w-5 h-5" />
                        <span>Lock Workstation</span>
                      </button>
                      <button
                        onClick={() => handleSendRemoteCommand(selectedClientForRemote.id, 'restart')}
                        className="p-3 border rounded text-xs font-semibold flex flex-col items-center gap-2 hover:bg-orange-50 dark:hover:bg-orange-900/30 border-orange-200 dark:border-orange-800 text-orange-700 dark:text-orange-300"
                      >
                        <RotateCcw className="w-5 h-5" />
                        <span>Graceful Reboot</span>
                      </button>
                      <button
                        onClick={() => handleSendRemoteCommand(selectedClientForRemote.id, 'shutdown')}
                        className="p-3 border rounded text-xs font-semibold flex flex-col items-center gap-2 hover:bg-red-50 dark:hover:bg-red-900/30 border-red-200 dark:border-red-800 text-red-700 dark:text-red-300"
                      >
                        <Power className="w-5 h-5" />
                        <span>Shutdown</span>
                      </button>
                      <button
                        onClick={() => handleSendRemoteCommand(selectedClientForRemote.id, 'query_processes')}
                        className="p-3 border rounded text-xs font-semibold flex flex-col items-center gap-2 hover:bg-slate-100 dark:hover:bg-slate-700 border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300"
                      >
                        <Activity className="w-5 h-5" />
                        <span>Query Processes</span>
                      </button>
                    </div>

                    {/* Command History Table */}
                    <div className="mt-4 pt-4 border-t border-slate-100 dark:border-slate-700">
                      <h5 className="font-bold text-xs mb-2">Command Dispatch History</h5>
                      <div className="max-h-48 overflow-y-auto divide-y divide-slate-100 dark:divide-slate-700 text-xs">
                        {commandHistory.map(cmd => (
                          <div key={cmd.id} className="py-2 flex items-center justify-between">
                            <div className="flex items-center gap-2">
                              <span className="font-mono uppercase font-bold text-blue-600">{cmd.command_type}</span>
                              <span className="text-[11px] text-slate-400">{cmd.created_at}</span>
                            </div>
                            <span className={`px-2 py-0.5 rounded text-[10px] font-mono ${
                              cmd.status === 'completed' ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600'
                            }`}>{cmd.status.toUpperCase()}</span>
                          </div>
                        ))}
                        {commandHistory.length === 0 && (
                          <p className="text-slate-400 text-xs italic">No commands dispatched to this workstation yet.</p>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="p-12 text-center text-slate-400 border rounded-lg border-dashed text-xs">
                  Please select a workstation from the dropdown above to access its remote control cockpit.
                </div>
              )}
            </div>
          )}

          {/* TAB 4: VIRTUAL DISK MANAGER */}
          {activeTab === 'disks' && (
            <div className="space-y-4">
              <div className="flex justify-between items-center">
                <div>
                  <h3 className="text-sm font-bold">Virtual Disk Management</h3>
                  <p className="text-xs text-slate-500">Local VHDX files, sparse capacity allocation, and integrity checks.</p>
                </div>
                <button
                  onClick={() => setShowAddDisk(true)}
                  className="px-3 py-2 bg-blue-600 text-white text-xs font-medium rounded hover:bg-blue-700 flex items-center gap-1.5 shadow"
                >
                  <Plus className="w-3.5 h-3.5" />
                  <span>Create Virtual Disk</span>
                </button>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {disks.map(d => (
                  <div key={d.id} className={`p-5 rounded-lg border flex flex-col justify-between ${
                    theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'
                  }`}>
                    <div>
                      <div className="flex items-center justify-between">
                        <span className="font-bold text-sm text-slate-800 dark:text-slate-100">{d.name}</span>
                        <span className="font-mono text-xs px-2 py-0.5 rounded bg-blue-100 dark:bg-blue-950 text-blue-700 dark:text-blue-300">
                          {d.disk_format}
                        </span>
                      </div>
                      <p className="text-xs text-slate-500 mt-1">{d.description || 'Virtual disk image'}</p>

                      <div className="mt-4 space-y-1.5 text-xs text-slate-500 border-t border-slate-100 dark:border-slate-700 pt-3">
                        <div className="flex justify-between"><span>Target OS:</span><span className="font-semibold text-slate-700 dark:text-slate-300">{d.os_type}</span></div>
                        <div className="flex justify-between"><span>Capacity:</span><span className="font-mono">{formatBytes(d.size_bytes)}</span></div>
                        <div className="flex justify-between"><span>Path:</span><span className="font-mono text-[11px] truncate max-w-[160px]">{d.file_path}</span></div>
                        {d.checksum_sha256 && (
                          <div className="flex justify-between"><span>SHA-256:</span><span className="font-mono text-[10px]">{d.checksum_sha256.substring(0, 10)}...</span></div>
                        )}
                      </div>
                    </div>

                    <div className="mt-5 pt-3 border-t border-slate-100 dark:border-slate-700 flex justify-between items-center gap-2">
                      <button
                        onClick={() => handleOpenApplyOS(d)}
                        className="px-2.5 py-1 text-xs bg-emerald-600 text-white rounded hover:bg-emerald-700 flex items-center gap-1 font-semibold shadow-sm"
                        title="Install Windows OS Image directly to this VHD from ISO/WIM"
                      >
                        <DownloadCloud className="w-3.5 h-3.5" />
                        <span>Build / Flash OS</span>
                      </button>
                      <div className="flex items-center gap-3">
                        <button
                          onClick={() => handleValidateDisk(d.id)}
                          className="text-xs text-blue-600 hover:underline flex items-center gap-1"
                        >
                          <Shield className="w-3.5 h-3.5" />
                          <span>Verify</span>
                        </button>
                        <button
                          onClick={() => handleDuplicateDisk(d.id, d.name)}
                          className="text-xs text-slate-600 dark:text-slate-400 hover:text-blue-600 flex items-center gap-1"
                        >
                          <Copy className="w-3.5 h-3.5" />
                          <span>Clone</span>
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
                {disks.length === 0 && (
                  <div className="col-span-full p-8 text-center text-slate-400 border rounded-lg border-dashed text-xs">
                    No virtual disks created yet. Click "Create Virtual Disk" to initialize a golden VHDX image.
                  </div>
                )}
              </div>
            </div>
          )}

          {/* TAB 5: GOLDEN IMAGE MANAGER */}
          {activeTab === 'golden' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">Golden Image Repository</h3>
                <p className="text-xs text-slate-500">Immutable, read-only baseline master images for production diskless deployment.</p>
              </div>

              <div className={`rounded-lg border overflow-hidden ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <table className="w-full text-left text-xs">
                  <thead className={`border-b font-semibold text-slate-500 uppercase tracking-wider ${
                    theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-slate-50 border-slate-200'
                  }`}>
                    <tr>
                      <th className="p-3.5">Status</th>
                      <th className="p-3.5">Golden Image Name</th>
                      <th className="p-3.5">OS Baseline</th>
                      <th className="p-3.5">Capacity</th>
                      <th className="p-3.5">SHA-256 Checksum</th>
                      <th className="p-3.5">Sealed Versions</th>
                      <th className="p-3.5 text-right">Protection</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-700">
                    {disks.filter(d => d.is_golden).map(d => (
                      <tr key={d.id}>
                        <td className="p-3.5">
                          <span className="px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300 font-bold">
                            SEALED RO
                          </span>
                        </td>
                        <td className="p-3.5 font-bold">{d.name}</td>
                        <td className="p-3.5">{d.os_type}</td>
                        <td className="p-3.5 font-mono">{formatBytes(d.size_bytes)}</td>
                        <td className="p-3.5 font-mono text-[11px]">{d.checksum_sha256 ? `${d.checksum_sha256.substring(0, 16)}...` : 'Pending'}</td>
                        <td className="p-3.5 font-semibold text-blue-600">
                          {(diskVersions[d.id] || []).length} snapshot(s)
                        </td>
                        <td className="p-3.5 text-right">
                          <button
                            onClick={() => handleValidateDisk(d.id)}
                            className="px-2.5 py-1 text-xs border border-blue-600 text-blue-600 rounded hover:bg-blue-50 dark:hover:bg-blue-900/30"
                          >
                            Validate Integrity
                          </button>
                        </td>
                      </tr>
                    ))}
                    {disks.filter(d => d.is_golden).length === 0 && (
                      <tr>
                        <td colSpan={7} className="p-8 text-center text-slate-400">
                          No Golden Images flagged yet. Commit a Super-Client session to seal an official Golden Image.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 6: DISK ASSIGNMENT */}
          {activeTab === 'assignments' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">Workstation-to-Disk Assignment Matrix</h3>
                <p className="text-xs text-slate-500">Configure virtual disk binding and toggle between Local VHDX and Centralized Network iSCSI Boot.</p>
              </div>

              <div className={`rounded-lg border overflow-hidden ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <table className="w-full text-left text-xs">
                  <thead className={`border-b font-semibold text-slate-500 uppercase tracking-wider ${
                    theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-slate-50 border-slate-200'
                  }`}>
                    <tr>
                      <th className="p-3.5">Workstation</th>
                      <th className="p-3.5">MAC Address</th>
                      <th className="p-3.5">Assigned Disk</th>
                      <th className="p-3.5">Deployment Mode</th>
                      <th className="p-3.5">Mount Target</th>
                      <th className="p-3.5 text-right">Action</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-700">
                    {clients.map(c => (
                      <tr key={c.id}>
                        <td className="p-3.5 font-bold">{c.hostname}</td>
                        <td className="p-3.5 font-mono text-slate-500">{c.mac_address}</td>
                        <td className="p-3.5 font-medium text-blue-600">{c.assigned_disk_name || 'None'}</td>
                        <td className="p-3.5">
                          <span className="px-2 py-0.5 rounded bg-slate-100 dark:bg-slate-800 font-mono text-[11px]">
                            network_iscsi
                          </span>
                        </td>
                        <td className="p-3.5 font-mono text-[11px] text-slate-500">SAN LUN 0</td>
                        <td className="p-3.5 text-right space-x-2">
                          <button
                            onClick={() => setAssignModal({ client: c, diskId: disks[0]?.id || '', mode: 'network_iscsi' })}
                            className="px-2.5 py-1 text-xs bg-blue-600 text-white rounded hover:bg-blue-700"
                          >
                            Assign Disk
                          </button>
                          {c.assigned_disk_id && (
                            <button
                              onClick={() => handleUnassignDisk(c.id)}
                              className="px-2.5 py-1 text-xs border border-red-500 text-red-500 rounded hover:bg-red-50"
                            >
                              Unassign
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 7: PXE MANAGER */}
          {activeTab === 'pxe' && (
            <div className="space-y-6">
              <div>
                <h3 className="text-sm font-bold">PXE / Network Boot Management</h3>
                <p className="text-xs text-slate-500">TFTP staging, ProxyDHCP qualification, and dynamic iPXE script generator.</p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div className={`p-4 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <span className="text-xs text-slate-500 font-semibold uppercase">UEFI x64 Bootloader</span>
                  <div className="mt-2 flex items-center justify-between">
                    <span className="font-mono text-sm font-bold">ipxe.efi</span>
                    <span className="px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs font-bold">ACTIVE</span>
                  </div>
                  <p className="text-[11px] text-slate-400 mt-2">TFTP Port 69 (Staged in tftpboot/uefi)</p>
                </div>

                <div className={`p-4 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <span className="text-xs text-slate-500 font-semibold uppercase">Legacy BIOS Bootloader</span>
                  <div className="mt-2 flex items-center justify-between">
                    <span className="font-mono text-sm font-bold">undionly.kpxe</span>
                    <span className="px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs font-bold">ACTIVE</span>
                  </div>
                  <p className="text-[11px] text-slate-400 mt-2">TFTP Port 69 (Staged in tftpboot/bios)</p>
                </div>

                <div className={`p-4 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <span className="text-xs text-slate-500 font-semibold uppercase">ProxyDHCP Service</span>
                  <div className="mt-2 flex items-center justify-between">
                    <span className="font-mono text-sm font-bold">UDP 4011</span>
                    <span className="px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs font-bold">RUNNING</span>
                  </div>
                  <p className="text-[11px] text-slate-400 mt-2">Isolated VLAN 99 (Does not touch corporate DHCP)</p>
                </div>
              </div>

              {/* Dynamic iPXE Script Inspector */}
              <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <h4 className="font-bold text-xs uppercase text-slate-500 mb-3">Live Dynamic iPXE Script Preview (/boot/ipxe)</h4>
                <pre className="p-4 rounded bg-slate-950 text-emerald-400 font-mono text-xs overflow-x-auto">
{`#!ipxe
# NKB Diskless System - Dynamic Boot Configuration
# Workstation MAC: B0:0C:D1:E6:53:31
# Target Node: 192.168.99.1:3260

echo Loading NKB Diskless Boot Environment...
set net0/ip 192.168.99.101
set net0/netmask 255.255.255.0
set net0/gateway 192.168.99.1

# Attach SAN Target & Build iBFT Table
sanhook iscsi:192.168.99.1::::iqn.2026-03.com.nkb:win11-golden-station01

# Chainload Windows EFI Boot Manager with iBFT Injection
chain -ar tftp://192.168.99.1/bootmgfw.efi || goto failed

:failed
echo [ERROR] NKB Bootloader failed to mount SAN volume. Retrying in 5 seconds...
sleep 5
reboot`}
                </pre>
              </div>
            </div>
          )}

          {/* TAB 8: iSCSI TARGET MANAGER */}
          {activeTab === 'iscsi' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">iSCSI SAN Target Subsystem</h3>
                <p className="text-xs text-slate-500">Block-level target orchestration, initiator access control lists, and TCP window optimization.</p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className={`p-5 rounded-lg border space-y-3 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <h4 className="font-bold text-xs uppercase text-slate-500">Target Configuration</h4>
                  <div className="space-y-2 text-xs">
                    <div className="flex justify-between"><span className="text-slate-400">Target IQN:</span><span className="font-mono font-bold">iqn.2026-03.com.nkb:target01</span></div>
                    <div className="flex justify-between"><span className="text-slate-400">Portal IP:</span><span className="font-mono">192.168.99.1:3260</span></div>
                    <div className="flex justify-between"><span className="text-slate-400">Storage Backend:</span><span>Microsoft iSCSI Target / Linux LIO</span></div>
                    <div className="flex justify-between"><span className="text-slate-400">NDIS Reset Guard:</span><span className="text-emerald-600 font-bold">ENABLED (Zero-Timeout)</span></div>
                  </div>
                </div>

                <div className={`p-5 rounded-lg border space-y-3 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <h4 className="font-bold text-xs uppercase text-slate-500">Active Initiator ACLs</h4>
                  <div className="space-y-1.5 text-xs">
                    {clients.map(c => (
                      <div key={c.id} className="flex justify-between items-center py-1 border-b border-slate-100 dark:border-slate-700">
                        <span className="font-mono">{c.mac_address}</span>
                        <span className="text-slate-500 font-mono">iqn.1991-05.com.microsoft:{c.hostname.toLowerCase()}</span>
                        <span className="text-emerald-600 font-bold text-[10px]">AUTHORIZED</span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* TAB 9: CACHE MANAGER */}
          {activeTab === 'cache' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">Differential Write-Cache Management</h3>
                <p className="text-xs text-slate-500">Volatile client differential buffers, persistent delta layers, and on-boot cache purges.</p>
              </div>

              <div className={`rounded-lg border overflow-hidden ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <table className="w-full text-left text-xs">
                  <thead className={`border-b font-semibold text-slate-500 uppercase tracking-wider ${
                    theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-slate-50 border-slate-200'
                  }`}>
                    <tr>
                      <th className="p-3.5">Workstation</th>
                      <th className="p-3.5">Cache Policy</th>
                      <th className="p-3.5">Allocated Size</th>
                      <th className="p-3.5">Delta Storage Path</th>
                      <th className="p-3.5 text-right">Cache Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-700">
                    {clients.map(c => (
                      <tr key={c.id}>
                        <td className="p-3.5 font-bold">{c.hostname}</td>
                        <td className="p-3.5">
                          <span className={`px-2 py-0.5 rounded font-mono font-medium ${
                            c.write_cache_mode === 'persistent' ? 'bg-indigo-100 text-indigo-700' : 'bg-sky-100 text-sky-700'
                          }`}>
                            {c.write_cache_mode.toUpperCase()}
                          </span>
                        </td>
                        <td className="p-3.5 font-mono">20.0 GB</td>
                        <td className="p-3.5 font-mono text-[11px] text-slate-500">
                          {`cache/wc_${c.mac_address.replace(/:/g, '')}.delta`}
                        </td>
                        <td className="p-3.5 text-right">
                          <button
                            onClick={() => handleRebootClient(c.id)}
                            className="px-2.5 py-1 text-xs bg-amber-600 text-white rounded hover:bg-amber-700"
                          >
                            Purge Cache & Reboot
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 10: SUPER-CLIENT */}
          {activeTab === 'super' && (
            <div className="space-y-6">
              <div>
                <h3 className="text-sm font-bold">Super-Client Maintenance Cockpit</h3>
                <p className="text-xs text-slate-500">
                  Exclusive database-locked maintenance mode. Only ONE designated workstation can boot the Golden Image in read-write mode to install updates and software.
                </p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <h4 className="font-bold text-xs uppercase text-slate-500 mb-3">Maintenance Status</h4>
                  {clients.some(c => c.is_super_client) ? (
                    <div className="space-y-3">
                      <div className="p-3 bg-amber-50 dark:bg-amber-950 border border-amber-200 rounded text-amber-800 dark:text-amber-300 text-xs">
                        <span className="font-bold">EXCLUSIVE LOCK ACTIVE</span>
                        <p className="mt-1">
                          Workstation <span className="font-mono font-bold">{clients.find(c => c.is_super_client)?.hostname}</span> currently holds exclusive read-write lock.
                        </p>
                      </div>
                      <div className="flex gap-2">
                        <button
                          onClick={() => setCommitModal(clients.find(c => c.is_super_client) || null)}
                          className="flex-1 py-2 bg-emerald-600 text-white rounded font-medium text-xs hover:bg-emerald-700"
                        >
                          Commit & Seal Version
                        </button>
                        <button
                          onClick={() => {
                            const sc = clients.find(c => c.is_super_client);
                            if (sc) handleToggleSuperClient(sc.id, true);
                          }}
                          className="px-3 py-2 border border-red-500 text-red-500 rounded text-xs hover:bg-red-50"
                        >
                          Discard
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="p-4 text-center text-slate-400 text-xs">
                      No client is currently in Super-Client mode. All workstations are operating in standard protected read-only mode.
                    </div>
                  )}
                </div>

                <div className={`md:col-span-2 p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                  <h4 className="font-bold text-xs uppercase text-slate-500 mb-3">Initiate Maintenance Mode</h4>
                  <div className="divide-y divide-slate-100 dark:divide-slate-700">
                    {clients.map(c => (
                      <div key={c.id} className="py-2.5 flex items-center justify-between text-xs">
                        <div>
                          <span className="font-bold">{c.hostname}</span>
                          <span className="text-slate-400 ml-2 font-mono">{c.mac_address}</span>
                        </div>
                        <div>
                          {c.is_super_client ? (
                            <span className="px-2 py-0.5 bg-amber-500 text-white rounded font-bold text-[10px]">
                              ACTIVE SUPER-CLIENT
                            </span>
                          ) : (
                            <button
                              disabled={clients.some(o => o.is_super_client)}
                              onClick={() => handleToggleSuperClient(c.id, false)}
                              className="px-3 py-1 bg-blue-600 text-white rounded text-xs hover:bg-blue-700 disabled:opacity-40"
                            >
                              Acquire Super Lock
                            </button>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* TAB 11: VERSION CONTROL */}
          {activeTab === 'versions' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">Golden Image Version Control Tree</h3>
                <p className="text-xs text-slate-500">Immutable version history, release notes, SHA-256 integrity signatures, and 1-click rollback.</p>
              </div>

              <div className="space-y-6">
                {disks.map(disk => {
                  const vers = diskVersions[disk.id] || [];
                  return (
                    <div key={disk.id} className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                      <div className="flex justify-between items-center mb-4">
                        <div>
                          <h4 className="font-bold text-sm">{disk.name}</h4>
                          <p className="text-xs text-slate-400">{disk.os_type} • {vers.length} version(s) published</p>
                        </div>
                        <span className="px-2 py-0.5 rounded bg-blue-100 dark:bg-blue-950 text-blue-700 dark:text-blue-300 font-mono text-xs">
                          Current: {vers[0]?.version_tag || 'Base v1.0'}
                        </span>
                      </div>

                      <div className="relative pl-6 border-l-2 border-blue-600 space-y-4">
                        {vers.map((v, i) => (
                          <div key={v.id} className="relative">
                            <span className="absolute -left-[31px] top-1 w-3.5 h-3.5 rounded-full bg-blue-600 border-2 border-white dark:border-slate-800"></span>
                            <div className="flex justify-between items-start text-xs">
                              <div>
                                <span className="font-bold text-sm text-blue-600">{v.version_tag}</span>
                                <span className="text-slate-400 ml-2 font-mono text-[11px]">{v.created_at}</span>
                                <p className="text-slate-600 dark:text-slate-300 mt-1">{v.description || 'Routine update release'}</p>
                                <p className="text-slate-400 font-mono text-[10px] mt-0.5">SHA-256: {v.checksum_sha256}</p>
                              </div>
                              {i > 0 && (
                                <button
                                  onClick={() => handleRollbackDisk(disk.id, v.id, v.version_tag)}
                                  className="px-2 py-1 text-xs border border-orange-500 text-orange-600 rounded hover:bg-orange-50 flex items-center gap-1"
                                >
                                  <RotateCcw className="w-3 h-3" />
                                  <span>Rollback</span>
                                </button>
                              )}
                            </div>
                          </div>
                        ))}
                        {vers.length === 0 && (
                          <p className="text-xs text-slate-400 italic">No incremental snapshots committed yet for this disk image.</p>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* TAB 12: HARDWARE & NETWORK MANAGEMENT (PHASE 3E) */}
          {activeTab === 'network' && (
            <NetworkManagement
              theme={theme}
              showNotification={showNotification}
              overview={overview}
            />
          )}

          {/* TAB 13: HARDWARE MONITOR */}
          {activeTab === 'hardware' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">Client Hardware Telemetry</h3>
                <p className="text-xs text-slate-500">Real-time CPU, RAM, Motherboard, NIC, and GPU specs reported by the NKB Workstation Agent.</p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {clients.map(c => (
                  <div key={c.id} className={`p-5 rounded-lg border space-y-3 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                    <div className="flex justify-between items-center">
                      <span className="font-bold text-sm">{c.hostname}</span>
                      <span className={`px-2 py-0.5 rounded text-xs font-mono ${
                        c.status === 'online' ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600'
                      }`}>{c.status.toUpperCase()}</span>
                    </div>
                    <div className="space-y-1.5 text-xs text-slate-500 border-t border-slate-100 dark:border-slate-700 pt-3">
                      <div className="flex justify-between"><span>MAC:</span><span className="font-mono text-slate-700 dark:text-slate-300">{c.mac_address}</span></div>
                      <div className="flex justify-between"><span>IP:</span><span className="font-mono">{c.ip_address || '—'}</span></div>
                      <div className="flex justify-between"><span>OS Version:</span><span>{c.os_version || 'Windows 10/11 x64'}</span></div>
                      <div className="flex justify-between"><span>Logged In User:</span><span className="font-semibold text-slate-700 dark:text-slate-300">{c.logged_in_user || '—'}</span></div>
                      <div className="flex justify-between"><span>Agent Version:</span><span className="font-mono">{c.agent_version || '1.0.0'}</span></div>
                      <div className="flex justify-between"><span>CPU Usage:</span><span className="font-mono">{c.cpu_usage_pct !== undefined ? `${c.cpu_usage_pct.toFixed(0)}%` : '—'}</span></div>
                      <div className="flex justify-between"><span>RAM Usage:</span><span className="font-mono">{c.ram_usage_pct !== undefined ? `${c.ram_usage_pct.toFixed(0)}%` : '—'}</span></div>
                      <div className="flex justify-between"><span>Uptime:</span><span>{formatUptime(c.uptime_seconds)}</span></div>
                    </div>
                  </div>
                ))}
                {clients.length === 0 && (
                  <div className="col-span-full p-8 text-center text-slate-400 border rounded-lg border-dashed text-xs">
                    No hardware inventory reported yet. Workstation telemetry will appear when agents report heartbeats.
                  </div>
                )}
              </div>
            </div>
          )}

          {/* TAB 14: CLIENT GROUPS */}
          {activeTab === 'groups' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">Client Department Groups</h3>
                <p className="text-xs text-slate-500">Organize workstations by department with shared default virtual disks and cache policies.</p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {groups.map(g => (
                  <div key={g.id} className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                    <h4 className="font-bold text-sm">{g.name}</h4>
                    <p className="text-xs text-slate-500 mt-1">{g.description || 'Department group'}</p>
                    <div className="mt-4 pt-3 border-t border-slate-100 dark:border-slate-700 space-y-1.5 text-xs text-slate-500">
                      <div className="flex justify-between"><span>Boot Timeout:</span><span className="font-mono">{g.boot_timeout_seconds}s</span></div>
                      <div className="flex justify-between"><span>Cache Policy:</span><span className="font-mono uppercase">{g.write_cache_policy}</span></div>
                      <div className="flex justify-between"><span>Allow Super-Client:</span><span className="font-bold">{g.allow_super_client ? 'YES' : 'NO'}</span></div>
                    </div>
                  </div>
                ))}
                {groups.length === 0 && (
                  <div className="col-span-full p-8 text-center text-slate-400 border rounded-lg border-dashed text-xs">
                    No department groups defined yet.
                  </div>
                )}
              </div>
            </div>
          )}

          {/* TAB 15: AUDIT LOGS */}
          {activeTab === 'audit' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">Security Audit & Compliance Trail</h3>
                <p className="text-xs text-slate-500">Tamper-evident record of all administrative commands, Super-Client sessions, and disk assignments.</p>
              </div>

              <div className={`rounded-lg border overflow-hidden ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <table className="w-full text-left text-xs">
                  <thead className={`border-b font-semibold text-slate-500 uppercase tracking-wider ${
                    theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-slate-50 border-slate-200'
                  }`}>
                    <tr>
                      <th className="p-3.5">Timestamp</th>
                      <th className="p-3.5">Operator</th>
                      <th className="p-3.5">Action</th>
                      <th className="p-3.5">Target</th>
                      <th className="p-3.5">Details</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-700">
                    {auditLogs.map(l => (
                      <tr key={l.id}>
                        <td className="p-3.5 font-mono text-[11px] text-slate-400">{l.created_at}</td>
                        <td className="p-3.5 font-semibold">{l.username || 'system'}</td>
                        <td className="p-3.5 font-mono text-blue-600 dark:text-blue-400 font-bold">{l.action}</td>
                        <td className="p-3.5 font-mono text-[11px]">{l.target_type}: {l.target_id}</td>
                        <td className="p-3.5">{l.details}</td>
                      </tr>
                    ))}
                    {auditLogs.length === 0 && (
                      <tr>
                        <td colSpan={5} className="p-8 text-center text-slate-400">
                          No audit entries recorded yet.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 16: UPDATE MANAGER */}
          {activeTab === 'updates' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">NKB Platform Update & Migration Engine</h3>
                <p className="text-xs text-slate-500">Atomic software updates, pre-update database snapshots, and rollback protection.</p>
              </div>

              <div className={`p-6 rounded-lg border max-w-xl space-y-4 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <div className="flex justify-between items-center">
                  <span className="text-xs text-slate-500">Installed Version:</span>
                  <span className="font-mono font-bold text-sm">{installedVersion}</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-xs text-slate-500">Target Upgrade:</span>
                  <span className="font-mono font-bold text-sm text-blue-600">v1.1.0-ENTERPRISE</span>
                </div>
                <div className="p-3 bg-blue-50 dark:bg-blue-950/40 border border-blue-200 dark:border-blue-800 rounded text-xs text-blue-800 dark:text-blue-300">
                  All updates automatically perform a full database snapshot before migration. Rollback is supported without data loss.
                </div>
                <button
                  onClick={handleApplyUpgrade}
                  disabled={updateLoading}
                  className="w-full py-2.5 bg-blue-600 disabled:bg-blue-400 text-white rounded text-xs font-semibold hover:bg-blue-700 shadow"
                >
                  {updateLoading ? 'Applying Upgrade & Pre-Update Snapshot...' : 'Verify & Apply Upgrade'}
                </button>
                {updateStatus && (
                  <p className="text-xs font-mono text-emerald-600 mt-2">{updateStatus}</p>
                )}
              </div>
            </div>
          )}

          {/* TAB 17: SETTINGS */}
          {activeTab === 'settings' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-bold">System Configuration & Settings</h3>
                <p className="text-xs text-slate-500">Global server parameters, storage roots, network bindings, and licensing.</p>
              </div>

              <div className={`p-6 rounded-lg border max-w-2xl space-y-4 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
                <div className="grid grid-cols-2 gap-4 text-xs">
                  <div>
                    <label className="font-semibold text-slate-500 block mb-1">Server Hostname</label>
                    <input type="text" readOnly value={overview?.server_hostname || 'NKB-SERVER-01'} className="w-full p-2 border rounded font-mono bg-slate-50 dark:bg-slate-800" />
                  </div>
                  <div>
                    <label className="font-semibold text-slate-500 block mb-1">Platform Architecture</label>
                    <input type="text" readOnly value="Windows 11 Pro Zero-Purchase PoC" className="w-full p-2 border rounded font-mono bg-slate-50 dark:bg-slate-800" />
                  </div>
                  <div>
                    <label className="font-semibold text-slate-500 block mb-1">Database Engine</label>
                    <input type="text" readOnly value="SQLite3 (PostgreSQL-Ready Dual Mode)" className="w-full p-2 border rounded font-mono bg-slate-50 dark:bg-slate-800" />
                  </div>
                  <div>
                    <label className="font-semibold text-slate-500 block mb-1">Dedicated Test Subnet</label>
                    <input type="text" readOnly value="192.168.50.0/24 (Air-Gapped)" className="w-full p-2 border rounded font-mono bg-slate-50 dark:bg-slate-800" />
                  </div>
                  <div>
                    <label className="font-semibold text-slate-500 block mb-1">TFTP Storage Root</label>
                    <input type="text" readOnly value="tftpboot" className="w-full p-2 border rounded font-mono bg-slate-50 dark:bg-slate-800" />
                  </div>
                  <div>
                    <label className="font-semibold text-slate-500 block mb-1">Write-Cache Storage</label>
                    <input type="text" readOnly value="storage/cache" className="w-full p-2 border rounded font-mono bg-slate-50 dark:bg-slate-800" />
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </main>

      {/* MODAL 1: REGISTER CLIENT */}
      {showAddClient && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className={`w-full max-w-md p-6 rounded-lg shadow-xl ${theme === 'dark' ? 'bg-slate-800 text-white' : 'bg-white text-slate-900'}`}>
            <h3 className="font-bold text-sm mb-4">Register New Workstation</h3>
            <form onSubmit={handleCreateClient} className="space-y-4 text-xs">
              <div>
                <label className="block text-slate-500 mb-1">Hostname *</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. NKB-STATION-01"
                  value={newClient.hostname}
                  onChange={e => setNewClient({ ...newClient, hostname: e.target.value })}
                  className="w-full p-2 border rounded font-mono focus:outline-none focus:ring-1 focus:ring-blue-600 dark:bg-slate-700 dark:border-slate-600"
                />
              </div>
              <div>
                <label className="block text-slate-500 mb-1">MAC Address *</label>
                <input
                  type="text"
                  required
                  placeholder="00:11:22:33:44:55"
                  value={newClient.mac}
                  onChange={e => setNewClient({ ...newClient, mac: e.target.value })}
                  className="w-full p-2 border rounded font-mono focus:outline-none focus:ring-1 focus:ring-blue-600 dark:bg-slate-700 dark:border-slate-600"
                />
              </div>
              <div>
                <label className="block text-slate-500 mb-1">Department Group</label>
                <select
                  value={newClient.group_id}
                  onChange={e => setNewClient({ ...newClient, group_id: e.target.value })}
                  className="w-full p-2 border rounded focus:outline-none dark:bg-slate-700 dark:border-slate-600"
                >
                  <option value="">No Group</option>
                  {groups.map(g => (
                    <option key={g.id} value={g.id}>{g.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-slate-500 mb-1">Assign Initial Virtual Disk</label>
                <select
                  value={newClient.disk_id}
                  onChange={e => setNewClient({ ...newClient, disk_id: e.target.value })}
                  className="w-full p-2 border rounded focus:outline-none dark:bg-slate-700 dark:border-slate-600"
                >
                  <option value="">None (Unassigned)</option>
                  {disks.map(d => (
                    <option key={d.id} value={d.id}>{d.name} ({formatBytes(d.size_bytes)})</option>
                  ))}
                </select>
              </div>
              <div className="flex justify-end gap-2 pt-3 border-t dark:border-slate-700">
                <button
                  type="button"
                  onClick={() => setShowAddClient(false)}
                  className="px-4 py-2 border rounded text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-700"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 bg-blue-600 text-white rounded font-medium hover:bg-blue-700"
                >
                  Save Workstation
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* MODAL 2: CREATE VIRTUAL DISK */}
      {showAddDisk && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className={`w-full max-w-md p-6 rounded-lg shadow-xl ${theme === 'dark' ? 'bg-slate-800 text-white' : 'bg-white text-slate-900'}`}>
            <h3 className="font-bold text-sm mb-4">Provision Virtual Disk (VHDX)</h3>
            <form onSubmit={handleCreateDisk} className="space-y-4 text-xs">
              <div>
                <label className="block text-slate-500 mb-1">Disk Image Name *</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. Win11 Pro CNC Operator"
                  value={newDisk.name}
                  onChange={e => setNewDisk({ ...newDisk, name: e.target.value })}
                  className="w-full p-2 border rounded focus:outline-none focus:ring-1 focus:ring-blue-600 dark:bg-slate-700 dark:border-slate-600"
                />
              </div>
              <div>
                <label className="block text-slate-500 mb-1">Capacity (GB)</label>
                <input
                  type="number"
                  min="10"
                  max="500"
                  value={newDisk.size_gb}
                  onChange={e => setNewDisk({ ...newDisk, size_gb: parseInt(e.target.value) || 50 })}
                  className="w-full p-2 border rounded font-mono focus:outline-none dark:bg-slate-700 dark:border-slate-600"
                />
              </div>
              <div>
                <label className="block text-slate-500 mb-1">Format</label>
                <select
                  value={newDisk.format}
                  onChange={e => setNewDisk({ ...newDisk, format: e.target.value })}
                  className="w-full p-2 border rounded focus:outline-none dark:bg-slate-700 dark:border-slate-600"
                >
                  <option value="VHDX">VHDX (Dynamic Expanding)</option>
                  <option value="VHD">VHD (Legacy)</option>
                </select>
              </div>
              <div>
                <label className="block text-slate-500 mb-1">Operating System</label>
                <input
                  type="text"
                  value={newDisk.os_type}
                  onChange={e => setNewDisk({ ...newDisk, os_type: e.target.value })}
                  className="w-full p-2 border rounded focus:outline-none dark:bg-slate-700 dark:border-slate-600"
                />
              </div>
              <div className="flex justify-end gap-2 pt-3 border-t dark:border-slate-700">
                <button
                  type="button"
                  onClick={() => setShowAddDisk(false)}
                  className="px-4 py-2 border rounded text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-700"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 bg-blue-600 text-white rounded font-medium hover:bg-blue-700"
                >
                  Provision Disk
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* MODAL: APPLY OS IMAGE TO VIRTUAL DISK */}
      {showApplyOSModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className={`w-full max-w-lg p-6 rounded-lg shadow-xl ${theme === 'dark' ? 'bg-slate-800 text-white' : 'bg-white text-slate-900'}`}>
            <div className="flex items-center justify-between mb-3 border-b dark:border-slate-700 pb-3">
              <div className="flex items-center gap-2">
                <DownloadCloud className="w-5 h-5 text-emerald-500" />
                <h3 className="font-bold text-sm">Deploy OS Image to Virtual Disk</h3>
              </div>
              <button onClick={() => setShowApplyOSModal(null)} className="text-slate-400 hover:text-white">
                <X className="w-4 h-4" />
              </button>
            </div>

            <div className="space-y-4 text-xs">
              <div className="p-3 rounded bg-blue-50 dark:bg-blue-950/40 border border-blue-200 dark:border-blue-900">
                <div className="flex justify-between items-center text-blue-900 dark:text-blue-300">
                  <span className="font-semibold">Target Virtual Disk:</span>
                  <span className="font-bold font-mono text-sm">{showApplyOSModal.name}</span>
                </div>
                <div className="flex justify-between items-center text-blue-700 dark:text-blue-400 mt-1">
                  <span>Capacity: {formatBytes(showApplyOSModal.size_bytes)}</span>
                  <span className="truncate max-w-[200px] text-[10px] font-mono">{showApplyOSModal.file_path}</span>
                </div>
              </div>

              <div>
                <div className="flex justify-between items-center mb-1">
                  <label className="text-slate-500 font-semibold">Select Source Installer</label>
                  <span className="text-[11px] px-2 py-0.5 rounded bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 font-medium font-mono">
                    {installerList.length} installer{installerList.length !== 1 ? 's' : ''} auto-detected
                  </span>
                </div>
                {installerList.length > 0 ? (
                  <select
                    value={selectedInstallerPath}
                    onChange={e => handleSelectInstaller(e.target.value)}
                    className="w-full p-2.5 border rounded focus:outline-none dark:bg-slate-700 dark:border-slate-600 font-mono text-xs"
                  >
                    {installerList.map((inst, idx) => (
                      <option key={idx} value={inst.path}>
                        {inst.name} ({formatBytes(inst.size_bytes)}) [{inst.type.toUpperCase()}]
                      </option>
                    ))}
                  </select>
                ) : (
                  <div className="p-3 border rounded text-amber-600 bg-amber-50 dark:bg-amber-950/30 text-xs space-y-1">
                    <p className="font-semibold">No installer files (.iso/.wim) found in C:\NKB-PoC\installers.</p>
                    <p className="text-[11px] text-slate-500 leading-normal">
                      Note: If accessing via the Cloud VPS, open the Local Server Dashboard at <span className="font-mono font-bold text-blue-600">http://192.168.254.100:8443</span> (or http://localhost:8443) where the physical installers folder is located.
                    </p>
                  </div>
                )}
              </div>

              <div>
                <div className="flex justify-between items-center mb-1">
                  <label className="text-slate-500 font-semibold">Target WIM Edition</label>
                  <span className="text-[10px] text-emerald-600 dark:text-emerald-400 font-medium">
                    ✓ Auto-Detected ({availableEditions.length} available)
                  </span>
                </div>
                <select
                  value={wimIndex}
                  onChange={e => setWimIndex(parseInt(e.target.value) || 1)}
                  className="w-full p-2.5 border rounded focus:outline-none dark:bg-slate-700 dark:border-slate-600 text-xs font-medium"
                >
                  {availableEditions.map(ed => (
                    <option key={ed.index} value={ed.index}>
                      Index {ed.index} - {ed.name} {ed.description && ed.description !== ed.name ? `(${ed.description})` : ''}
                    </option>
                  ))}
                  {availableEditions.length === 0 && (
                    <option value={1}>Index 1 - Windows 11 Pro / Master Image (Default)</option>
                  )}
                </select>
              </div>

              <div className="p-3 rounded bg-slate-50 dark:bg-slate-900 border text-slate-500 text-[11px] leading-relaxed">
                <span className="font-semibold text-slate-700 dark:text-slate-300">⚡ Automated Fast Deployment:</span>
                <p className="mt-1">
                  The server mounts the ISO, formats the VHD with GPT + UEFI partitions, applies the Windows image directly using DISM, and writes the UEFI bootloader.
                  Workstations will boot directly to the Windows Desktop without requiring manual setup on the client!
                </p>
              </div>

              <div className="flex justify-end gap-2 pt-3 border-t dark:border-slate-700">
                <button
                  type="button"
                  disabled={isApplyingOS}
                  onClick={() => setShowApplyOSModal(null)}
                  className="px-4 py-2 border rounded text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-700"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  disabled={isApplyingOS || installerList.length === 0}
                  onClick={handleExecuteApplyOS}
                  className="px-4 py-2 bg-emerald-600 text-white rounded font-medium hover:bg-emerald-700 flex items-center gap-1.5 shadow"
                >
                  <DownloadCloud className="w-4 h-4" />
                  <span>{isApplyingOS ? 'Deploying...' : 'Build & Flash OS to VHD'}</span>
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* MODAL 3: COMMIT SUPER-CLIENT */}
      {commitModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className={`w-full max-w-md p-6 rounded-lg shadow-xl ${theme === 'dark' ? 'bg-slate-800 text-white' : 'bg-white text-slate-900'}`}>
            <h3 className="font-bold text-sm mb-2">Publish Sealed Golden Version</h3>
            <p className="text-xs text-slate-500 mb-4">
              Commit software updates from workstation <span className="font-bold">{commitModal.hostname}</span> into an immutable Golden Image snapshot.
            </p>
            <div className="space-y-4 text-xs">
              <div>
                <label className="block text-slate-500 mb-1">New Version Tag *</label>
                <input
                  type="text"
                  value={commitVersionTag}
                  onChange={e => setCommitVersionTag(e.target.value)}
                  placeholder="e.g. v1.1"
                  className="w-full p-2 border rounded font-mono focus:outline-none dark:bg-slate-700 dark:border-slate-600"
                />
              </div>
              <div>
                <label className="block text-slate-500 mb-1">Release Notes *</label>
                <textarea
                  rows={3}
                  value={commitReleaseNotes}
                  onChange={e => setCommitReleaseNotes(e.target.value)}
                  placeholder="e.g. Installed Windows security updates and updated Realtek NIC drivers."
                  className="w-full p-2 border rounded focus:outline-none dark:bg-slate-700 dark:border-slate-600"
                />
              </div>
              <div className="flex justify-end gap-2 pt-3 border-t dark:border-slate-700">
                <button
                  type="button"
                  onClick={() => setCommitModal(null)}
                  className="px-4 py-2 border rounded text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-700"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={handleCommitSuperClient}
                  className="px-4 py-2 bg-emerald-600 text-white rounded font-medium hover:bg-emerald-700 shadow"
                >
                  Commit & Seal
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* MODAL 4: DISK ASSIGNMENT */}
      {assignModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className={`w-full max-w-md p-6 rounded-lg shadow-xl ${theme === 'dark' ? 'bg-slate-800 text-white' : 'bg-white text-slate-900'}`}>
            <h3 className="font-bold text-sm mb-4">Assign Virtual Disk to {assignModal.client.hostname}</h3>
            <div className="space-y-4 text-xs">
              <div>
                <label className="block text-slate-500 mb-1">Select Disk Image</label>
                <select
                  value={assignModal.diskId}
                  onChange={e => setAssignModal({ ...assignModal, diskId: e.target.value })}
                  className="w-full p-2 border rounded dark:bg-slate-700 dark:border-slate-600"
                >
                  {disks.map(d => (
                    <option key={d.id} value={d.id}>{d.name} ({formatBytes(d.size_bytes)})</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-slate-500 mb-1">Deployment Mode</label>
                <select
                  value={assignModal.mode}
                  onChange={e => setAssignModal({ ...assignModal, mode: e.target.value })}
                  className="w-full p-2 border rounded dark:bg-slate-700 dark:border-slate-600"
                >
                  <option value="network_iscsi">Network Diskless Boot (iSCSI)</option>
                  <option value="local_vhdx">Local Attached VHDX (Direct Mount)</option>
                </select>
              </div>
              <div className="flex justify-end gap-2 pt-3 border-t dark:border-slate-700">
                <button
                  type="button"
                  onClick={() => setAssignModal(null)}
                  className="px-4 py-2 border rounded text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-700"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={() => handleAssignDisk(assignModal.client.id, assignModal.diskId, assignModal.mode)}
                  className="px-4 py-2 bg-blue-600 text-white rounded font-medium hover:bg-blue-700"
                >
                  Confirm Assignment
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
