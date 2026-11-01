param([string]$Candidate = 'audit2')
$ErrorActionPreference = 'Stop'
if ($Candidate -notmatch '^[a-z0-9-]{1,24}$') {throw 'Invalid candidate'}
$projectRoot=Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
$utf8=[Text.UTF8Encoding]::new($false)
$evidence=Join-Path $projectRoot ".cache/release-v1.0.0-$Candidate/evidence"
New-Item -ItemType Directory -Force -Path $evidence | Out-Null
$env:GOCACHE=Join-Path $projectRoot '.cache/go-build'
$env:XRAY_BINARY=Join-Path $projectRoot '.tools/xray/bin/xray.exe'
$env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
foreach ($helper in @('ZIC_AUDIT_GRPC_HELPER','ZIC_AUDIT_PANIC_HELPER','ZIC_DAEMON_CRASH_HELPER','ZIC_SEND_CRASH_HELPER')) {Remove-Item -LiteralPath "Env:$helper" -ErrorAction SilentlyContinue}
$bash='C:/Program Files/Git/bin/bash.exe'
if (!(Test-Path -LiteralPath $bash)) {throw 'This host runner requires Git Bash; use Linux runner separately'}
function Hash($path) {(Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()}
function Inputs {
    $files=@(Get-Item AGENTS.md,go.mod,go.sum,install.sh)
    foreach ($dir in @('cmd','internal','packaging','scripts')) {$files+=Get-ChildItem -LiteralPath $dir -File -Recurse | Where-Object {$_.Extension -in '.go','.sh','.ps1','.service'}}
    @($files | Sort-Object FullName | ForEach-Object {"$(Hash $_.FullName)  $($_.FullName.Substring($projectRoot.Length+1).Replace('\','/'))"})
}
$before=Inputs
& go test -count=1 -v ./... *> (Join-Path $evidence 'windows-go-tests.log')
if ($LASTEXITCODE -ne 0) {throw 'Native tests failed; inspect private evidence without publishing raw payloads'}
$goLog=[IO.File]::ReadAllText((Join-Path $evidence 'windows-go-tests.log'))
$packages=[regex]::Matches($goLog,'(?m)^ok\s+zic-xray-agent/').Count
$passes=[regex]::Matches($goLog,'(?m)^--- PASS:').Count
$skips=@([regex]::Matches($goLog,'(?m)^--- SKIP: (\S+)') | ForEach-Object {$_.Groups[1].Value})
if ($packages -ne 11 -or $skips.Count -ne 1 -or $skips[0] -ne 'TestLinuxReadOnlyFDAndIOPlateau') {throw 'Native test result/skip mismatch'}
Write-Output "Native packages PASS=$packages; top-level PASS=$passes; Linux-only SKIP=$($skips.Count)"
& $bash scripts/test-installer.sh *> (Join-Path $evidence 'windows-shell-tests.log')
if ($LASTEXITCODE -ne 0) {throw 'Installer mock failed'}
& $bash scripts/test-lab-startup.sh *> (Join-Path $evidence 'windows-lab-startup-tests.log')
if ($LASTEXITCODE -ne 0) {throw 'Startup mock failed'}
& $bash scripts/test-debian-runner-guards.sh *> (Join-Path $evidence 'debian-runner-guards.log')
if ($LASTEXITCODE -ne 0) {throw 'Debian runner guard mock failed'}
Write-Output 'Installer/startup/readiness/PID/rollback mocks PASS (no real services)'
$syntax=@()
foreach ($file in @(Get-Item install.sh)+@(Get-ChildItem scripts,packaging -Filter *.sh -File -Recurse)) {
    & $bash -n $file.FullName
    if ($LASTEXITCODE -ne 0) {throw 'Bash syntax failed'}
    $syntax+=$file.FullName.Substring($projectRoot.Length+1)
}
[IO.File]::WriteAllText((Join-Path $evidence 'bash-syntax.log'),"PASS $($syntax.Count) scripts`n",$utf8)
$vet=@()
foreach ($target in @('windows/amd64','linux/amd64','linux/arm64')) {
    $parts=$target.Split('/');$env:GOOS=$parts[0];$env:GOARCH=$parts[1]
    & go vet ./... *> (Join-Path $evidence ("vet-"+$target.Replace('/','-')+'.log'))
    if ($LASTEXITCODE -ne 0) {throw 'Vet failed'}
    $vet+="$target PASS"
}
[IO.File]::WriteAllText((Join-Path $evidence 'vet.log'),($vet -join "`n")+"`n",$utf8)
$env:GOOS='windows';$env:GOARCH='amd64'
& powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -IncludeTests *> (Join-Path $evidence 'build.log')
if ($LASTEXITCODE -ne 0) {throw 'Linux cross-build failed'}
Write-Output 'Vet three targets and Linux cross-build PASS; Linux execution NOT RUN'
& go test -run '^$' -bench '^BenchmarkEnqueueIdle$' -benchmem -benchtime=1s -count=3 ./internal/accounting *> (Join-Path $evidence 'enqueue-current.log')
if ($LASTEXITCODE -ne 0) {throw 'Idle benchmark failed'}
# Fixed-path source inspection supplements runtime canary tests; it does not
# claim to inspect journal/cores or active Xray on another host.
$main=[IO.File]::ReadAllText((Join-Path $projectRoot 'cmd/zic-xray-agent/main.go'))
$core=[IO.File]::ReadAllText((Join-Path $projectRoot 'internal/installcheck/core.go'))
if (!$main.Contains('logging.Diagnostic(err)') -or !$core.Contains('command.Stdin = bytes.NewReader(raw)') -or !$core.Contains('command.Stdout, command.Stderr = io.Discard, io.Discard')) {throw 'Safe diagnostic/core stdin boundary missing'}
foreach ($file in Get-ChildItem packaging -Filter *.service -File -Recurse) {
    $unit=[IO.File]::ReadAllText($file.FullName)
    foreach ($directive in @('DynamicUser=yes','LoadCredential=','NoNewPrivileges=yes','ProtectSystem=strict','PrivateTmp=yes','StandardOutput=null','StandardError=null','LimitCORE=0','CoredumpFilter=0x0','GOTRACEBACK=none','MemoryMax=','TasksMax=')) {if (!$unit.Contains($directive)) {throw 'Required privacy/sandbox/resource directive missing'}}
}
foreach ($file in @('internal/lab/config.go','internal/installcheck/core.go')) {
    $s=[IO.File]::ReadAllText((Join-Path $projectRoot $file))
    foreach ($literal in @('"loglevel": "none"','"access": "none"','"error": "none"','"dnsLog": false')) {if (!$s.Contains($literal)) {throw 'Generated Xray no-log policy missing'}}
}
$forbidden=@('[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}','\b(?:\d{1,3}\.){3}\d{1,3}\b','[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+','-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----','https?://\S*\?\S*(?:token|key|password)=','private-api-canary|private-key-canary|private-panic-canary|private-grpc-canary|hwid-canary|visited-canary|dns-canary|not-for-output-resource-canary|FAKE-API-KEY-not-production')
foreach ($file in Get-ChildItem -LiteralPath $evidence -Filter *.log -File) {
    $s=[IO.File]::ReadAllText($file.FullName)
    foreach ($pattern in $forbidden) {if ([regex]::IsMatch($s,$pattern)) {throw 'Evidence privacy scan failed; values suppressed'}}
    [IO.File]::WriteAllText($file.FullName,$s,$utf8)
}
$after=Inputs
if (($before -join "`n") -cne ($after -join "`n")) {throw 'Inputs changed during tests'}
[IO.File]::WriteAllText((Join-Path $evidence 'TESTED_INPUTS.sha256'),($after -join "`n")+"`n",$utf8)
$metrics=[regex]::Match($goLog,'host=windows/amd64 polls=300\s+goroutines_before=(\d+) goroutines_after=(\d+) heap_alloc_before_gc_bytes=(\d+) heap_alloc_after_gc_bytes=(\d+)').Value
$receipt=[ordered]@{candidate=$Candidate;host='windows/amd64';compiler=(& go version);native_pass=$true;native_packages=$packages;top_level_pass=$passes;linux_only_skips=$skips;mocks_pass=$true;vet_pass=$true;build_pass=$true;log_path_checks_pass=$true;evidence_canary_scan='PASS';api_key_input='visible normal read; no app echo, argv or exported environment';short_poll_test_process_metrics=$metrics;linux_execution='NOT RUN';systemd_tty_reboot_rollback='NOT RUN';vps_ram_cpu_ssd_fd_soak_network='NOT RUN';race_detector='NOT RUN: suitable C compiler unavailable';github_writes=$false;production_ready=$false}
[IO.File]::WriteAllText((Join-Path $evidence 'TEST_EXECUTION.json'),($receipt|ConvertTo-Json -Depth 5)+"`n",$utf8)
Write-Output 'Current input hashes recorded; log paths/canary evidence checks PASS; candidate still HOLD for Linux/VPS'
