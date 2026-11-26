import React, { useState, useEffect } from 'react';
import {
  Network,
  RefreshCw,
  CheckCircle2,
  AlertTriangle,
  RotateCcw,
  Copy,
  Check,
  Shield,
  ShieldAlert,
  Server,
  Activity,
  Terminal,
  HelpCircle,
  ExternalLink,
  Lock,
  ArrowRight,
  Sliders,
  FileCheck
} from 'lucide-react';

export interface DiscoveredNetworkAdapter {
  id: string;
  interface_alias: string;
  interface_description: string;
  interface_index: number;
  interface_guid: string;
  mac_address: string;
  status: string;
  link_speed: string;
  ipv4_addresses: string[];
  ipv6_addresses?: string[];
  default_gateways?: string[];
  dns_servers?: string[];
  dhcp_enabled: boolean;
  is_physical: boolean;
  is_virtual: boolean;
  is_wireless: boolean;
  is_primary_internet: boolean;
  safety_role: 'CANDIDATE' | 'PROTECTED_INTERNET' | 'VIRTUAL_BLOCKED';
  safety_warning?: string;
}

export interface AdapterMatch {
  persistent_key: string;
  alias: string;
  description: string;
  index: number;
  guid: string;
  mac: string;
}

export interface NetworkProfileData {
  version: number;
  profile_id: string;
  profile_name: string;
  network_mode: string;
  selected_adapter: AdapterMatch;
  ipv4_config: {
    server_ip?: string;
    ip_address?: string;
    prefix_length: number;
    subnet_mask?: string;
    default_gateway: string;
    dns_servers: string[];
  };
  dhcp_config: {
    enabled: boolean;
    pool_configured: boolean;
    pool_start: string;
    pool_end: string;
    lease_duration_sec?: number;
    lease_duration_seconds?: number;
    subnet_mask?: string;
  };
  pxe_config: {
    enable_standalone_dhcp: boolean;
    enable_proxy_dhcp: boolean;
    enable_tftp: boolean;
    enable_http_boot: boolean;
    tftp_port: number;
    proxy_dhcp_port: number;
    http_boot_port: number;
    uefi_bootloader: string;
    bios_bootloader: string;
  };
  storage_root: string;
  status: string;
  last_verification: string;
  last_verified_at?: string;
  last_updated_at: string;
  management_route_secured?: boolean;
  alternate_management_nic?: string;
}

export interface StagingPreview {
  adapter_name: string;
  adapter_mac: string;
  adapter_desc: string;
  current_ipv4: string[];
  current_gateway: string[];
  current_dns: string[];
  proposed_ipv4: string;
  proposed_gateway: string;
  proposed_dns: string[];
  proposed_pool: string;
  network_mode: string;
  conflicts: {
    has_conflict: boolean;
    overlapping_subnet: boolean;
    ip_conflict: boolean;
    gateway_conflict: boolean;
    protected_adapter: boolean;
    warnings: string[];
    errors: string[];
  };
}

export interface PreflightCheck {
  name: string;
  passed: boolean;
  blocked: boolean;
  detail: string;
}

export interface PreflightResult {
  eligible: boolean;
  status: string;
  adapter_alias: string;
  adapter_mac: string;
  adapter_guid: string;
  current_role: string;
  has_active_default_route: boolean;
  active_gateway?: string;
  management_route_secured: boolean;
  management_interface_alias?: string;
  subnet_overlap_detected: boolean;
  subnet_overlap_details?: string;
  message: string;
  checks: PreflightCheck[];
}

interface NetworkManagementProps {
  theme: 'dark' | 'light';
  showNotification: (msg: string) => void;
  overview?: {
    network_rx_mbs: number;
    network_tx_mbs: number;
  } | null;
}

