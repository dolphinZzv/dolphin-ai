$WebHostDll = "bin\Release\net5.0-windows\WebHost.dll"
$OutputFile = "$env:USERPROFILE\Desktop\webhost_screenshot.png"
$BaseUrl = "http://localhost:9223"

Write-Host "=== WebHost Integration Test ===" -ForegroundColor Cyan

# Build
dotnet build -c Release --nologo -v q 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Host "Build failed" -ForegroundColor Red; exit 1 }

# Start WebHost
Write-Host "Starting WebHost..." -ForegroundColor Yellow
$process = Start-Process -FilePath "dotnet" -ArgumentList "exec", $WebHostDll -WindowStyle Minimized -PassThru
Start-Sleep -Seconds 3

try {
    # Wait for server
    $ready = $false
    for ($i = 0; $i -lt 10; $i++) {
        try {
            $r = Invoke-WebRequest -Uri "$BaseUrl/health" -UseBasicParsing -TimeoutSec 2
            if ($r.StatusCode -eq 200) { $ready = $true; break }
        } catch {}
        Start-Sleep -Seconds 1
    }

    if (-not $ready) { throw "WebHost did not start within 15s" }
    Write-Host "WebHost is ready at $BaseUrl" -ForegroundColor Green

    function Call-JsonRpc($body) {
        $resp = Invoke-WebRequest -Uri "$BaseUrl/mcp/call" -Method Post -Body $body -ContentType "application/json" -UseBasicParsing
        $raw = $resp.Content
        $obj = $raw | ConvertFrom-Json
        Write-Host "   Response: $raw" -ForegroundColor DarkGray
        return $obj
    }

    # 1. Create session
    Write-Host "`n1. Creating session..." -ForegroundColor Cyan
    $body = '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"web_session_create","arguments":{"viewport":{"width":1280,"height":720}}}}'
    $resp = Call-JsonRpc $body
    $sessionId = $resp.result.sessionId
    if (-not $sessionId) { throw "No sessionId in response" }
    Write-Host "   Session created: $sessionId" -ForegroundColor Green

    # 2. Navigate to Baidu
    Write-Host "`n2. Navigating to Baidu..." -ForegroundColor Cyan
    $nbody = '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"page_open","arguments":{"sessionId":"' + $sessionId + '","url":"https://www.baidu.com"}}}'
    $resp = Call-JsonRpc $nbody
    Write-Host "   Navigation result: $($resp.result.success)" -ForegroundColor Green
    Start-Sleep -Seconds 3

    # 3. Take screenshot
    Write-Host "`n3. Taking screenshot..." -ForegroundColor Cyan
    $sbody = '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"page_screenshot","arguments":{"sessionId":"' + $sessionId + '"}}}'
    $resp = Call-JsonRpc $sbody
    $base64 = $resp.result.data

    if ($base64) {
        $bytes = [Convert]::FromBase64String($base64)
        [IO.File]::WriteAllBytes($OutputFile, $bytes)
        Write-Host "   Screenshot saved to: $OutputFile ($($bytes.Length) bytes)" -ForegroundColor Green
    } else {
        Write-Host "   Screenshot failed or empty" -ForegroundColor Red
    }

    # 4. List sessions
    Write-Host "`n4. Listing sessions..." -ForegroundColor Cyan
    $resp = Invoke-RestMethod -Uri "$BaseUrl/mcp/sessions" -UseBasicParsing
    Write-Host "   Active sessions: $($resp | ConvertTo-Json -Compress)" -ForegroundColor Green

    # 5. Execute JS
    Write-Host "`n5. Executing JavaScript..." -ForegroundColor Cyan
    $ebody = '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"script_run","arguments":{"sessionId":"' + $sessionId + '","script":"document.title"}}}'
    $resp = Call-JsonRpc $ebody
    Write-Host "   Page title: $($resp.result.value)" -ForegroundColor Green

    # 6. Close session
    Write-Host "`n6. Closing session..." -ForegroundColor Cyan
    $cbody = '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"web_session_close","arguments":{"sessionId":"' + $sessionId + '"}}}'
    $resp = Call-JsonRpc $cbody
    Write-Host "   Session closed: $($resp.result.success)" -ForegroundColor Green

    Write-Host "`n=== All tests passed! ===" -ForegroundColor Cyan
}
catch {
    Write-Host "`nERROR: $_" -ForegroundColor Red
}
finally {
    Write-Host "`nStopping WebHost..." -ForegroundColor Yellow
    if ($process -and !$process.HasExited) { $process.Kill() }
}
