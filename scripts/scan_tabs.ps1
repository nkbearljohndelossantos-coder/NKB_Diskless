$content = Get-Content "frontend\src\App.tsx"
for ($i = 0; $i -lt $content.Count; $i++) {
    if ($content[$i] -match "activeTab === '") {
        Write-Host ("{0}: {1}" -f ($i + 1), $content[$i].Trim())
    }
}
