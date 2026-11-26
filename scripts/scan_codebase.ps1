Get-ChildItem -Path . -Recurse -File -Include *.go,*.ts,*.tsx | 
    Where-Object { 
        $_.FullName -notmatch '\\node_modules\\' -and 
        $_.FullName -notmatch '\\\.git\\' -and 
        $_.FullName -notmatch '\\dist\\' -and
        $_.FullName -notmatch '\\tftpboot\\'
    } | 
    Select-String -Pattern "TODO|FIXME|placeholder|MOCK" | 
    ForEach-Object {
        [PSCustomObject]@{
            File = $_.Path.Replace($PWD.Path + "\", "")
            LineNumber = $_.LineNumber
            Line = $_.Line.Trim()
        }
    } | Format-Table -Wrap -AutoSize
