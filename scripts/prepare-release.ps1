param([string]$Version = 'v1.0.1', [string]$Candidate = 'audit3-test', [string]$ReleaseTag = 'v1.0.1-audit3-test', [string]$SourceCommit = '')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
$env:GOCACHE = Join-Path $projectRoot '.cache/go-build'
if ($Version -ne 'v1.0.1') { throw 'This reviewed release profile is pinned to v1.0.1' }
 $versionSource=[IO.File]::ReadAllText((Join-Path $projectRoot 'internal/buildinfo/version.go'))
 $agentVersion=[regex]::Match($versionSource,'const Version = "([^"]+)"').Groups[1].Value
 $baseVersion=[regex]::Match($versionSource,'const BaseVersion = "([^"]+)"').Groups[1].Value
 if ($Version -cne $baseVersion -or $ReleaseTag -cne ('v'+$agentVersion)) {throw 'Go source and packaging version mismatch'}
if ($Candidate -notmatch '^[a-z0-9-]{1,24}$') { throw 'Invalid local candidate identifier' }
if ($ReleaseTag -cne 'v1.0.1-audit3-test') { throw 'Only the new audit3 test tag is allowed' }
if ($SourceCommit -ne '' -and $SourceCommit -cnotmatch '^[a-f0-9]{40}$') { throw 'Invalid source commit' }
$commitValue=$null
if ($SourceCommit -ne '') {$commitValue=$SourceCommit}
$repository = 'Luanhoangkaki/ZIC-Xray-Agent'
$output = Join-Path $projectRoot "dist/release-candidates/$Version-$Candidate"
if (Test-Path -LiteralPath $output) { throw 'Review folder exists; preserve reviewed bytes instead of overwriting' }
$utf8 = [Text.UTF8Encoding]::new($false)
function Write-LF([string]$Path, [string]$Value) { [IO.File]::WriteAllText($Path, $Value.Replace("`r`n", "`n"), $utf8) }
function Hash([string]$Path) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
function Copy-Text([string]$Source, [string]$Target) {
    if ((Get-Item -LiteralPath $Source).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing source symlink' }
    Write-LF $Target ([IO.File]::ReadAllText($Source))
}
$evidence = Join-Path $projectRoot ".cache/release-$Version-$Candidate/evidence"
foreach ($file in @('windows-go-tests.log','windows-shell-tests.log','windows-lab-startup-tests.log','vet.log','bash-syntax.log')) {
    if (!(Test-Path -LiteralPath (Join-Path $evidence $file))) { throw "Missing test evidence: $file" }
}
$goLog = [IO.File]::ReadAllText((Join-Path $evidence 'windows-go-tests.log'))
if ([regex]::Matches($goLog, '(?m)^ok\s+zic-xray-agent/').Count -ne 12 -or $goLog -match '(?m)^FAIL') { throw 'Native Go evidence is not twelve successful packages' }
$skips=@([regex]::Matches($goLog, '(?m)^--- SKIP: (\S+)') | ForEach-Object {$_.Groups[1].Value})
if ($skips.Count -ne 1 -or $skips[0] -ne 'TestLinuxReadOnlyFDAndIOPlateau') { throw 'Unexpected native skip; review required' }
$tested=Get-Content -Raw -LiteralPath (Join-Path $evidence 'TEST_EXECUTION.json') | ConvertFrom-Json
if (!$tested.native_pass -or !$tested.mocks_pass -or !$tested.vet_pass -or !$tested.build_pass -or !$tested.log_path_checks_pass) {throw 'Missing current test execution receipt'}
foreach ($line in Get-Content -LiteralPath (Join-Path $evidence 'TESTED_INPUTS.sha256')) {
    if ($line -notmatch '^([a-f0-9]{64})  (.+)$') {throw 'Invalid tested input manifest'}
    if ((Hash (Join-Path $projectRoot $Matches[2])) -ne $Matches[1]) {throw 'Tested source changed; rerun candidate tests'}
}
foreach ($file in @('windows-shell-tests.log','windows-lab-startup-tests.log')) {
    if ([IO.File]::ReadAllText((Join-Path $evidence $file)) -notmatch '(?m)regressions PASS') { throw "Mock test evidence incomplete: $file" }
}
New-Item -ItemType Directory -Force $output | Out-Null
foreach ($file in @('INSTALL.md','UPDATE_ROLLBACK.md','RELEASE_NOTES.md','PUBLISH_CHECKLIST.md','RELEASE_GATE.md')) {
    Copy-Text (Join-Path $projectRoot "docs/releases/$Version/$file") (Join-Path $output $file)
}

# Source provenance only, NOT a fabricated Git commit. Do not read .git, .env,
# .tools, .cache, panel-reference, credentials, runtime state or any backups.
$sources = @()
foreach ($file in @('AGENTS.md','README.md','go.mod','go.sum','install.sh','.gitignore','.gitattributes')) { $sources += Get-Item -LiteralPath (Join-Path $projectRoot $file) }
foreach ($dir in @('cmd','internal','scripts','packaging','docs')) {
    $sources += Get-ChildItem -LiteralPath (Join-Path $projectRoot $dir) -Recurse -File | Where-Object { $_.Extension -in @('.go','.sh','.ps1','.md','.service') }
}
$sourceChecksums = @()
foreach ($file in ($sources | Sort-Object FullName -Unique)) {
    if ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Source manifest contains a symlink' }
    $relative = $file.FullName.Substring($projectRoot.Length+1).Replace('\','/')
    $sourceChecksums += (Hash $file.FullName) + '  ' + $relative
}
Write-LF (Join-Path $output 'SOURCE_FILES.sha256') (($sourceChecksums -join "`n") + "`n")
$sourceHash = Hash (Join-Path $output 'SOURCE_FILES.sha256')
$buildManifest = Get-Content -LiteralPath dist/SHA256SUMS
$buildArtifacts = @()
foreach ($line in $buildManifest) {
    if ($line -notmatch '^([a-f0-9]{64})  ((?:linux-|lab-tests/linux-)(?:amd64|arm64)/[a-z0-9.-]+)$') { throw 'Unsafe build manifest entry' }
    $hash = $Matches[1]; $relative = $Matches[2]
    $file = Join-Path $projectRoot "dist/$relative"
    if ((Hash $file) -ne $hash) { throw "Build checksum mismatch: $relative" }
    $stream = [IO.File]::OpenRead($file)
    try { $header = New-Object byte[] 20; if ($stream.Read($header,0,20) -ne 20) { throw 'Short ELF' } } finally { $stream.Dispose() }
    $machine = [int]$header[18] + 256*[int]$header[19]; $want = 62
    if ($relative.Contains('linux-arm64/')) { $want = 183 }
    if ($header[0] -ne 127 -or $header[1] -ne 69 -or $header[2] -ne 76 -or $header[3] -ne 70 -or $header[4] -ne 2 -or $header[5] -ne 1 -or $machine -ne $want) { throw "Wrong ELF target: $relative" }
    $buildArtifacts += @{file=$relative;sha256=$hash;elf_machine=$machine}
}
if ($buildArtifacts.Count -ne 30 -or @($buildArtifacts | Where-Object {$_.file.StartsWith('lab-tests/')}).Count -ne 24) { throw 'Expected 6 tools and 24 test binaries' }
$releaseMetadata = Get-Content -Raw dist/verification/xray-release.json | ConvertFrom-Json
if ($releaseMetadata.tag_name -ne 'v26.10.29') { throw 'Unexpected official core metadata' }
$modules = @(& go list -deps -f '{{with .Module}}{{.Path}}|{{.Dir}}{{end}}' ./...)
if ($LASTEXITCODE -ne 0) { throw 'Cannot list dependency licenses' }
$goRoot = & go env GOROOT
if ($LASTEXITCODE -ne 0) { throw 'Cannot locate Go standard library license' }
Add-Type -AssemblyName System.IO.Compression.FileSystem
$secretRules = [ordered]@{
    private_key_header='-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----'
    credential_json='(?i)"(?:server_token|api_key|password|private_key|access_token)"\s*:\s*"[^"\s][^"]*"'
    known_token_prefix='(?:ghp_[0-9A-Za-z]{36}|github_pat_[0-9A-Za-z_]{40,}|AKIA[0-9A-Z]{16})'
}
$scanRecords = @(); $assets = @(); $packages = @(); $packageHashes = @{}
foreach ($arch in @('amd64','arm64')) {
    $name = "zic-xray-agent-installer-debian12-$arch-$Version"
    $parent = Join-Path $projectRoot ('.cache/release-packaging/' + [guid]::NewGuid().ToString('N'))
    $stage = Join-Path $parent $name
    foreach ($dir in @("bin/linux-$arch","validation/linux-$arch",'packaging','licenses')) { New-Item -ItemType Directory -Force (Join-Path $stage $dir) | Out-Null }
    Copy-Text (Join-Path $projectRoot 'install.sh') (Join-Path $stage 'install.sh')
    Copy-Text (Join-Path $projectRoot 'packaging/rollback-agent-install.sh') (Join-Path $stage 'rollback-install.sh')
    Copy-Text (Join-Path $projectRoot 'packaging/zic-xray-agent.service') (Join-Path $stage 'packaging/zic-xray-agent.service')
    Copy-Text (Join-Path $output 'INSTALL.md') (Join-Path $stage 'README.md')
    Copy-Text (Join-Path $output 'UPDATE_ROLLBACK.md') (Join-Path $stage 'UPDATE_ROLLBACK.md')
    Copy-Item -LiteralPath (Join-Path $output 'SOURCE_FILES.sha256') -Destination (Join-Path $stage 'SOURCE_FILES.sha256')
    Copy-Item -LiteralPath "dist/linux-$arch/zic-xray-agent" -Destination (Join-Path $stage "bin/linux-$arch/zic-xray-agent")
    $zipName='Xray-linux-64.zip'; $zipDir='.tools/xray-linux'; $zipHash='8a5f3e6a1057e4bd0f8a7c57c9d91204b4be0b74222a6e210d070603101163c2'; $coreHash='e783b9d2b46ed1ab3617c59b04dfbce38c999322d018c6e060f18d88bf6158aa'
    if ($arch -eq 'arm64') { $zipName='Xray-linux-arm64-v8a.zip'; $zipDir='.tools/xray-linux-arm64'; $zipHash='afe01f19045b05237420328028ee1ee2e1edd2100eb41a8cbbf50e683b0a6e58'; $coreHash='f02fa77fa2f1a0f8f2b4fd69af5000494bcc3fa4e97899caf023c7f72fd74474' }
    $zipPath=Join-Path $projectRoot "$zipDir/$zipName"
    $asset=@($releaseMetadata.assets | Where-Object {$_.name -eq $zipName})
    if ((Hash $zipPath) -ne $zipHash -or $asset.Count -ne 1 -or $asset[0].digest -ne "sha256:$zipHash" -or $asset[0].browser_download_url -ne "https://github.com/XTLS/Xray-core/releases/download/v26.10.29/$zipName") { throw 'Official core archive verification failed' }
    $zip=[IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
        [IO.Compression.ZipFileExtensions]::ExtractToFile($zip.GetEntry('xray'), (Join-Path $stage "validation/linux-$arch/xray"))
        [IO.Compression.ZipFileExtensions]::ExtractToFile($zip.GetEntry('LICENSE'), (Join-Path $stage 'licenses/Xray-core-LICENSE'))
    } finally { $zip.Dispose() }
    if ((Hash (Join-Path $stage "validation/linux-$arch/xray")) -ne $coreHash) { throw 'Official core executable verification failed' }
    Write-LF (Join-Path $stage 'licenses/Xray-core-SOURCE.txt') "Unmodified official Xray-core v26.10.29, offline validation only.`nSource: https://github.com/XTLS/Xray-core/tree/v26.10.29`nArchive: $($asset[0].browser_download_url)`n"
    Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination (Join-Path $stage 'licenses/Go-LICENSE')
    foreach ($module in ($modules | Sort-Object -Unique)) {
        $parts=$module.Split('|'); if ($parts.Length -ne 2 -or !$parts[1]) { continue }
        $license=Join-Path $parts[1] 'LICENSE'
        if (Test-Path -LiteralPath $license) { Copy-Item -LiteralPath $license -Destination (Join-Path $stage ('licenses/' + $parts[0].Replace('/','_') + '-LICENSE')) }
    }
$metadata=[ordered]@{release_version=$Version;agent_version=$agentVersion;release_tag=$releaseTag;candidate=$Candidate;repository=$repository;target="linux/$arch";compiler=(& go version);source_commit=$commitValue;source_tree_manifest_sha256=$sourceHash;go_buildvcs=$false;agent_mode='observe';traffic_post_enabled=$false;production_ready=$false;api_key_input='visible TTY read; stdin only; never echoed by program';supported_adapter='VLESS subset only; Linux acceptance pending';incomplete_adapters=@('shadowsocks','trojan','hysteria1','hysteria2');core_version='v26.10.29';core_sha256=$coreHash;core_use='offline run -test only; never installed as a service';linux_tests='NOT RUN';debian12_systemd='NOT RUN';vps_measurements='NOT RUN';test_binaries_packaged=$false;runtime_data_packaged=$false}
    Write-LF (Join-Path $stage 'BUILD.json') ($metadata | ConvertTo-Json)
    $allowed=@('install.sh','rollback-install.sh','README.md','UPDATE_ROLLBACK.md','SOURCE_FILES.sha256','BUILD.json',"bin/linux-$arch/zic-xray-agent","validation/linux-$arch/xray",'packaging/zic-xray-agent.service')
    $checksums=@()
    foreach ($file in Get-ChildItem -LiteralPath $stage -Recurse -File | Sort-Object FullName) {
        $relative=$file.FullName.Substring($stage.Length+1).Replace('\','/')
        if ($relative -notin $allowed -and $relative -notmatch '^licenses/[a-zA-Z0-9_.-]+$') { throw "Unapproved release member: $relative" }
        if ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Release contains a symlink' }
        if ($file.Length -gt 128MB) { throw 'Release member too large' }
        $raw=[Text.Encoding]::GetEncoding(28591).GetString([IO.File]::ReadAllBytes($file.FullName))
        $counts=@{}
        foreach ($rule in $secretRules.Keys) {
            $counts[$rule]=[regex]::Matches($raw,$secretRules[$rule]).Count
            if ($counts[$rule] -ne 0) { throw "Release secret scan rejected: $relative ($rule); value suppressed" }
        }
        $scanRecords+=@{target=$arch;file=$relative;bytes=$file.Length;sha256=(Hash $file.FullName);findings=$counts}
        $checksums+=(Hash $file.FullName) + '  ' + $relative
    }
    Write-LF (Join-Path $stage 'SHA256SUMS') (($checksums -join "`n") + "`n")
    $archive=Join-Path $output "$name.tar.gz"
    & 'C:/Program Files/Git/bin/bash.exe' scripts/archive-installer.sh $parent.Replace('\','/') $name $arch $archive.Replace('\','/')
    if ($LASTEXITCODE -ne 0) { throw 'Cannot create release archive' }
    $hash=Hash $archive; $packageHashes[$arch]=$hash
    Write-LF "$archive.sha256" "$hash  $name.tar.gz`n"
    $packages+=@{architecture=$arch;archive="$name.tar.gz";sha256=$hash;bytes=(Get-Item -LiteralPath $archive).Length;members=$checksums.Count+1;stage=$stage}
    $assets+=@{name="$name.tar.gz";sha256=$hash;bytes=(Get-Item $archive).Length;url="https://github.com/$repository/releases/download/$releaseTag/$name.tar.gz"}
    $assets+=@{name="$name.tar.gz.sha256";sha256=(Hash "$archive.sha256");bytes=(Get-Item "$archive.sha256").Length;url="https://github.com/$repository/releases/download/$releaseTag/$name.tar.gz.sha256"}
}
$template=[IO.File]::ReadAllText((Join-Path $projectRoot 'scripts/download-release.template.sh'))
$downloader=$template.Replace('@RELEASE_TAG@',$releaseTag).Replace('@SHA256_AMD64@',$packageHashes['amd64']).Replace('@SHA256_ARM64@',$packageHashes['arm64'])
if ($downloader.Contains('@SHA256_')) { throw 'Unresolved downloader SHA pin' }
$downloaderPath=Join-Path $output "get-zic-xray-agent-$Version.sh"
Write-LF $downloaderPath $downloader
$downloadHash=Hash $downloaderPath
Write-LF "$downloaderPath.sha256" "$downloadHash  get-zic-xray-agent-$Version.sh`n"
foreach ($file in @((Split-Path -Leaf $downloaderPath),((Split-Path -Leaf $downloaderPath)+'.sha256'))) {
    $path=Join-Path $output $file
    $assets+=@{name=$file;sha256=(Hash $path);bytes=(Get-Item $path).Length;url="https://github.com/$repository/releases/download/$releaseTag/$file"}
}
$security=[ordered]@{status='PASS: allowlist + binary/text pattern scan; no matched secret patterns';scope='installer archives, regular files only, fixed allowlist';patterns=$secretRules;excluded=@('test executables and embedded test certificates','logs/evidence','source test fixtures','panel-reference','.cache','.tools except verified core','credentials/config/state/receipt/backups','.env/private certificate/key files');records=$scanRecords;limitations='Pattern scans are not a universal proof of absence. Source allowlist, build hashes and official core provenance also require review.'}
Write-LF (Join-Path $output 'SECURITY_SCAN.json') ($security | ConvertTo-Json -Depth 8)
$assetIndex=[ordered]@{version=$Version;release_tag=$releaseTag;repository=$repository;source_commit=$commitValue;source_tree_manifest_sha256=$sourceHash;github_writes_performed=$false;tag_created=$false;release_created=$false;published=$false;production_ready=$false;assets=$assets}
Write-LF (Join-Path $output 'ASSETS.json') ($assetIndex | ConvertTo-Json -Depth 6)
$buildReport=@{artifacts=$buildArtifacts;linux_test_binaries_cross_compiled=24;linux_execution='NOT RUN';debian12_systemd='NOT RUN';packages=@($packages | Select-Object architecture,archive,sha256,bytes,members);source_tree_manifest_sha256=$sourceHash}
Write-LF (Join-Path $output 'BUILD_AUDIT.json') ($buildReport | ConvertTo-Json -Depth 6)
# The result table distinguishes host execution, static inspection and pending
# Linux acceptance. Raw test logs stay local, outside the upload asset set.
$results=@'
# v1.0.1: kết quả kiểm thử

| Bài kiểm tra | Môi trường | Kết quả |
| --- | --- | --- |
| go test -count=1 -v ./...; 12 package có test, official Windows Xray v26.10.29 | Windows/amd64 | PASS package; 1 Linux /proc test SKIP (NOT RUN) |
| scripts/test-installer.sh: menu/input/secret handling/autostart/rollback | Git Bash + mocks, không systemd/TTY thật | PASS |
| scripts/test-lab-startup.sh: notify/PID/restart/rollback logic | Git Bash + mocks | PASS |
| Bash syntax các script; go vet windows/amd64 + linux/amd64 + linux/arm64 | Windows build host/static analysis | PASS |
| Build 6 tool + 24 test binary, SHA256 và ELF target | Windows cross-build/static inspection | PASS; KHÔNG phải chạy Linux |
| Linux-specific /proc/boot/permissions tests | Linux amd64/arm64 | NOT RUN: không có Linux/WSL/Docker được cài |
| 12 test binary từng target và runner Debian 12 | Linux amd64/arm64 | NOT RUN |
| Debian 12 systemd/DynamicUser/LoadCredential/visible TTY/install/start/restart/reboot | Debian 12 thật | NOT RUN |
| Cập nhật/rollback thực trên Debian 12 | Debian 12 thật | NOT RUN; chỉ mock/guide |
| VPS 1 vCPU/1 GB, RAM/CPU/10/20/30 kết nối/soak/recovery faults | VPS thật | NOT RUN |
| Downloader pins/tampering/extract-only/active-lab guard | Command mocks | Xem PACKAGE_CHECK.json và downloader-mock.log sau final verification |

Không tạo môi trường Linux giả rồi ghi PASS. Các file _linux_test.go không được chạy bởi go test Windows; compile cho Linux không thay thế thực thi. Không production-ready.

## Chạy các bài còn thiếu

Trên máy build Linux/source đã đối chiếu SOURCE_FILES.sha256 (không production/Lab active), cung cấp core official riêng và chạy go test -count=1 -v ./..., go vet ./..., bash scripts/test-installer.sh, bash scripts/test-lab-startup.sh. Chạy scripts/test-debian12.sh chỉ trên VM Debian 12 trống theo điều kiện script; kiểm tra mọi log/skip.

Không buộc VPS 1 GB phải cài compiler: 24 test binary đã cross-build ở dist/lab-tests trong workspace, 12/kiến trúc, có manifest dist/SHA256SUMS. Có thể chuyển riêng đúng bộ test/core đến máy Debian 12 test để chạy -test.v -test.timeout=120s. Chúng không là release installer asset vì thư viện test có thể chứa certificate/key fixture công khai. Không dùng user/token thật và không POST production.

Sau functional test, trên Debian 12 riêng được phép mới chạy installer với panel HTTPS mock/UUID giả, kiểm quyền 0700/0600, DynamicUser/LoadCredential, start/stop/restart/reboot, update/rollback. Giữ logs và ledger, không reset counter/state để làm PASS. Cập nhật bảng bằng evidence thực, không suy từ Windows/mocks.

Raw logs giữ trong evidence directory của candidate (local only), không đưa vào installer/asset upload. BUILD.json/ASSETS.json ghi source_commit được cung cấp (hoặc null nếu chưa chuẩn bị commit). SHA phải được đối chiếu với Git object và checkout sạch trong biên bản độc lập; không suy ra từ HEAD của workspace. SOURCE_FILES.sha256 ghi đúng byte source đã test; build dùng -buildvcs=false. Xem TEST_EXECUTION.json để biết số test và metric test process thực tế.
'@
Write-LF (Join-Path $output 'TEST_RESULTS.md') ($results + "`n")
Copy-Item -LiteralPath (Join-Path $evidence 'TEST_EXECUTION.json') -Destination (Join-Path $output 'TEST_EXECUTION.json')
Write-LF (Join-Path $output 'PREPARATION_STATUS.txt') "LOCAL CANDIDATE $Candidate ONLY. No push/tag/release/publish/deploy.`nLinux/systemd/VPS and owner approval remain HOLD.`nDownloader tests and extracted package audit must finish before READY_FOR_REVIEW.`n"
Write-Output "Prepared $output"
foreach ($package in $packages) { Write-Output "$($package.sha256)  $($package.archive)" }
