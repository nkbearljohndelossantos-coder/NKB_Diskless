$content = Get-Content "frontend\src\App.tsx"

function Search-Pattern($pattern) {
    Write-Host "=== Matches for: $pattern ===" -ForegroundColor Cyan
    for ($i = 0; $i -lt $content.Count; $i++) {
        if ($content[$i] -match $pattern) {
            Write-Host ("{0}: {1}" -f ($i + 1), $content[$i].Trim())
        }
    }
}

Search-Pattern "rdp"
Search-Pattern "updates"
Search-Pattern "handleDeleteDisk"
Search-Pattern "handleUpdateDisk"
Search-Pattern "CreateGroup"
