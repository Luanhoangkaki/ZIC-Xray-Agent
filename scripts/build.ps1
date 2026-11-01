param([switch]$IncludeTests)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
    $env:GOOS = 'linux'
    $env:CGO_ENABLED = '0'
    $checksums = @()
    foreach ($architecture in @('amd64', 'arm64')) {
        $env:GOARCH = $architecture
        $directory = Join-Path $projectRoot "dist/linux-$architecture"
        New-Item -ItemType Directory -Force -Path $directory | Out-Null
        foreach ($component in @('zic-xray-agent', 'zic-bench','zic-lab')) {
            $target = Join-Path $directory $component
            & go build -buildvcs=false -trimpath -ldflags '-s -w' -o $target "./cmd/$component"
            if ($LASTEXITCODE -ne 0) { throw "Build failed for $architecture/$component" }
            $hash = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant()
            $checksums += "$hash  linux-$architecture/$component"
        }
        if ($IncludeTests) {
            $testDirectory = Join-Path $projectRoot "dist/lab-tests/linux-$architecture"
            New-Item -ItemType Directory -Force -Path $testDirectory | Out-Null
            $packages = & go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...
            if ($LASTEXITCODE -ne 0) { throw 'Cannot list test packages' }
            foreach ($package in $packages) {
                if ([string]::IsNullOrWhiteSpace($package)) { continue }
                $name = $package.Substring('zic-xray-agent/'.Length).Replace('/', '-') + '.test'
                $target = Join-Path $testDirectory $name
                & go test -c -buildvcs=false -trimpath -ldflags '-s -w' -o $target $package
                if ($LASTEXITCODE -ne 0) { throw "Test build failed for $architecture/$package" }
                $hash = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant()
                $checksums += "$hash  lab-tests/linux-$architecture/$name"
            }
        }
    }
    [IO.File]::WriteAllText((Join-Path $projectRoot 'dist/SHA256SUMS'), (($checksums -join "`n") + "`n"), [Text.UTF8Encoding]::new($false))
} finally {
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    $env:CGO_ENABLED = $previousCGO
    Pop-Location
}
