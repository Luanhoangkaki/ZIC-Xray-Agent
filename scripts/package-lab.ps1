param(
    [string]$BundleID = '20261031',
    [string]$XrayArchive = '.tools/xray-linux/Xray-linux-64.zip'
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
if ($BundleID -notmatch '^[a-zA-Z0-9.-]+$') { throw 'Invalid bundle ID' }
$bundleName = "zic-xray-agent-lab-debian12-amd64-$BundleID"
$archive = Join-Path $projectRoot "dist/$bundleName.tar.gz"
if (Test-Path -LiteralPath $archive) { throw 'Bundle already exists; use a new BundleID to preserve evidence' }
$utf8 = [Text.UTF8Encoding]::new($false)
function Write-LF([string]$Path, [string]$Value) {
    [IO.File]::WriteAllText($Path, $Value.Replace("`r`n", "`n"), $utf8)
}

# Check every build artifact and both ELF machine types, without claiming to
# execute Linux binaries on a Windows host.
$rows = @()
$manifest = Get-Content -LiteralPath dist/SHA256SUMS
foreach ($line in $manifest) {
    if ($line -notmatch '^([a-f0-9]{64})  ((?:linux-|lab-tests/linux-)(?:amd64|arm64)/[a-z0-9.-]+)$') { throw 'Invalid artifact manifest' }
    $expected, $relative = $Matches[1], $Matches[2]
    $path = Join-Path $projectRoot "dist/$relative"
    $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($hash -ne $expected) { throw "Checksum mismatch: $relative" }
    $stream = [IO.File]::OpenRead($path)
    try { $header = New-Object byte[] 20; if ($stream.Read($header,0,20) -ne 20) { throw 'Short ELF' } } finally { $stream.Dispose() }
    $machine = [int]$header[18] + 256 * [int]$header[19]
    $wantMachine = 62
    if ($relative.Contains('linux-arm64/')) { $wantMachine = 183 }
    if ($header[0] -ne 127 -or $header[1] -ne 69 -or $header[2] -ne 76 -or $header[3] -ne 70 -or $header[4] -ne 2 -or $header[5] -ne 1 -or $machine -ne $wantMachine) { throw "Wrong ELF target: $relative" }
    $rows += [ordered]@{file=$relative; sha256=$hash; elf_machine=$machine; bytes=(Get-Item -LiteralPath $path).Length}
}
$tests = @($rows | Where-Object { $_.file.StartsWith('lab-tests/') })
if ($rows.Count -ne 28 -or $tests.Count -ne 22) { throw 'Expected 6 tools and 22 test binaries' }
foreach ($arch in @('amd64','arm64')) {
    if (@($tests | Where-Object { $_.file.StartsWith("lab-tests/linux-$arch/") }).Count -ne 11) { throw "Expected 11 suites for $arch" }
}
$audit = [ordered]@{compiler=(& go version); linux_execution='NOT RUN on Windows build host'; test_binary_count=22; artifact_count=28; artifacts=$rows}
Write-LF (Join-Path $projectRoot 'dist/verification/artifact-audit.json') ($audit | ConvertTo-Json -Depth 6)

# Pin and verify the independently downloaded OFFICIAL core archive. Confirm
# extraction bytes against the archive, not a previously extracted directory.
$release = Get-Content -Raw dist/verification/xray-release.json | ConvertFrom-Json
$asset = @($release.assets | Where-Object { $_.name -eq 'Xray-linux-64.zip' })
$pinned = '8a5f3e6a1057e4bd0f8a7c57c9d91204b4be0b74222a6e210d070603101163c2'
if ($release.tag_name -ne 'v26.10.29' -or $asset.Count -ne 1 -or $asset[0].digest -ne "sha256:$pinned" -or $asset[0].browser_download_url -ne 'https://github.com/XTLS/Xray-core/releases/download/v26.10.29/Xray-linux-64.zip') { throw 'Official core metadata does not match pinned release' }
if ((Get-FileHash -LiteralPath $XrayArchive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $pinned) { throw 'Official core archive checksum mismatch' }
$stageParent = Join-Path $projectRoot ('.cache/packaging/' + [guid]::NewGuid().ToString('N'))
$stage = Join-Path $stageParent $bundleName
foreach ($dir in @('bin','tests','units','evidence','licenses')) { New-Item -ItemType Directory -Force (Join-Path $stage $dir) | Out-Null }
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip = [IO.Compression.ZipFile]::OpenRead((Resolve-Path -LiteralPath $XrayArchive))
try {
    foreach ($name in @('xray','LICENSE')) {
        $entry = $zip.GetEntry($name)
        if ($null -eq $entry) { throw "Missing core archive entry: $name" }
        $target = Join-Path $stage "bin/$name"
        if ($name -eq 'LICENSE') { $target = Join-Path $stage 'licenses/Xray-core-LICENSE' }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $target)
    }
} finally { $zip.Dispose() }
foreach ($component in @('zic-xray-agent','zic-bench','zic-lab')) { Copy-Item -LiteralPath "dist/linux-amd64/$component" -Destination (Join-Path $stage "bin/$component") }
foreach ($test in Get-ChildItem -LiteralPath dist/lab-tests/linux-amd64 -Filter '*.test' -File) { Copy-Item -LiteralPath $test.FullName -Destination (Join-Path $stage "tests/$($test.Name)") }
foreach ($unit in Get-ChildItem -LiteralPath packaging/lab -Filter '*.service' -File) { Write-LF (Join-Path $stage "units/$($unit.Name)") ([IO.File]::ReadAllText($unit.FullName)) }
Write-LF (Join-Path $stage 'lab.sh') ([IO.File]::ReadAllText((Join-Path $projectRoot 'packaging/lab/lab.sh')))
Write-LF (Join-Path $stage 'start.sh') ([IO.File]::ReadAllText((Join-Path $projectRoot 'packaging/lab/start.sh')))
Write-LF (Join-Path $stage 'README.md') ([IO.File]::ReadAllText((Join-Path $projectRoot 'docs/debian12-lab.md')))
foreach ($file in @('artifact-audit.json','xray-release.json','windows-tests.log','vet.log','bash-syntax.log','panel-reference-audit.json')) {
    $source = Join-Path $projectRoot "dist/verification/$file"
    if (!(Test-Path -LiteralPath $source)) { throw "Missing verification evidence: $file" }
    Copy-Item -LiteralPath $source -Destination (Join-Path $stage "evidence/$file")
}
$modules = @(& go list -deps -f '{{with .Module}}{{.Path}}|{{.Dir}}{{end}}' ./...)
if ($LASTEXITCODE -ne 0) { throw 'Cannot list dependency licenses' }
foreach ($module in ($modules | Sort-Object -Unique)) {
    $parts = $module.Split('|')
    if ($parts.Length -ne 2 -or !$parts[1]) { continue }
    $license = Join-Path $parts[1] 'LICENSE'
    if (Test-Path -LiteralPath $license) { Copy-Item -LiteralPath $license -Destination (Join-Path $stage ('licenses/' + $parts[0].Replace('/','_') + '-LICENSE')) }
}
$metadata = [ordered]@{bundle=$bundleName; synthetic=$true; production_ready=$false; sending_enabled=$false; agent_mode='observe'; core_version='v26.10.29'; core_archive_sha256=$pinned; compiler=(& go version); linux_vps_measurements='NOT RUN'; tests_in_bundle=11; test_binaries_audited_across_architectures=22}
Write-LF (Join-Path $stage 'BUILD.json') ($metadata | ConvertTo-Json)
$checksums = @()
foreach ($file in Get-ChildItem -LiteralPath $stage -Recurse -File | Sort-Object FullName) {
    $relative = $file.FullName.Substring($stage.Length+1).Replace('\','/')
    $checksums += (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $relative
}
Write-LF (Join-Path $stage 'SHA256SUMS') (($checksums -join "`n") + "`n")
& tar -czf $archive -C $stageParent $bundleName
if ($LASTEXITCODE -ne 0) { throw 'Cannot create archive' }
$archiveHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
Write-LF "$archive.sha256" "$archiveHash  $bundleName.tar.gz`n"
Write-Output "$archiveHash  $archive"
