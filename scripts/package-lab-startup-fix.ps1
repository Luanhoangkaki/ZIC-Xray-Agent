param([string]$Release = '20261031-r1')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
if ($Release -notmatch '^[a-zA-Z0-9.-]+$') { throw 'Invalid release' }
$name = "zic-xray-lab-startup-fix-$Release"
$archive = Join-Path $projectRoot "dist/$name.tar.gz"
if (Test-Path -LiteralPath $archive) { throw 'Archive already exists; use a new release ID' }
$utf8 = [Text.UTF8Encoding]::new($false)
function Write-LF([string]$Path, [string]$Value) {
    [IO.File]::WriteAllText($Path, $Value.Replace("`r`n", "`n"), $utf8)
}
$parent = Join-Path $projectRoot ('.cache/startup-fix-packaging/' + [guid]::NewGuid().ToString('N'))
$stage = Join-Path $parent $name
foreach ($directory in @('units','source/packaging/lab','source/scripts','evidence')) { New-Item -ItemType Directory -Force (Join-Path $stage $directory) | Out-Null }
foreach ($file in @('lab.sh','start.sh','update-startup.sh')) {
    $raw = [IO.File]::ReadAllText((Join-Path $projectRoot "packaging/lab/$file"))
    Write-LF (Join-Path $stage $file) $raw
    Write-LF (Join-Path $stage "source/packaging/lab/$file") $raw
}
foreach ($role in @('panel','core','agent')) {
    $raw = [IO.File]::ReadAllText((Join-Path $projectRoot "packaging/lab/zic-lab-$role.service"))
    # Agent unit is included ONLY as a rootless regression fixture, not an
    # installation target. No binary or credentials are in this hotfix.
    Write-LF (Join-Path $stage "source/packaging/lab/zic-lab-$role.service") $raw
    if ($role -ne 'agent') { Write-LF (Join-Path $stage "units/zic-lab-$role.service") $raw }
}
Write-LF (Join-Path $stage 'source/scripts/test-lab-startup.sh') ([IO.File]::ReadAllText((Join-Path $projectRoot 'scripts/test-lab-startup.sh')))
Write-LF (Join-Path $stage 'README.md') ([IO.File]::ReadAllText((Join-Path $projectRoot 'docs/lab-startup-fix.md')))
foreach ($file in @('lab-startup-regressions.log','lab-startup-invariants.json','lab-startup-source.patch')) {
    $inputFile = Join-Path $projectRoot "dist/verification/$file"
    if (!(Test-Path -LiteralPath $inputFile)) { throw "Missing evidence: $file" }
    $output = "evidence/$file"
    if ($file.EndsWith('.patch')) { $output = 'CHANGES.patch' }
    Write-LF (Join-Path $stage $output) ([IO.File]::ReadAllText($inputFile))
}
$checksums = @()
foreach ($file in Get-ChildItem -LiteralPath $stage -Recurse -File | Sort-Object FullName) {
    $relative = $file.FullName.Substring($stage.Length+1).Replace('\','/')
    $checksums += (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $relative
}
Write-LF (Join-Path $stage 'SHA256SUMS') (($checksums -join "`n") + "`n")
& tar -czf $archive -C $parent $name
if ($LASTEXITCODE -ne 0) { throw 'Cannot package startup fix' }
$hash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
Write-LF "$archive.sha256" "$hash  $name.tar.gz`n"
Write-Output "$hash  $archive"
