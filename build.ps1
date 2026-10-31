param(
    [string]$Version = '0.2.0',
    [switch]$Pack,
    [string]$UpxPath = 'upx'
)

$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^[A-Za-z0-9._+-]+$') {
    throw 'Version may contain only letters, numbers, dots, underscores, plus signs and hyphens.'
}
$go = (Get-Command go -CommandType Application -ErrorAction Stop).Source
if ($Pack) {
    $upx = (Get-Command $UpxPath -CommandType Application -ErrorAction Stop).Source
}

$savedEnvironment = @{}
foreach ($name in @('CGO_ENABLED', 'GOOS', 'GOARCH', 'GOAMD64', 'GO386')) {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
Push-Location $PSScriptRoot
try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOAMD64 = 'v1'
    $env:GO386 = 'sse2'
    $unpackedDirectory = Join-Path $PSScriptRoot 'bin\unpacked'
    New-Item -ItemType Directory -Path $unpackedDirectory -Force | Out-Null
    if ($Pack) {
        $packedDirectory = Join-Path $PSScriptRoot 'bin\packed'
        New-Item -ItemType Directory -Path $packedDirectory -Force | Out-Null
    }
    foreach ($target in @(
        @{ Name = 'x86'; GoArch = '386' },
        @{ Name = 'x64'; GoArch = 'amd64' }
    )) {
        $env:GOARCH = $target.GoArch
        $filename = 'websrc-{0}.exe' -f $target.Name
        $unpacked = Join-Path $unpackedDirectory $filename
        & $go build -trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.version=$Version" -o $unpacked .
        if ($LASTEXITCODE -ne 0) { throw "Go build failed for $($target.Name)." }
        Write-Output ('Built {0} ({1:N0} bytes)' -f $unpacked, (Get-Item -LiteralPath $unpacked).Length)
        if ($Pack) {
            $packed = Join-Path $packedDirectory $filename
            Copy-Item -LiteralPath $unpacked -Destination $packed -Force
            & $upx --best $packed
            if ($LASTEXITCODE -ne 0) { throw "UPX compression failed for $($target.Name)." }
            & $upx -t $packed
            if ($LASTEXITCODE -ne 0) { throw "UPX integrity check failed for $($target.Name)." }
            Write-Output ('Packed {0} ({1:N0} bytes)' -f $packed, (Get-Item -LiteralPath $packed).Length)
        }
    }
}
finally {
    foreach ($name in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process')
    }
    Pop-Location
}
