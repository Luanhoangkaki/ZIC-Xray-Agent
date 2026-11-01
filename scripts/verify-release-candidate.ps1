param([string]$Candidate = 'audit3-test', [string]$ReleaseTag = 'v1.0.1-audit3-test')
$ErrorActionPreference='Stop'
if ($Candidate -notmatch '^[a-z0-9-]{1,24}$') {throw 'Invalid candidate'}
if ($ReleaseTag -cne 'v1.0.1-audit3-test') {throw 'Only the new audit3 test tag is allowed'}
$projectRoot=Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
$output=Join-Path $projectRoot "dist/release-candidates/v1.0.1-$Candidate"
$utf8=[Text.UTF8Encoding]::new($false)
function Hash($path) {(Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()}
function Write-JSON($path,$value) {[IO.File]::WriteAllText($path,($value|ConvertTo-Json -Depth 9)+"`n",$utf8)}
if (Test-Path -LiteralPath (Join-Path $output 'READY_FOR_REVIEW.txt')) {throw 'Candidate already finalized; preserve reviewed files'}
$index=Get-Content -Raw -LiteralPath (Join-Path $output 'ASSETS.json') | ConvertFrom-Json
if ($index.release_tag -ne $releaseTag -or $index.repository -ne 'Luanhoangkaki/ZIC-Xray-Agent') {throw 'Wrong test release target'}
foreach ($asset in $index.assets) {
    if ($asset.name -notmatch '^[a-zA-Z0-9_.-]+$' -or $asset.url -cne "https://github.com/Luanhoangkaki/ZIC-Xray-Agent/releases/download/$releaseTag/$($asset.name)") {throw 'Asset URL is not pinned to the test release'}
}
$bootstrap=[IO.File]::ReadAllText((Join-Path $output 'get-zic-xray-agent-v1.0.1.sh'))
if (!$bootstrap.Contains("readonly release_tag='$releaseTag'")) {throw 'Bootstrap test tag pin missing'}
$records=@();$sourceRecords=@();$sourceFiles=@()
$rules=[ordered]@{
    private_key_header='-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----'
    credential_json='(?i)"(?:server_token|api_key|password|private_key|access_token)"\s*:\s*"[^"\s][^"]*"'
    cloud_token='(?:ghp_[0-9A-Za-z]{36}|github_pat_[0-9A-Za-z_]{40,}|AKIA[0-9A-Z]{16})'
}
foreach ($line in Get-Content -LiteralPath (Join-Path $output 'SOURCE_FILES.sha256')) {
    if ($line -notmatch '^([a-f0-9]{64})  (.+)$') {throw 'Invalid source manifest'}
    $sha=$Matches[1];$relative=$Matches[2]
    if ($relative -notin @('AGENTS.md','README.md','go.mod','go.sum','install.sh','.gitignore','.gitattributes') -and $relative -notmatch '^(?:cmd|internal|packaging|scripts|docs)/[a-zA-Z0-9_./-]+\.(?:go|sh|ps1|md|service)$') {throw 'Unapproved source path'}
    if ($relative -match '(^|/)\.\.?(/|$)') {throw 'Unsafe source path'}
    $path=Join-Path $projectRoot $relative
    if ((Hash $path) -ne $sha) {throw 'Source differs from packaged manifest'}
    if ((Get-Item -LiteralPath $path).Attributes -band [IO.FileAttributes]::ReparsePoint) {throw 'Source symlink rejected'}
    $text=[IO.File]::ReadAllText($path);$findings=@{}
    foreach ($rule in $rules.Keys) {
        $hits=[regex]::Matches($text,$rules[$rule]);$findings[$rule]=$hits.Count
        foreach ($hit in $hits) {
            # Exact shell printf format, NOT a credential. Do not exempt a
            # whole test directory/file or accept unknown literals there.
            if ($rule -eq 'credential_json' -and $relative -eq 'scripts/test-installer.sh' -and $hit.Value -ceq ('"server_token":'+'"%s"')) {continue}
            throw "Source pattern scan rejected $relative ($rule); value suppressed"
        }
    }
    $sourceRecords+=@{file=$relative;sha256=$sha;findings=$findings}
    $sourceFiles+=$relative
}
$parent=Join-Path $projectRoot ('.cache/candidate-verification/'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $parent | Out-Null
foreach ($arch in @('amd64','arm64')) {
    $name="zic-xray-agent-installer-debian12-$arch-v1.0.1"
    $archive=Join-Path $output "$name.tar.gz"
    $asset=@($index.assets | Where-Object {$_.name -eq "$name.tar.gz"})
    if ($asset.Count -ne 1 -or (Hash $archive) -ne $asset[0].sha256) {throw 'Archive hash/index mismatch'}
    $members=@(& tar -tzf $archive)
    if ($LASTEXITCODE -ne 0 -or $members.Count -gt 256) {throw 'Cannot inspect archive'}
    foreach ($member in $members) {if ($member -notmatch ('^'+[regex]::Escape($name)+'/[a-zA-Z0-9_./-]*$') -or $member -match '(^|/)\.\.?(/|$)') {throw 'Unsafe archive path'}}
    $types=@(& tar -tvzf $archive)
    if ($LASTEXITCODE -ne 0) {throw 'Cannot inspect archive types'}
    foreach ($entry in $types) {if ($entry -notmatch '^[-d]') {throw 'Nonregular member rejected'}}
    foreach ($relative in @("validation/linux-$arch/xray","bin/linux-$arch/zic-xray-agent",'install.sh','rollback-install.sh')) {
        $entry=@($types | Where-Object { $_.EndsWith("$name/$relative") })
        if ($entry.Count -ne 1 -or $entry[0] -cnotmatch '^-rwx------\s') {throw 'Archive executable Unix mode must be 0700'}
    }
    if (@($members | Sort-Object -Unique).Count -ne $members.Count) {throw 'Duplicate archive member'}
    $target=Join-Path $parent $arch
    New-Item -ItemType Directory -Path $target | Out-Null
    & tar -xzf $archive -C $target
    if ($LASTEXITCODE -ne 0) {throw 'Extraction failed'}
    $stage=Join-Path $target $name
    $expected=@{}
    foreach ($line in Get-Content -LiteralPath (Join-Path $stage 'SHA256SUMS')) {
        if ($line -notmatch '^([a-f0-9]{64})  ([a-zA-Z0-9_./-]+)$') {throw 'Invalid inner manifest'}
        $sha=$Matches[1];$rel=$Matches[2]
        if ($rel -match '(^|/)\.\.?(/|$)' -or $expected.ContainsKey($rel)) {throw 'Unsafe/duplicate inner path'}
        $expected[$rel]=$sha
    }
    $actual=@(Get-ChildItem -LiteralPath $stage -File -Recurse)
    if ($actual.Count -ne $expected.Count+1) {throw 'Unmanifested/missing package member'}
    foreach ($file in $actual) {
        $rel=$file.FullName.Substring($stage.Length+1).Replace('\','/')
        if ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) {throw 'Package link rejected'}
        if ($rel -eq 'SHA256SUMS') {continue}
        if (!$expected.ContainsKey($rel) -or (Hash $file.FullName) -ne $expected[$rel]) {throw 'Extracted member checksum mismatch'}
        $raw=[Text.Encoding]::GetEncoding(28591).GetString([IO.File]::ReadAllBytes($file.FullName))
        foreach ($rule in $rules.Keys) {if ([regex]::IsMatch($raw,$rules[$rule])) {throw "Extracted package pattern rejected ($rule); values suppressed"}}
    }
    foreach ($pair in @(@("bin/linux-$arch/zic-xray-agent","dist/linux-$arch/zic-xray-agent"),@('install.sh','install.sh'),@('packaging/zic-xray-agent.service','packaging/zic-xray-agent.service'),@('rollback-install.sh','packaging/rollback-agent-install.sh'))) {
        # Text is LF-normalized by packaging; Go binary must match byte-for-byte.
        $packed=Join-Path $stage $pair[0];$source=Join-Path $projectRoot $pair[1]
        if ($pair[0] -match '^bin/') {if ((Hash $packed) -ne (Hash $source)) {throw 'Packaged binary differs from fresh build'}}
        elseif ([IO.File]::ReadAllText($packed) -cne [IO.File]::ReadAllText($source).Replace("`r`n","`n")) {throw 'Packaged script/unit differs from audited source'}
    }
    $agent=Join-Path $stage "bin/linux-$arch/zic-xray-agent"
    $version=@(& go version -m $agent)
    if ($LASTEXITCODE -ne 0 -or ($version -join "`n") -match 'github.com/xtls/xray-core|github.com/XrayR|V2Node') {throw 'Unexpected embedded/substitute core dependency'}
    foreach ($binary in @("bin/linux-$arch/zic-xray-agent","validation/linux-$arch/xray")) {
        $stream=[IO.File]::OpenRead((Join-Path $stage $binary))
        try {$h=New-Object byte[] 20;$read=$stream.Read($h,0,20)} finally {$stream.Dispose()}
        $want=62;if ($arch -eq 'arm64') {$want=183}
        if ($read -ne 20 -or $h[0] -ne 127 -or $h[1] -ne 69 -or $h[2] -ne 76 -or $h[3] -ne 70 -or $h[4] -ne 2 -or $h[5] -ne 1 -or ($h[18]+256*$h[19]) -ne $want) {throw 'Packaged ELF architecture mismatch'}
    }
    $build=Get-Content -Raw -LiteralPath (Join-Path $stage 'BUILD.json') | ConvertFrom-Json
    if ((Hash (Join-Path $stage "validation/linux-$arch/xray")) -ne $build.core_sha256 -or $build.agent_mode -ne 'observe' -or $build.traffic_post_enabled -or $build.production_ready -or $build.release_tag -ne $index.release_tag) {throw 'Core hash, mode or release tag mismatch'}
    if ($build.source_commit -cne $index.source_commit -or $build.go_buildvcs -ne $false -or ($version -join "`n") -match '\bvcs(?:\.|=)') {throw 'Source provenance or buildvcs mismatch'}
    if ($build.release_version -cne 'v1.0.1' -or $build.agent_version -cne '1.0.1-audit3-test') {throw 'Agent/package version mismatch'}
    $records+=@{target=$arch;archive_sha256=(Hash $archive);regular_members=$actual.Count;inner_checksums='PASS';source_binary_unit_installer_match='PASS';elf='PASS';unix_executable_modes='PASS 0700 archive headers';unpacked_secret_patterns='PASS';linux_execution='NOT RUN'}
}
# Independent downloader tests use local curl/systemctl mocks and actual GNU
# tar/SHA. No installer or network is executed by this verification.
$bash='C:/Program Files/Git/bin/bash.exe'
& $bash scripts/test-release-downloader.sh $output $releaseTag *> (Join-Path $output 'downloader-mock.log')
if ($LASTEXITCODE -ne 0) {throw 'Candidate downloader test failed'}
$sourceName="zic-xray-agent-source-v1.0.1-$Candidate"
$sourceStage=Join-Path $parent $sourceName
New-Item -ItemType Directory -Path $sourceStage | Out-Null
foreach ($rel in $sourceFiles) {
    $dest=Join-Path $sourceStage $rel
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $dest) | Out-Null
    Copy-Item -LiteralPath (Join-Path $projectRoot $rel) -Destination $dest
}
Copy-Item -LiteralPath (Join-Path $output 'SOURCE_FILES.sha256') -Destination (Join-Path $sourceStage 'SOURCE_FILES.sha256')
$sourceArchive=Join-Path $output "$sourceName.tar.gz"
& tar -czf $sourceArchive -C $parent $sourceName
if ($LASTEXITCODE -ne 0) {throw 'Cannot package reviewed source'}
$sourceCheck=Join-Path $parent 'source-unpacked'
New-Item -ItemType Directory -Path $sourceCheck | Out-Null
& tar -xzf $sourceArchive -C $sourceCheck
if ($LASTEXITCODE -ne 0) {throw 'Cannot verify source archive'}
$sourceUnpacked=Join-Path $sourceCheck $sourceName
if (@(Get-ChildItem -LiteralPath $sourceUnpacked -Recurse -File).Count -ne $sourceFiles.Count+1) {throw 'Source archive member mismatch'}
foreach ($record in $sourceRecords) {if ((Hash (Join-Path $sourceUnpacked $record.file)) -ne $record.sha256) {throw 'Unpacked source differs from reviewed manifest'}}
[IO.File]::WriteAllText("$sourceArchive.sha256","$(Hash $sourceArchive)  $sourceName.tar.gz`n",$utf8)
$baseline=@(& git rev-parse HEAD)
if ($LASTEXITCODE -ne 0) {throw 'Cannot read local baseline commit'}
Write-JSON (Join-Path $output 'SOURCE_SECURITY_SCAN.json') @{status='PASS fixed allowlist and pattern scan';source_files=$sourceFiles.Count;records=$sourceRecords;reviewed_exception='one exact shell printf placeholder, not a secret';source_archive="$sourceName.tar.gz";source_archive_sha256=(Hash $sourceArchive);baseline_commit=$baseline[0];working_tree_dirty=$true;release_commit=$null;proposed_source_commit=$index.source_commit;limitations='Pattern scans cannot prove universal absence. Public synthetic test fixtures remain source only; no runtime data, Git history or private config is included.'}
Write-JSON (Join-Path $output 'PACKAGE_CHECK.json') @{status='PASS local archive verification';packages=$records;downloader_mocks='PASS both targets/tamper/active-Lab guard';linux_systemd='NOT RUN';production_ready=$false}
$sums=@($index.assets | Sort-Object name | ForEach-Object {if ((Hash (Join-Path $output $_.name)) -ne $_.sha256) {throw 'Final asset drift'};"$($_.sha256)  $($_.name)"})
[IO.File]::WriteAllText((Join-Path $output 'SHA256SUMS'),($sums -join "`n")+"`n",$utf8)
[IO.File]::WriteAllText((Join-Path $output 'READY_FOR_REVIEW.txt'),"LOCAL CANDIDATE $Candidate verified. HOLD: Linux/systemd/VPS/owner approval. No GitHub writes or service changes. Source archive is local review material, not an installer asset.`n",$utf8)
# Hash the bootstrap in the one-line command too; transport alone is not an
# independent pin. This command is a future publication template, not run here.
$scriptName='get-zic-xray-agent-v1.0.1.sh';$pin=Hash (Join-Path $output $scriptName)
$oneLine='umask 077; zic_dl=$(mktemp) && curl --disable --fail --silent --show-error --location --proto ''=https'' --proto-redir ''=https'' --connect-timeout 10 --max-time 300 --max-filesize 65536 --output "$zic_dl" https://github.com/Luanhoangkaki/ZIC-Xray-Agent/releases/download/@RELEASE_TAG@/get-zic-xray-agent-v1.0.1.sh && printf ''{0}  %s\n'' "$zic_dl" | sha256sum --check --strict && bash "$zic_dl" --install'
[IO.File]::WriteAllText((Join-Path $output 'ONE_LINE_AFTER_APPROVAL.txt'),"NOT PUBLISHED. Run only after matching assets are approved/published and on an unused test VPS. API Key is visibly entered on TTY. No command below has been executed.`n`n"+($oneLine.Replace('@RELEASE_TAG@',$releaseTag) -f $pin)+"`n",$utf8)
Write-Output "Verified candidate archives, source patterns, exact packaged binary/source, ELF and downloader. Source files=$($sourceFiles.Count). HOLD Linux/systemd/VPS/approval."
