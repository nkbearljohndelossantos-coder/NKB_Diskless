# Audit PXE listeners
$ports = @(67, 68, 69, 4011, 8080, 8000, 8443)
$udp = Get-NetUDPEndpoint -ErrorAction SilentlyContinue | Where-Object { $ports -contains $_.LocalPort }
$tcp = Get-NetTCPConnection -ErrorAction SilentlyContinue | Where-Object { $ports -contains $_.LocalPort }

Write-Host "--- ACTIVE UDP LISTENERS ---"
if ($udp) { $udp | Select-Object LocalAddress, LocalPort | Format-Table -AutoSize } else { Write-Host "None (ALL PXE UDP LISTENERS STOPPED)" }

Write-Host "--- ACTIVE TCP LISTENERS ---"
if ($tcp) { $tcp | Select-Object LocalAddress, LocalPort, State | Format-Table -AutoSize } else { Write-Host "None (ALL PXE TCP LISTENERS STOPPED)" }
