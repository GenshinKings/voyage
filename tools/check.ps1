# check.ps1 —— 一条命令跑完全部验收（本地等价于 CI）
#
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -File tools\check.ps1
# 退出码 0 = 全过；非 0 = 有一步没过（哪一步会打印出来）

$ErrorActionPreference = 'Continue'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
$script:failed = @()

function Step($name, $block) {
    Write-Host ("== " + $name)
    & $block
    if ($LASTEXITCODE -ne 0) {
        $script:failed += $name
        Write-Host ("   x " + $name + " 失败")
    } else {
        Write-Host ("   ok " + $name)
    }
}

Step 'build' { go build ./... }
Step 'vet' { go vet ./... }

Step 'gofmt' {
    $bad = gofmt -l ./cmd ./internal ./fsrs
    if ($bad) {
        Write-Host ("   需要 gofmt: " + ($bad -join ', '))
        $global:LASTEXITCODE = 1
    } else {
        $global:LASTEXITCODE = 0
    }
}

Step 'test' { go test ./... }

Step '版本与变更日志' {
    $ok = $true
    if (-not (Test-Path (Join-Path $root 'VERSION'))) { Write-Host '   缺 VERSION'; $ok = $false }
    if (-not (Test-Path (Join-Path $root 'CHANGELOG.md'))) { Write-Host '   缺 CHANGELOG.md'; $ok = $false }
    if ($ok) { $global:LASTEXITCODE = 0 } else { $global:LASTEXITCODE = 1 }
}

Step '命名（outpost2 只许出现在数据目录名与 docs/）' {
    $files = Get-ChildItem -Recurse -Include *.go, *.md, *.cmd -File |
        Where-Object {
            $_.FullName -notmatch '\\outpost2-data\\' -and
            $_.FullName -notmatch '\\docs\\' -and
            $_.FullName -notmatch '\\.git\\'
        }
    $hits = @()
    foreach ($f in $files) {
        $n = 0
        foreach ($ln in [System.IO.File]::ReadAllLines($f.FullName, [System.Text.Encoding]::UTF8)) {
            $n++
            if ($ln -cmatch 'outpost2' -and $ln -cnotmatch 'outpost2-data') {
                $hits += ($f.FullName + ':' + $n + '  ' + $ln.Trim())
            }
        }
    }
    if ($hits.Count -gt 0) {
        $hits | Select-Object -First 20 | ForEach-Object { Write-Host ('   ' + $_) }
        $global:LASTEXITCODE = 1
    } else {
        $global:LASTEXITCODE = 0
    }
}

Pop-Location

Write-Host ''
if ($script:failed.Count -gt 0) {
    Write-Host ('没过：' + ($script:failed -join '、'))
    exit 1
}
$v = Get-Content (Join-Path $root 'VERSION') -ErrorAction SilentlyContinue
Write-Host ('全过。版本 ' + $v)
exit 0
