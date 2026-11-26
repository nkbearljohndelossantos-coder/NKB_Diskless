Write-Host "=== NETWORK ADAPTERS ==="
Get-NetAdapter | Select-Object Name, InterfaceIndex, Status, LinkSpeed, MacAddress | Format-Table -AutoSize

Write-Host "=== IP CONFIGURATION ==="
Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -notlike "*Loopback*" } | Select-Object InterfaceAlias, IPAddress, PrefixLength | Format-Table -AutoSize

Write-Host "=== DEFAULT GATEWAYS ==="
Get-NetRoute -DestinationPrefix "0.0.0.0/0" | Select-Object InterfaceAlias, NextHop, RouteMetric | Format-Table -AutoSize

Write-Host "=== IP FORWARDING CHECK ==="
Get-NetIPInterface | Where-Object { $_.AddressFamily -eq "IPv4" } | Select-Object InterfaceAlias, Forwarding, Dhcp | Format-Table -AutoSize

Write-Host "=== TEST NIC (192.168.50.1) PRESENT CHECK ==="
$testIP = Get-NetIPAddress -IPAddress "192.168.50.1" -ErrorAction SilentlyContinue
if ($testIP) {
    Write-Host "TEST NIC PRESENT: YES on $($testIP.InterfaceAlias)"
} else {
    Write-Host "TEST NIC PRESENT: NO - 192.168.50.1 is NOT configured on any interface!"
}
