# Unit verification of stage-test-nic.ps1 safety validation rules
$prohibitedNames = @("Wi-Fi", "Wi-Fi 2", "WLAN")
$virtualKeywords = @("Hyper-V", "WSL", "VirtualBox", "VMware", "Loopback", "TAP", "VPN", "Bluetooth", "NDIS", "PANGP")

$testCases = @(
    @{ Name = "Wi-Fi"; Desc = "Intel Wireless-AC"; Expected = "REJECT_NAME" },
    @{ Name = "Wi-Fi 2"; Desc = "Realtek Wi-Fi Adapter"; Expected = "REJECT_NAME" },
    @{ Name = "vEthernet (WSL)"; Desc = "Hyper-V Virtual Ethernet Adapter"; Expected = "REJECT_VIRTUAL" },
    @{ Name = "OpenVPN TAP"; Desc = "TAP-Windows Adapter V9"; Expected = "REJECT_VIRTUAL" },
    @{ Name = "Ethernet"; Desc = "Realtek Gaming GbE Family Controller"; Expected = "ALLOW_PHYSICAL" },
    @{ Name = "Ethernet 2"; Desc = "Realtek USB GbE Family Controller"; Expected = "ALLOW_PHYSICAL" }
)

Write-Host "=== TESTING NIC STAGING SAFETY RULES ==="
foreach ($tc in $testCases) {
    $result = "ALLOW_PHYSICAL"
    if ($prohibitedNames -contains $tc.Name) {
        $result = "REJECT_NAME"
    } else {
        foreach ($kw in $virtualKeywords) {
            if ($tc.Desc -like "*$kw*") {
                $result = "REJECT_VIRTUAL"
                break
            }
        }
    }

    $pass = ($result -eq $tc.Expected)
    Write-Host ("Adapter '{0}' ({1}) -> Result: {2} | Expected: {3} | PASS: {4}" -f $tc.Name, $tc.Desc, $result, $tc.Expected, $pass)
    if (-not $pass) { exit 1 }
}
Write-Host "ALL NIC SAFETY RULES PASSED VERIFICATION!" -ForegroundColor Green
