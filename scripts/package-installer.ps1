param([string]$Release = '20261031-r1')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
if ($Release -notmatch '^[a-zA-Z0-9.-]+$') { throw 'Invalid release ID' }
$utf8 = [Text.UTF8Encoding]::new($false)
function Write-LF([string]$Path, [string]$Value) { [IO.File]::WriteAllText($Path, $Value.Replace("`r`n", "`n"), $utf8) }
$releaseMetadata = Get-Content -Raw dist/verification/xray-release.json | ConvertFrom-Json
if ($releaseMetadata.tag_name -ne 'v26.10.29') { throw 'Wrong core release metadata' }
$modules = @(& go list -deps -f '{{with .Module}}{{.Path}}|{{.Dir}}{{end}}' ./...)
if ($LASTEXITCODE -ne 0) { throw 'Cannot list dependency licenses' }
Add-Type -AssemblyName System.IO.Compression.FileSystem
foreach ($arch in @('amd64','arm64')) {
    $name = "zic-xray-agent-installer-debian12-$arch-$Release"
    $archive = Join-Path $projectRoot "dist/$name.tar.gz"
    if (Test-Path -LiteralPath $archive) { throw 'Release already exists; preserve evidence and use a new ID' }
    $parent = Join-Path $projectRoot ('.cache/installer-packaging/' + [guid]::NewGuid().ToString('N'))
    $stage = Join-Path $parent $name
    foreach ($dir in @("bin/linux-$arch","validation/linux-$arch",'packaging','tests','source/packaging','source/scripts','evidence','licenses')) { New-Item -ItemType Directory -Force (Join-Path $stage $dir) | Out-Null }
    $agentRelative = "linux-$arch/zic-xray-agent"
    $manifest = Get-Content dist/SHA256SUMS
    foreach ($file in @($agentRelative) + @(Get-ChildItem -LiteralPath "dist/lab-tests/linux-$arch" -Filter '*.test' -File | ForEach-Object { "lab-tests/linux-$arch/$($_.Name)" })) {
        $hash = (Get-FileHash -LiteralPath "dist/$file").Hash.ToLowerInvariant()
        if (@($manifest | Where-Object {$_ -eq "$hash  $file"}).Count -ne 1) { throw "Artifact mismatch: $file" }
    }
    $tests = @(Get-ChildItem -LiteralPath "dist/lab-tests/linux-$arch" -Filter '*.test' -File)
    if ($tests.Count -ne 12) { throw 'Expected twelve functional suites per architecture' }
    Copy-Item -LiteralPath "dist/$agentRelative" -Destination (Join-Path $stage "bin/linux-$arch/zic-xray-agent")
    foreach ($test in $tests) { Copy-Item -LiteralPath $test.FullName -Destination (Join-Path $stage "tests/$($test.Name)") }
    foreach ($file in @('install.sh','packaging/zic-xray-agent.service','packaging/rollback-agent-install.sh')) {
        $raw = [IO.File]::ReadAllText((Join-Path $projectRoot $file))
        Write-LF (Join-Path $stage "source/$file") $raw
        $output = $file
        if ($file -eq 'packaging/rollback-agent-install.sh') { $output = 'rollback-install.sh' }
        Write-LF (Join-Path $stage $output) $raw
    }
    Write-LF (Join-Path $stage 'source/scripts/test-installer.sh') ([IO.File]::ReadAllText((Join-Path $projectRoot 'scripts/test-installer.sh')))
    Write-LF (Join-Path $stage 'tests/run.sh') ([IO.File]::ReadAllText((Join-Path $projectRoot 'packaging/test-installer-bundle.sh')))
    Write-LF (Join-Path $stage 'README.md') ([IO.File]::ReadAllText((Join-Path $projectRoot 'docs/interactive-installer.md')))
    $zipName = 'Xray-linux-64.zip'; $zipDir = '.tools/xray-linux'
    if ($arch -eq 'arm64') { $zipName = 'Xray-linux-arm64-v8a.zip'; $zipDir = '.tools/xray-linux-arm64' }
    $asset = @($releaseMetadata.assets | Where-Object {$_.name -eq $zipName})
    $zipPath = Join-Path $projectRoot "$zipDir/$zipName"
    if ($asset.Count -ne 1 -or $asset[0].digest -ne ('sha256:' + (Get-FileHash -LiteralPath $zipPath).Hash.ToLowerInvariant()) -or $asset[0].browser_download_url -ne "https://github.com/XTLS/Xray-core/releases/download/v26.10.29/$zipName") { throw 'Official core archive verification failed' }
    $zip = [IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
        [IO.Compression.ZipFileExtensions]::ExtractToFile($zip.GetEntry('xray'), (Join-Path $stage "validation/linux-$arch/xray"))
        [IO.Compression.ZipFileExtensions]::ExtractToFile($zip.GetEntry('LICENSE'), (Join-Path $stage 'licenses/Xray-core-LICENSE'))
    } finally { $zip.Dispose() }
    Write-LF (Join-Path $stage 'licenses/Xray-core-SOURCE.txt') "Official unmodified Xray-core v26.10.29. Source: https://github.com/XTLS/Xray-core/tree/v26.10.29`nBinary archive: $($asset[0].browser_download_url)`n"
    foreach ($module in ($modules | Sort-Object -Unique)) {
        $parts = $module.Split('|'); if ($parts.Length -ne 2 -or !$parts[1]) { continue }
        $license = Join-Path $parts[1] 'LICENSE'
        if (Test-Path -LiteralPath $license) { Copy-Item -LiteralPath $license -Destination (Join-Path $stage ('licenses/' + $parts[0].Replace('/','_') + '-LICENSE')) }
    }
    foreach ($evidence in @('interactive-go-tests.log','interactive-shell-tests.log','interactive-vet.log','interactive-audit.json')) {
        $path = Join-Path $projectRoot "dist/verification/$evidence"
        if (!(Test-Path -LiteralPath $path)) { throw "Missing evidence: $evidence" }
        Write-LF (Join-Path $stage "evidence/$evidence") ([IO.File]::ReadAllText($path))
    }
    $metadata = [ordered]@{release=$name; target="linux/$arch"; compiler=(& go version); agent_mode='observe'; sending_enabled=$false; production_ready=$false; installed_protocols=@('vless'); unsupported_adapters=@('shadowsocks','trojan','hysteria1','hysteria2'); core_use='offline run -test only'; core_version='v26.10.29'; core_sha256=(Get-FileHash (Join-Path $stage "validation/linux-$arch/xray")).Hash.ToLowerInvariant(); test_binaries=12; debian_installer_execution='NOT RUN'; vps_measurements='NOT RUN'}
    Write-LF (Join-Path $stage 'BUILD.json') ($metadata | ConvertTo-Json)
    $checksums = @()
    foreach ($file in Get-ChildItem -LiteralPath $stage -Recurse -File | Sort-Object FullName) {
        $relative = $file.FullName.Substring($stage.Length+1).Replace('\','/')
        $checksums += (Get-FileHash -LiteralPath $file.FullName).Hash.ToLowerInvariant() + '  ' + $relative
    }
    Write-LF (Join-Path $stage 'SHA256SUMS') (($checksums -join "`n") + "`n")
    & 'C:/Program Files/Git/bin/bash.exe' scripts/archive-installer.sh $parent.Replace('\','/') $name $arch $archive.Replace('\','/')
    if ($LASTEXITCODE -ne 0) { throw 'Cannot create installer archive' }
    $hash = (Get-FileHash -LiteralPath $archive).Hash.ToLowerInvariant()
    Write-LF "$archive.sha256" "$hash  $name.tar.gz`n"
    Write-Output "$hash  $archive"
}