export default function NetworkManagement({
  theme,
  showNotification,
  overview
}: NetworkManagementProps) {
  const [adapters, setAdapters] = useState<DiscoveredNetworkAdapter[]>([]);
  const [profile, setProfile] = useState<NetworkProfileData | null>(null);
  const [preview, setPreview] = useState<StagingPreview | null>(null);
  const [preflight, setPreflight] = useState<PreflightResult | null>(null);
  const [stagingScript, setStagingScript] = useState<string>('');
  const [rollbackScript, setRollbackScript] = useState<string>('');
  const [isScanning, setIsScanning] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isPreflighting, setIsPreflighting] = useState(false);
  const [isVerifying, setIsVerifying] = useState(false);
  const [isRollingBackProfile, setIsRollingBackProfile] = useState(false);
  const [copiedStaging, setCopiedStaging] = useState(false);
  const [copiedRollback, setCopiedRollback] = useState(false);
  const [adoptTarget, setAdoptTarget] = useState<DiscoveredNetworkAdapter | null>(null);
  const [showWindowsRollbackModal, setShowWindowsRollbackModal] = useState(false);
  const [verificationResult, setVerificationResult] = useState<{
    status: 'PASS' | 'FAIL' | 'BLOCKED';
    message: string;
    timestamp: string;
  } | null>(null);

  // Form State
  const [serverIP, setServerIP] = useState('192.168.50.1');
  const [prefixLen, setPrefixLen] = useState(24);
  const [poolStart, setPoolStart] = useState('192.168.50.100');
  const [poolEnd, setPoolEnd] = useState('192.168.50.200');
  const [leaseDuration, setLeaseDuration] = useState(86400);
  const [networkMode, setNetworkMode] = useState('isolated_standalone');
  const [managementRouteSecured, setManagementRouteSecured] = useState(false);
  const [uefiBootloader, setUefiBootloader] = useState('ipxe.efi');
  const [biosBootloader, setBiosBootloader] = useState('undionly.kpxe');

  const fetchData = async () => {
    setIsScanning(true);
    try {
      const [resAdapters, resProfile, resStageScript, resRollbackScript] = await Promise.all([
        fetch('/api/v1/network/adapters'),
        fetch('/api/v1/network/profile'),
        fetch('/api/v1/network/scripts/stage'),
        fetch('/api/v1/network/scripts/rollback')
      ]);

      if (resAdapters.ok) {
        const d = await resAdapters.json();
        setAdapters(d.adapters || []);
      }
      if (resProfile.ok) {
        const p: NetworkProfileData = await resProfile.json();
        setProfile(p);
        setServerIP(p.ipv4_config?.server_ip || p.ipv4_config?.ip_address || '192.168.50.1');
        setPrefixLen(p.ipv4_config?.prefix_length || 24);
        setPoolStart(p.dhcp_config?.pool_start || '192.168.50.100');
        setPoolEnd(p.dhcp_config?.pool_end || '192.168.50.200');
        setLeaseDuration(p.dhcp_config?.lease_duration_sec || p.dhcp_config?.lease_duration_seconds || 86400);
        setNetworkMode(p.network_mode || 'isolated_standalone');
        setManagementRouteSecured(p.management_route_secured ?? false);
        setUefiBootloader(p.pxe_config?.uefi_bootloader || 'ipxe.efi');
        setBiosBootloader(p.pxe_config?.bios_bootloader || 'undionly.kpxe');
      }
      if (resStageScript.ok) {
        setStagingScript(await resStageScript.text());
      }
      if (resRollbackScript.ok) {
        setRollbackScript(await resRollbackScript.text());
      } else {
        setRollbackScript('# No rollback script available.');
      }
    } catch (e) {
      console.error('Failed to load network data:', e);
      showNotification('Failed to query network discovery service.');
    } finally {
      setIsScanning(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  const handleRunPreflight = async () => {
    setIsPreflighting(true);
    try {
      const res = await fetch('/api/v1/network/preflight', { method: 'POST' });
      const data: PreflightResult = await res.json();
      setPreflight(data);
      if (data.status === 'READY_TO_STAGE') {
        showNotification('Preflight check PASSED: Adapter is ready for safe staging.');
      } else if (data.status === 'BLOCKED') {
        showNotification(`Preflight BLOCKED: ${data.message}`);
      } else {
        showNotification(`Preflight notice: ${data.status}`);
      }
      await fetchData();
    } catch (e) {
      console.error('Preflight error:', e);
      showNotification('Preflight inspection request failed.');
    } finally {
      setIsPreflighting(false);
    }
  };

  const handleAdopt = async () => {
    if (!adoptTarget) return;
    try {
      const res = await fetch('/api/v1/network/adopt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          persistent_key: adoptTarget.id,
          mac_address: adoptTarget.mac_address,
          interface_alias: adoptTarget.interface_alias,
          interface_description: adoptTarget.interface_description
        })
      });
      const data = await res.json();
      if (!res.ok) {
        showNotification(`Adoption failed: ${data.error || 'Unknown error'}`);
        return;
      }
      showNotification(`Adopted adapter: ${adoptTarget.interface_alias} (${adoptTarget.mac_address})`);
      setAdoptTarget(null);
      await fetchData();
    } catch (e) {
      console.error(e);
      showNotification('Error adopting network adapter.');
    }
  };

  const handleSaveProfile = async () => {
    if (!profile) return;
    setIsSaving(true);
    const updated: NetworkProfileData = {
      ...profile,
      network_mode: networkMode,
      management_route_secured: managementRouteSecured,
      ipv4_config: {
        ...profile.ipv4_config,
        server_ip: serverIP,
        ip_address: serverIP,
        prefix_length: Number(prefixLen),
        default_gateway: networkMode === 'isolated_standalone' ? '' : (profile.ipv4_config?.default_gateway || ''),
        dns_servers: networkMode === 'isolated_standalone' ? [] : (profile.ipv4_config?.dns_servers || [])
      },
      dhcp_config: {
        ...profile.dhcp_config,
        pool_start: poolStart,
        pool_end: poolEnd,
        lease_duration_sec: Number(leaseDuration),
        lease_duration_seconds: Number(leaseDuration)
      },
      pxe_config: {
        ...profile.pxe_config,
        uefi_bootloader: uefiBootloader,
        bios_bootloader: biosBootloader
      }
    };

    try {
      const res = await fetch('/api/v1/network/profile', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updated)
      });
      const data = await res.json();
      if (!res.ok) {
        showNotification(`Validation error: ${data.error || 'Failed to update profile'}`);
        return;
      }
      if (data.preview) setPreview(data.preview);
      showNotification('Profile validated and saved atomically with backup snapshot.');
      await fetchData();
    } catch (e) {
      console.error(e);
      showNotification('Error saving network profile.');
    } finally {
      setIsSaving(false);
    }
  };

  const handleVerify = async () => {
    setIsVerifying(true);
    try {
      const res = await fetch('/api/v1/network/verify', { method: 'POST' });
      const data = await res.json();
      setVerificationResult({
        status: data.status,
        message: data.message,
        timestamp: data.timestamp || new Date().toISOString()
      });
      showNotification(`Live Staging Verification: ${data.status}`);
      await fetchData();
    } catch (e) {
      console.error(e);
      showNotification('Live verification request failed.');
    } finally {
      setIsVerifying(false);
    }
  };

  const handleRollbackProfile = async () => {
    if (!window.confirm('Revert network profile JSON to the previous backup snapshot?')) return;
    setIsRollingBackProfile(true);
    try {
      const res = await fetch('/api/v1/network/rollback/profile', { method: 'POST' });
      const data = await res.json();
      if (!res.ok) {
        showNotification(`Profile rollback failed: ${data.error || 'Error'}`);
        return;
      }
      showNotification('Successfully restored previous network profile snapshot.');
      await fetchData();
    } catch (e) {
      console.error(e);
      showNotification('Profile rollback request failed.');
    } finally {
      setIsRollingBackProfile(false);
    }
  };

  const handleCopyStagingScript = () => {
    const cmd = stagingScript || `powershell -ExecutionPolicy Bypass -File C:\\NKB-PoC\\config\\scripts\\stage_adapter.ps1`;
    navigator.clipboard.writeText(cmd);
    setCopiedStaging(true);
    showNotification('PowerShell staging script copied to clipboard.');
    setTimeout(() => setCopiedStaging(false), 3000);
  };

  const handleCopyRollbackScript = () => {
    const cmd = rollbackScript || `powershell -ExecutionPolicy Bypass -File C:\\NKB-PoC\\config\\scripts\\rollback_adapter.ps1`;
    navigator.clipboard.writeText(cmd);
    setCopiedRollback(true);
    showNotification('PowerShell rollback script copied to clipboard.');
    setTimeout(() => setCopiedRollback(false), 3000);
  };

  const getStatusBadge = (status: string) => {
    switch (status?.toUpperCase()) {
      case 'ACTIVE':
      case 'STAGED':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300">STAGED / ACTIVE</span>;
      case 'READY_TO_STAGE':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-indigo-100 text-indigo-800 dark:bg-indigo-900/40 dark:text-indigo-300">READY TO STAGE</span>;
      case 'VALIDATED':
      case 'SELECTED':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300">SELECTED / VALIDATED</span>;
      case 'CANDIDATE':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-teal-100 text-teal-800 dark:bg-teal-900/40 dark:text-teal-300">CANDIDATE</span>;
      case 'PROTECTED':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">PROTECTED GATEWAY</span>;
      case 'BLOCKED':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300">STAGING BLOCKED</span>;
      case 'ROLLBACK':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-orange-100 text-orange-800 dark:bg-orange-900/40 dark:text-orange-300">ROLLBACK APPLIED</span>;
      case 'ROLLBACK_FAILED':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-red-200 text-red-900 dark:bg-red-950 dark:text-red-200 animate-bounce">ROLLBACK FAILED</span>;
      case 'MISSING':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300 animate-pulse">HARDWARE MISSING</span>;
      case 'ERROR':
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300">CONFIG ERROR</span>;
      default:
        return <span className="px-2 py-0.5 rounded text-xs font-semibold bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300">DISCOVERED</span>;
    }
  };

  const isCurrentAdopted = (a: DiscoveredNetworkAdapter) => {
    if (!profile || !profile.selected_adapter) return false;
    return (
      (profile.selected_adapter.persistent_key && profile.selected_adapter.persistent_key === a.id) ||
      (profile.selected_adapter.mac && profile.selected_adapter.mac.toLowerCase() === a.mac_address.toLowerCase())
    );
  };

  return (
    <div className="space-y-6">
      {/* Header Toolbar */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <div className="flex items-center gap-2">
            <h3 className="text-base font-bold flex items-center gap-2">
              <Network className="w-5 h-5 text-blue-500" />
              Hardware & Network Management
            </h3>
            {profile && getStatusBadge(profile.status)}
          </div>
          <p className="text-xs text-slate-500 mt-1">
            Transactional staging with host route safety, preflight inspection, atomic snapshots, and dual-layer rollback.
          </p>
        </div>

        <div className="flex items-center flex-wrap gap-2">
          <button
            onClick={fetchData}
            disabled={isScanning}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded border border-slate-300 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isScanning ? 'animate-spin' : ''}`} />
            Rescan Adapters
          </button>
          <button
            onClick={handleRunPreflight}
            disabled={isPreflighting}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded border border-indigo-400 bg-indigo-50 text-indigo-700 dark:bg-indigo-950/40 dark:border-indigo-800 dark:text-indigo-300 hover:bg-indigo-100 transition-colors"
          >
            <FileCheck className={`w-3.5 h-3.5 ${isPreflighting ? 'animate-pulse' : ''}`} />
            Run Staging Preflight
          </button>
          <button
            onClick={handleRollbackProfile}
            disabled={isRollingBackProfile}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded border border-amber-300 dark:border-amber-800 text-amber-700 dark:text-amber-400 hover:bg-amber-50 dark:hover:bg-amber-950/30 transition-colors"
            title="Restore previous JSON configuration backup"
          >
            <RotateCcw className="w-3.5 h-3.5" />
            Rollback Profile JSON
          </button>
          <button
            onClick={() => setShowWindowsRollbackModal(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded border border-rose-300 text-rose-700 dark:border-rose-800 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-950/30 transition-colors"
            title="Restore real Windows adapter configuration from snapshot"
          >
            <RotateCcw className="w-3.5 h-3.5 text-rose-600" />
            Windows Adapter Rollback
          </button>
          <button
            onClick={handleVerify}
            disabled={isVerifying}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded bg-blue-600 text-white hover:bg-blue-700 transition-colors shadow-sm"
          >
            <CheckCircle2 className="w-3.5 h-3.5" />
            Verify Staging Gate
          </button>
        </div>
      </div>

      {/* Guided Replacement Alert Banner if Adapter is MISSING */}
      {profile?.status === 'MISSING' && (
        <div className="p-4 rounded-lg border border-amber-300 bg-amber-50 dark:bg-amber-950/40 dark:border-amber-700 flex items-start gap-3">
          <AlertTriangle className="w-5 h-5 text-amber-600 shrink-0 mt-0.5" />
          <div className="space-y-1 text-xs">
            <h4 className="font-bold text-amber-800 dark:text-amber-200">
              Configured Network Adapter Not Found (Hardware Changed or Unplugged)
            </h4>
            <p className="text-amber-700 dark:text-amber-300">
              The previously adopted adapter (<code className="font-mono font-bold">{profile.selected_adapter.alias} - {profile.selected_adapter.mac}</code>) was not detected on this server. Select a candidate adapter from the table below and click <strong>Adopt Adapter</strong>.
            </p>
          </div>
        </div>
      )}

      {/* PREFLIGHT INSPECTION CARD */}
      {preflight && (
        <div className={`p-4 rounded-lg border space-y-3 ${
          preflight.status === 'READY_TO_STAGE'
            ? 'border-emerald-300 bg-emerald-50 dark:bg-emerald-950/30 dark:border-emerald-800'
            : preflight.status === 'BLOCKED'
            ? 'border-rose-300 bg-rose-50 dark:bg-rose-950/30 dark:border-rose-800'
            : 'border-amber-300 bg-amber-50 dark:bg-amber-950/30 dark:border-amber-800'
        }`}>
          <div className="flex justify-between items-start gap-4">
            <div className="flex items-center gap-2">
              <FileCheck className={`w-5 h-5 ${preflight.status === 'READY_TO_STAGE' ? 'text-emerald-600' : 'text-rose-600'}`} />
              <h4 className="font-bold text-sm text-slate-800 dark:text-slate-100">
                Staging Preflight Analysis Gate:
              </h4>
              <span className={`px-2 py-0.5 rounded text-xs font-mono font-bold ${
                preflight.status === 'READY_TO_STAGE'
                  ? 'bg-emerald-200 text-emerald-900 dark:bg-emerald-800 dark:text-emerald-100'
                  : 'bg-rose-200 text-rose-900 dark:bg-rose-800 dark:text-rose-100'
              }`}>
                {preflight.status}
              </span>
            </div>
            <span className="text-[11px] text-slate-500 font-mono">
              Target: {preflight.adapter_alias} ({preflight.adapter_mac})
            </span>
          </div>

          <p className="text-xs text-slate-700 dark:text-slate-300 font-medium">
            {preflight.message}
          </p>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-2 pt-2 border-t border-slate-200 dark:border-slate-700/60 text-xs">
            {preflight.checks.map((chk, i) => (
              <div key={i} className="flex items-start gap-2 p-2 rounded bg-white/70 dark:bg-slate-900/40 border border-slate-200 dark:border-slate-800">
                {chk.passed ? (
                  <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0 mt-0.5" />
                ) : chk.blocked ? (
                  <ShieldAlert className="w-4 h-4 text-rose-600 shrink-0 mt-0.5" />
                ) : (
                  <AlertTriangle className="w-4 h-4 text-amber-600 shrink-0 mt-0.5" />
                )}
                <div className="space-y-0.5">
                  <span className="font-bold text-slate-800 dark:text-slate-200 block text-[11px]">
                    {chk.name}
                  </span>
                  <p className="text-[11px] text-slate-600 dark:text-slate-400">
                    {chk.detail}
                  </p>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Verification Feedback Result Banner */}
      {verificationResult && (
        <div
          className={`p-4 rounded-lg border flex items-start gap-3 ${
            verificationResult.status === 'PASS'
              ? 'border-emerald-300 bg-emerald-50 dark:bg-emerald-950/40 dark:border-emerald-800'
              : verificationResult.status === 'BLOCKED'
              ? 'border-amber-300 bg-amber-50 dark:bg-amber-950/40 dark:border-amber-800'
              : 'border-rose-300 bg-rose-50 dark:bg-rose-950/40 dark:border-rose-800'
          }`}
        >
          {verificationResult.status === 'PASS' ? (
            <CheckCircle2 className="w-5 h-5 text-emerald-600 shrink-0 mt-0.5" />
          ) : (
            <ShieldAlert className="w-5 h-5 text-rose-600 shrink-0 mt-0.5" />
          )}
          <div className="space-y-1 text-xs">
            <div className="flex items-center gap-2">
              <span className="font-bold uppercase tracking-wider text-slate-800 dark:text-slate-100">
                Verification Gate:
              </span>
              <span
                className={`font-mono font-bold px-2 py-0.5 rounded text-xs ${
                  verificationResult.status === 'PASS'
                    ? 'bg-emerald-200 text-emerald-900 dark:bg-emerald-800 dark:text-emerald-100'
                    : 'bg-rose-200 text-rose-900 dark:bg-rose-800 dark:text-rose-100'
                }`}
              >
                {verificationResult.status}
              </span>
              <span className="text-[11px] text-slate-500 font-mono">
                {new Date(verificationResult.timestamp).toLocaleTimeString()}
              </span>
            </div>
            <p className="text-slate-700 dark:text-slate-300 font-mono text-[11px]">
              {verificationResult.message}
            </p>
          </div>
        </div>
      )}

      {/* SECTION 1: DISCOVERED NETWORK ADAPTERS */}
      <div className={`p-5 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
        <div className="flex justify-between items-center mb-3">
          <div>
            <h4 className="text-sm font-bold">Server Network Adapters</h4>
            <p className="text-xs text-slate-500">
              Live hardware discovery with safety classification. Internet & Wi-Fi adapters are automatically protected.
            </p>
          </div>
          <span className="text-xs font-mono text-slate-400">Total: {adapters.length}</span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-xs text-left">
            <thead>
              <tr className="border-b border-slate-200 dark:border-slate-700 text-slate-400 font-semibold uppercase">
                <th className="py-2 px-3">Adapter / Alias</th>
                <th className="py-2 px-3">Hardware Description</th>
                <th className="py-2 px-3">MAC Address</th>
                <th className="py-2 px-3">Current IPv4 & GW</th>
                <th className="py-2 px-3">Status / Speed</th>
                <th className="py-2 px-3">Role & Classification</th>
                <th className="py-2 px-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-700/50">
              {adapters.map((a) => {
                const adopted = isCurrentAdopted(a);
                return (
                  <tr
                    key={a.id}
                    className={`hover:bg-slate-50/50 dark:hover:bg-slate-700/30 transition-colors ${
                      adopted ? 'bg-blue-50/40 dark:bg-blue-950/20' : ''
                    }`}
                  >
                    <td className="py-3 px-3">
                      <div className="font-semibold text-slate-800 dark:text-slate-200 flex items-center gap-1.5">
                        {a.interface_alias}
                        {adopted && (
                          <span className="text-[10px] bg-blue-600 text-white font-bold px-1.5 py-0.2 rounded">
                            CURRENT
                          </span>
                        )}
                      </div>
                      <span className="text-[10px] text-slate-400 font-mono">Index {a.interface_index}</span>
                    </td>
                    <td className="py-3 px-3 text-slate-600 dark:text-slate-300">
                      <div className="truncate max-w-xs">{a.interface_description}</div>
                      <span className="text-[10px] text-slate-400 font-mono truncate block max-w-xs">{a.id}</span>
                    </td>
                    <td className="py-3 px-3 font-mono text-slate-700 dark:text-slate-300">
                      {a.mac_address || '—'}
                    </td>
                    <td className="py-3 px-3 font-mono">
                      <div>{a.ipv4_addresses?.length ? a.ipv4_addresses.join(', ') : 'Unassigned'}</div>
                      {a.default_gateways?.length ? (
                        <div className="text-[10px] text-amber-600 dark:text-amber-400">
                          GW: {a.default_gateways.join(', ')}
                        </div>
                      ) : null}
                    </td>
                    <td className="py-3 px-3">
                      <div className="flex items-center gap-1.5">
                        <span
                          className={`w-2 h-2 rounded-full ${
                            a.status === 'Up' ? 'bg-emerald-500' : 'bg-slate-400'
                          }`}
                        />
                        <span className="font-medium">{a.status}</span>
                      </div>
                      <span className="text-[10px] text-slate-400 font-mono">{a.link_speed || 'Unknown'}</span>
                    </td>
                    <td className="py-3 px-3">
                      {a.safety_role === 'CANDIDATE' ? (
                        <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300">
                          CANDIDATE NIC
                        </span>
                      ) : a.safety_role === 'PROTECTED_INTERNET' ? (
                        <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">
                          PROTECTED GATEWAY
                        </span>
                      ) : (
                        <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-400">
                          VIRTUAL / BLOCKED
                        </span>
                      )}
                      {a.safety_warning && (
                        <p className="text-[10px] text-amber-600 dark:text-amber-400 mt-0.5">
                          {a.safety_warning}
                        </p>
                      )}
                    </td>
                    <td className="py-3 px-3 text-right">
                      {adopted ? (
                        <span className="text-xs font-semibold text-blue-600 dark:text-blue-400">
                          Bound
                        </span>
                      ) : (a.safety_role === 'CANDIDATE' || managementRouteSecured) ? (
                        <button
                          onClick={() => setAdoptTarget(a)}
                          className="px-2.5 py-1 text-xs font-medium rounded bg-blue-600 text-white hover:bg-blue-700 transition-colors shadow-sm"
                        >
                          Adopt Adapter
                        </button>
                      ) : (
                        <span className="text-[11px] text-slate-400 italic">Protected</span>
                      )}
                    </td>
                  </tr>
                );
              })}
              {adapters.length === 0 && (
                <tr>
                  <td colSpan={7} className="py-8 text-center text-slate-400 italic">
                    No network adapters discovered. Click "Rescan Adapters" to query host devices.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* SECTION 2 & 3: CONFIGURATION FORM & TRANSACTIONAL STAGING */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Network Profile Editor */}
        <div className={`p-5 rounded-lg border space-y-4 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
          <div className="flex justify-between items-center">
            <div>
              <h4 className="text-sm font-bold">Network Profile Configuration</h4>
              <p className="text-xs text-slate-500">
                Persistent configuration stored in <code className="font-mono">config/network_profile.json</code>
              </p>
            </div>
            <span className="text-xs font-mono text-slate-400">v{profile?.version || 1}</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
            <div>
              <label className="block text-slate-500 font-medium mb-1">Operating Mode</label>
              <select
                value={networkMode}
                onChange={(e) => setNetworkMode(e.target.value)}
                className={`w-full p-2 rounded border font-sans text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              >
                <option value="isolated_standalone">Isolated Standalone (Zero Uplink)</option>
                <option value="proxydhcp_shared">ProxyDHCP Shared Subnet</option>
                <option value="diagnostic">Diagnostic Boot Only</option>
              </select>
            </div>

            <div>
              <label className="block text-slate-500 font-medium mb-1">Server IPv4 Address</label>
              <input
                type="text"
                value={serverIP}
                onChange={(e) => setServerIP(e.target.value)}
                placeholder="192.168.50.1"
                className={`w-full p-2 rounded border font-mono text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              />
            </div>

            <div>
              <label className="block text-slate-500 font-medium mb-1">Subnet Prefix Length</label>
              <input
                type="number"
                min="8"
                max="30"
                value={prefixLen}
                onChange={(e) => setPrefixLen(Number(e.target.value))}
                className={`w-full p-2 rounded border font-mono text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              />
            </div>

            <div>
              <label className="block text-slate-500 font-medium mb-1">DHCP Pool Start</label>
              <input
                type="text"
                value={poolStart}
                onChange={(e) => setPoolStart(e.target.value)}
                placeholder="192.168.50.100"
                className={`w-full p-2 rounded border font-mono text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              />
            </div>

            <div>
              <label className="block text-slate-500 font-medium mb-1">DHCP Pool End</label>
              <input
                type="text"
                value={poolEnd}
                onChange={(e) => setPoolEnd(e.target.value)}
                placeholder="192.168.50.200"
                className={`w-full p-2 rounded border font-mono text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              />
            </div>

            <div>
              <label className="block text-slate-500 font-medium mb-1">DHCP Lease Duration (sec)</label>
              <input
                type="number"
                value={leaseDuration}
                onChange={(e) => setLeaseDuration(Number(e.target.value))}
                className={`w-full p-2 rounded border font-mono text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              />
            </div>

            <div>
              <label className="block text-slate-500 font-medium mb-1">UEFI Bootloader</label>
              <input
                type="text"
                value={uefiBootloader}
                onChange={(e) => setUefiBootloader(e.target.value)}
                className={`w-full p-2 rounded border font-mono text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              />
            </div>

            <div>
              <label className="block text-slate-500 font-medium mb-1">BIOS Bootloader</label>
              <input
                type="text"
                value={biosBootloader}
                onChange={(e) => setBiosBootloader(e.target.value)}
                className={`w-full p-2 rounded border font-mono text-xs ${
                  theme === 'dark' ? 'bg-slate-900 border-slate-700 text-slate-200' : 'bg-white border-slate-300 text-slate-800'
                }`}
              />
            </div>

            <div className="flex items-center gap-2 pt-2 md:col-span-2">
              <input
                type="checkbox"
                id="mgmt-override"
                checked={managementRouteSecured}
                onChange={(e) => setManagementRouteSecured(e.target.checked)}
                className="rounded border-slate-300 text-blue-600 focus:ring-blue-500 h-4 w-4 cursor-pointer"
              />
              <label htmlFor="mgmt-override" className="text-xs text-slate-500 font-medium cursor-pointer select-none">
                Override Internet Safety Guard (Single-NIC / Lab Testing Mode)
              </label>
            </div>
          </div>

          <div className="pt-2 flex justify-between items-center">
            <span className="text-[11px] text-slate-400">
              Last saved: {profile?.last_updated_at ? new Date(profile.last_updated_at).toLocaleString() : 'Never'}
            </span>
            <button
              onClick={handleSaveProfile}
              disabled={isSaving}
              className="px-4 py-2 text-xs font-semibold rounded bg-blue-600 text-white hover:bg-blue-700 transition-colors shadow-sm"
            >
              {isSaving ? 'Validating...' : 'Save & Validate Profile'}
            </button>
          </div>
        </div>

        {/* Transactional Staging Script & Execution */}
        <div className={`p-5 rounded-lg border space-y-4 ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
          <div className="flex justify-between items-center">
            <div>
              <h4 className="text-sm font-bold">Transactional Staging & Auto-Rollback</h4>
              <p className="text-xs text-slate-500">
                Preflight verified. Captures snapshot and restores automatically on error.
              </p>
            </div>
            <Lock className="w-4 h-4 text-slate-400" />
          </div>

          {/* Staging Diff Preview */}
          <div className="p-3 rounded border border-slate-200 dark:border-slate-700 bg-slate-50/50 dark:bg-slate-900/40 text-xs space-y-2">
            <div className="font-semibold text-slate-700 dark:text-slate-300 flex items-center justify-between">
              <span>Selected Target:</span>
              <span className="font-mono text-blue-600 dark:text-blue-400">
                {profile?.selected_adapter.alias || 'None Selected'}
              </span>
            </div>

            <div className="grid grid-cols-2 gap-2 text-[11px] font-mono border-t border-slate-200 dark:border-slate-700 pt-2">
              <div>
                <span className="text-slate-400 block font-sans">CURRENT STATE:</span>
                <div>IP: {profile?.selected_adapter ? 'Dynamic / Unverified' : '—'}</div>
                <div>Gateway: Inherited or None</div>
              </div>
              <div>
                <span className="text-slate-400 block font-sans">PROPOSED STATE:</span>
                <div className="text-emerald-600 dark:text-emerald-400 font-semibold">
                  IP: {serverIP}/{prefixLen}
                </div>
                <div>Gateway: None (Isolated)</div>
                <div>Pool: {poolStart} - {poolEnd}</div>
              </div>
            </div>
          </div>

          {/* Elevated Command Box */}
          <div className="space-y-1.5 text-xs">
            <label className="block text-slate-500 font-medium">
              Administrator PowerShell Transactional Staging Command:
            </label>
            <div className="p-2.5 rounded bg-slate-900 text-slate-200 font-mono text-[11px] flex items-center justify-between gap-2 overflow-x-auto border border-slate-800">
              <span className="truncate">
                powershell -ExecutionPolicy Bypass -File C:\NKB-PoC\config\scripts\stage_adapter.ps1
              </span>
              <button
                onClick={handleCopyStagingScript}
                className="shrink-0 p-1.5 rounded hover:bg-slate-800 text-slate-300 hover:text-white transition-colors"
                title="Copy Staging Command"
              >
                {copiedStaging ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
              </button>
            </div>
            <p className="text-[11px] text-slate-400">
              Executes Preflight verification, saves Windows snapshot, applies static IP, and rolls back automatically if any step fails.
            </p>
          </div>

          <div className="pt-2 flex justify-end gap-2">
            <button
              onClick={handleCopyStagingScript}
              className="px-3 py-1.5 text-xs font-medium rounded border border-slate-300 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors flex items-center gap-1.5"
            >
              <Copy className="w-3.5 h-3.5" />
              {copiedStaging ? 'Copied' : 'Copy Staging Script'}
            </button>
            <button
              onClick={handleVerify}
              disabled={isVerifying}
              className="px-3 py-1.5 text-xs font-semibold rounded bg-emerald-600 text-white hover:bg-emerald-700 transition-colors shadow-sm flex items-center gap-1.5"
            >
              <CheckCircle2 className="w-3.5 h-3.5" />
              Verify Applied State
            </button>
          </div>
        </div>
      </div>

      {/* Guided Replacement Confirmation Modal */}
      {adoptTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
          <div className={`w-full max-w-md p-6 rounded-xl border shadow-2xl space-y-4 ${theme === 'dark' ? 'bg-slate-900 border-slate-700' : 'bg-white border-slate-200'}`}>
            <div className="flex items-start gap-3">
              <Shield className="w-6 h-6 text-blue-600 shrink-0 mt-0.5" />
              <div>
                <h4 className="text-base font-bold text-slate-900 dark:text-slate-100">
                  Confirm Adapter Adoption
                </h4>
                <p className="text-xs text-slate-500 mt-0.5">
                  Authorize hardware binding for the NKB Diskless System.
                </p>
              </div>
            </div>

            <div className="p-3 rounded-lg bg-slate-50 dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700 text-xs space-y-2">
              <div className="flex justify-between">
                <span className="text-slate-500">Interface Alias:</span>
                <span className="font-semibold text-slate-800 dark:text-slate-200">{adoptTarget.interface_alias}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Hardware:</span>
                <span className="text-slate-700 dark:text-slate-300 truncate max-w-[220px]">{adoptTarget.interface_description}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">MAC Address:</span>
                <span className="font-mono text-slate-800 dark:text-slate-200">{adoptTarget.mac_address}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Persistent Key:</span>
                <span className="font-mono text-blue-600 dark:text-blue-400">{adoptTarget.id}</span>
              </div>
            </div>

            <p className="text-xs text-slate-600 dark:text-slate-400">
              Adopting this adapter will bind it to the network profile and generate staging scripts for <code className="font-mono font-bold">{serverIP}/{prefixLen}</code>. Wi-Fi and primary internet adapters will remain unaffected.
            </p>

            <div className="flex justify-end gap-2 pt-2">
              <button
                onClick={() => setAdoptTarget(null)}
                className="px-4 py-2 text-xs font-medium rounded border border-slate-300 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleAdopt}
                className="px-4 py-2 text-xs font-semibold rounded bg-blue-600 text-white hover:bg-blue-700 transition-colors shadow-sm flex items-center gap-1.5"
              >
                <Check className="w-4 h-4" />
                Confirm & Adopt
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Windows Network Rollback Modal */}
      {showWindowsRollbackModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
          <div className={`w-full max-w-lg p-6 rounded-xl border shadow-2xl space-y-4 ${theme === 'dark' ? 'bg-slate-900 border-slate-700' : 'bg-white border-slate-200'}`}>
            <div className="flex items-start gap-3">
              <RotateCcw className="w-6 h-6 text-rose-600 shrink-0 mt-0.5" />
              <div>
                <h4 className="text-base font-bold text-slate-900 dark:text-slate-100">
                  Windows Network Adapter Restoration (Rollback)
                </h4>
                <p className="text-xs text-slate-500 mt-0.5">
                  Restore physical adapter IPs, routes, DNS, and DHCP state to its pre-staging Windows snapshot.
                </p>
              </div>
            </div>

            <div className="p-3 rounded bg-slate-50 dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700 text-xs space-y-2">
              <span className="font-semibold block text-slate-700 dark:text-slate-300">
                Administrator Rollback Command:
              </span>
              <div className="p-2 rounded bg-slate-900 text-slate-200 font-mono text-[11px] flex items-center justify-between gap-2 overflow-x-auto">
                <span className="truncate">
                  powershell -ExecutionPolicy Bypass -File C:\NKB-PoC\config\scripts\rollback_adapter.ps1
                </span>
                <button
                  onClick={handleCopyRollbackScript}
                  className="p-1 rounded hover:bg-slate-800 text-slate-300 hover:text-white"
                  title="Copy Command"
                >
                  {copiedRollback ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
              <p className="text-[11px] text-slate-400">
                This script reads the saved snapshot, removes any staged static IP, restores original DHCP or static IPs, re-applies default routes, and verifies restoration.
              </p>
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <button
                onClick={() => setShowWindowsRollbackModal(false)}
                className="px-4 py-2 text-xs font-medium rounded border border-slate-300 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
              >
                Close
              </button>
              <button
                onClick={handleCopyRollbackScript}
                className="px-4 py-2 text-xs font-semibold rounded bg-rose-600 text-white hover:bg-rose-700 transition-colors shadow-sm flex items-center gap-1.5"
              >
                <Copy className="w-4 h-4" />
                {copiedRollback ? 'Command Copied' : 'Copy Rollback Command'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* SAN & Boot Throughput Telemetry */}
      <div className="pt-4 border-t border-slate-200 dark:border-slate-800">
        <h4 className="text-xs font-bold uppercase tracking-wider text-slate-400 mb-3">
          Network Boot & SAN Throughput Monitor
        </h4>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className={`p-4 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
            <span className="text-xs text-slate-400 font-medium">INTERFACE TX (OUT)</span>
            <div className="mt-1 text-2xl font-bold font-mono text-blue-600">
              {overview ? overview.network_tx_mbs.toFixed(1) : 0} MB/s
            </div>
            <p className="text-[11px] text-slate-400 mt-0.5">1 Gbps Dedicated Link Speed</p>
          </div>

          <div className={`p-4 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
            <span className="text-xs text-slate-400 font-medium">INTERFACE RX (IN)</span>
            <div className="mt-1 text-2xl font-bold font-mono text-emerald-600">
              {overview ? overview.network_rx_mbs.toFixed(1) : 0} MB/s
            </div>
            <p className="text-[11px] text-slate-400 mt-0.5">Client differential write traffic</p>
          </div>

          <div className={`p-4 rounded-lg border ${theme === 'dark' ? 'bg-slate-800/60 border-slate-700' : 'bg-white border-slate-200'}`}>
            <span className="text-xs text-slate-400 font-medium">ISOLATION ENFORCEMENT</span>
            <div className="mt-1 text-base font-bold font-mono text-slate-700 dark:text-slate-300">
              Air-Gap Isolated Switch Port
            </div>
            <p className="text-[11px] text-slate-400 mt-0.5">Zero default gateway configured</p>
          </div>
        </div>
      </div>
    </div>
  );
}
