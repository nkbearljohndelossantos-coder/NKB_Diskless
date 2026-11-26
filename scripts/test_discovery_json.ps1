$adapters = Get-NetAdapter -ErrorAction SilentlyContinue
$results = @()
foreach ($a in $adapters) {
    $idx = $a.InterfaceIndex
    $ipConf = Get-NetIPConfiguration -InterfaceIndex $idx -ErrorAction SilentlyContinue
    $v4 = Get-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue
    
    $v4Addrs = @()
    if ($ipConf -and $ipConf.IPv4Address) {
        $v4Addrs = @($ipConf.IPv4Address | ForEach-Object { "$($_.IPAddress)/$($_.PrefixLength)" })
    }
    
    $v6Addrs = @()
    if ($ipConf -and $ipConf.IPv6Address) {
        $v6Addrs = @($ipConf.IPv6Address.IPAddress)
    }

    $gws = @()
    if ($ipConf -and $ipConf.IPv4DefaultGateway) {
        $gws = @($ipConf.IPv4DefaultGateway.NextHop)
    }

    $dns = @()
    if ($ipConf -and $ipConf.DNSServer) {
        $dns = @($ipConf.DNSServer.ServerAddresses)
    }

    $dhcp = $false
    if ($v4 -and $v4.Dhcp -eq 'Enabled') {
        $dhcp = $true
    }

    $results += [PSCustomObject]@{
        InterfaceAlias = $a.Name
        InterfaceDescription = $a.InterfaceDescription
        InterfaceIndex = $idx
        InterfaceGuid = $a.InterfaceGuid
        MacAddress = $a.MacAddress
        Status = $a.Status
        LinkSpeed = $a.LinkSpeed
        IPv4Addresses = $v4Addrs
        IPv6Addresses = $v6Addrs
        DefaultGateways = $gws
        DNSServers = $dns
        DhcpEnabled = $dhcp
    }
}
$results | ConvertTo-Json -Depth 3 -Compress
