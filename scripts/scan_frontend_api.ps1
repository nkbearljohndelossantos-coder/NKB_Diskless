$lines = Get-Content "frontend\src\App.tsx"
$apiCalls = @()
for ($i = 0; $i -lt $lines.Count; $i++) {
    if ($lines[$i] -match "fetch\(") {
        $apiCalls += [PSCustomObject]@{
            LineNumber = $i + 1
            Line = $lines[$i].Trim()
        }
    }
}
Write-Host "Total fetch calls: $($apiCalls.Count)"
$apiCalls | Format-Table -Wrap -AutoSize
