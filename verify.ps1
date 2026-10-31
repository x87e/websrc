param([string]$Version = '0.2.0')

$ErrorActionPreference = 'Stop'

function Invoke-WebsrcProcess([string]$Binary, [string]$CommandArguments, [string]$InputText = '') {
    $start = New-Object System.Diagnostics.ProcessStartInfo
    $start.FileName = $Binary
    $start.Arguments = $CommandArguments
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardInput = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.StandardOutputEncoding = [System.Text.Encoding]::UTF8
    $start.StandardErrorEncoding = [System.Text.Encoding]::UTF8
    $process = [System.Diagnostics.Process]::Start($start)
    try {
        $outputTask = $process.StandardOutput.ReadToEndAsync()
        $errorTask = $process.StandardError.ReadToEndAsync()
        $process.StandardInput.Write($InputText)
        $process.StandardInput.Close()
        if (-not $process.WaitForExit(30000)) {
            $process.Kill()
            throw "Process timed out: $Binary $CommandArguments"
        }
        return [PSCustomObject]@{
            ExitCode = $process.ExitCode
            Output = $outputTask.GetAwaiter().GetResult()
            Diagnostic = $errorTask.GetAwaiter().GetResult()
        }
    }
    finally { $process.Dispose() }
}

# Temporary fixtures keep verification independent of external files and services.
$temporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ('websrc-verify-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
$htmlPath = Join-Path $temporaryDirectory 'sample.html'
$pdfPath = Join-Path $temporaryDirectory 'sample.pdf'
try {
    $html = '<!doctype html><html><head><style>.title { font-size:32px } p { font-size:16px }</style></head><body><div class="title">Example title</div><p>This sufficiently long body paragraph sets the common font size and contains <strong>bold</strong> text.</p></body></html>'
    [System.IO.File]::WriteAllText($htmlPath, $html, (New-Object System.Text.UTF8Encoding($false)))

    # A small text PDF with valid object offsets, generated entirely in memory.
    $content = "BT /F1 24 Tf 72 720 Td (PDF extraction example) Tj ET`nBT /F1 12 Tf 72 680 Td (This longer body paragraph verifies readable PDF text extraction from each executable.) Tj ET`n"
    $objects = @(
        '<< /Type /Catalog /Pages 2 0 R >>',
        '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
        '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
        '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
        ("<< /Length $($content.Length) >>`nstream`n" + $content + 'endstream')
    )
    $pdf = New-Object System.Text.StringBuilder
    [void]$pdf.Append("%PDF-1.4`n")
    $offsets = @()
    for ($index = 0; $index -lt $objects.Count; $index++) {
        $offsets += $pdf.Length
        [void]$pdf.Append("$($index + 1) 0 obj`n$($objects[$index])`nendobj`n")
    }
    $xrefOffset = $pdf.Length
    [void]$pdf.Append("xref`n0 6`n0000000000 65535 f `n")
    foreach ($offset in $offsets) { [void]$pdf.Append(('{0:D10} 00000 n ' -f $offset) + "`n") }
    [void]$pdf.Append("trailer`n<< /Size 6 /Root 1 0 R >>`nstartxref`n$xrefOffset`n%%EOF`n")
    [System.IO.File]::WriteAllBytes($pdfPath, [System.Text.Encoding]::ASCII.GetBytes($pdf.ToString()))

    $banner = [System.IO.File]::ReadAllText((Join-Path $PSScriptRoot 'internal\cli\banner.txt')).TrimEnd([char[]]"`r`n")
    foreach ($architecture in @('x86', 'x64')) {
        $filename = "websrc-$architecture.exe"
        $unpacked = Join-Path $PSScriptRoot "bin\unpacked\$filename"
        if (-not (Test-Path -LiteralPath $unpacked -PathType Leaf)) { throw "Missing build: $unpacked" }
        $binaries = @($unpacked)
        $packed = Join-Path $PSScriptRoot "bin\packed\$filename"
        if (Test-Path -LiteralPath $packed -PathType Leaf) { $binaries += $packed }

        foreach ($binary in $binaries) {
            $bytes = [System.IO.File]::ReadAllBytes($binary)
            $peOffset = [BitConverter]::ToInt32($bytes, 0x3c)
            $machine = [BitConverter]::ToUInt16($bytes, $peOffset + 4)
            $expectedMachine = if ($architecture -eq 'x86') { 0x014c } else { 0x8664 }
            if ($machine -ne $expectedMachine) { throw "Wrong architecture: $binary" }
            $result = Invoke-WebsrcProcess $binary '--version'
            if ($result.ExitCode -ne 0 -or $result.Output.TrimEnd() -cne "websrc $Version" -or $result.Diagnostic.Length -ne 0) {
                throw "Version/stream check failed: $binary"
            }
            $result = Invoke-WebsrcProcess $binary '' "exit`n"
            if ($result.ExitCode -ne 0 -or $result.Output.Length -ne 0 -or -not $result.Diagnostic.StartsWith($banner)) {
                throw "Interactive banner check failed: $binary"
            }
            if (-not $result.Diagnostic.Contains("websrc $Version - HTML and PDF downloader and converter")) {
                throw "Interactive version check failed: $binary"
            }
            foreach ($inputPath in @($htmlPath, $pdfPath)) {
                foreach ($format in @('raw', 'md', 'txt', 'txtx', 'xml', 'mdx')) {
                    $commandArguments = '--input "' + $inputPath + '" --stdout -' + $format
                    $result = Invoke-WebsrcProcess $binary $commandArguments
                    if ($result.ExitCode -ne 0 -or $result.Output.Length -eq 0) {
                        throw "$binary $format failed: $($result.Diagnostic)"
                    }
                    if ($format -eq 'raw') {
                        $expectedRaw = [System.IO.File]::ReadAllText($inputPath)
                        if ($result.Output -cne $expectedRaw) { throw "Raw source changed: $binary" }
                    }
                    else {
                        $expectedTitle = if ($inputPath -eq $pdfPath) { 'PDF extraction example' } else { 'Example title' }
                        if (-not $result.Output.Contains($expectedTitle)) { throw "Missing converted title: $binary $format" }
                        if ($format -eq 'xml' -or $format -eq 'txtx') {
                            $xmlResult = [xml]$result.Output
                            if ($xmlResult.DocumentElement.Name -ne 'document') { throw "Wrong XML root: $binary" }
                        }
                        if ($format -eq 'mdx' -or ($inputPath -eq $pdfPath -and $format -eq 'md')) {
                            if (-not $result.Output.StartsWith("# $expectedTitle")) { throw "Heading inference failed: $binary" }
                        }
                    }
                    if ($binary -ne $unpacked) {
                        $reference = Invoke-WebsrcProcess $unpacked $commandArguments
                        if ($reference.ExitCode -ne $result.ExitCode -or $reference.Output -cne $result.Output -or $reference.Diagnostic -cne $result.Diagnostic) {
                            throw "Packed/unpacked output differs: $binary $format"
                        }
                    }
                }
            }
            $result = Invoke-WebsrcProcess $binary '--unknown-option'
            if ($result.ExitCode -ne 2 -or $result.Output.Length -ne 0 -or $result.Diagnostic.Length -eq 0) {
                throw "Invalid argument handling failed: $binary"
            }
            Write-Output "Verified architecture, title, version, HTML/PDF formats and error handling: $binary"
        }
    }
}
finally {
    Remove-Item -LiteralPath $htmlPath,$pdfPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $temporaryDirectory -Force
}
