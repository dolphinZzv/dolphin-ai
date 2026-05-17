$ProjectDir = $PSScriptRoot
$BaseUrl = "http://localhost:9223"

Write-Host "=== WebHost Interactive Demo ===" -ForegroundColor Cyan

# Build
dotnet build -c Release --nologo -v q 2>&1 | Out-Null

# Start WebHost - use dotnet run for proper WPF hosting
Write-Host "Starting WebHost..." -ForegroundColor Yellow
$process = Start-Process -FilePath "dotnet" -ArgumentList "run","--project",$ProjectDir,"-c","Release","--no-build" -WindowStyle Normal -PassThru
Start-Sleep -Seconds 5

# Wait for server
for ($i = 0; $i -lt 15; $i++) {
    try { $r = Invoke-WebRequest -Uri "$BaseUrl/health" -UseBasicParsing -TimeoutSec 2; if ($r.StatusCode -eq 200) { break } } catch {}
    Start-Sleep -Seconds 1
}

Write-Host "WebHost ready: $BaseUrl" -ForegroundColor Green

# Create session
Write-Host "Creating browser session..." -ForegroundColor Cyan
$body = '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"web_session_create","arguments":{"viewport":{"width":1280,"height":720}}}}'
$resp = Invoke-WebRequest -Uri "$BaseUrl/mcp/call" -Method Post -Body $body -ContentType "application/json" -UseBasicParsing
$obj = $resp.Content | ConvertFrom-Json
$sessionId = $obj.result.sessionId
Write-Host "Session: $sessionId" -ForegroundColor Green

# Navigate to Baidu
Write-Host "Navigating to Baidu..." -ForegroundColor Cyan
$nbody = '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"page_open","arguments":{"sessionId":"' + $sessionId + '","url":"https://www.baidu.com"}}}'
Invoke-WebRequest -Uri "$BaseUrl/mcp/call" -Method Post -Body $nbody -ContentType "application/json" -UseBasicParsing | Out-Null
Write-Host "Baidu loading... (wait 3s)" -ForegroundColor Green
Start-Sleep -Seconds 3

# Switch to interactive mode
Write-Host "Switching to INTERACTIVE mode - window is now visible!" -ForegroundColor Magenta
$ibody = '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"web_set_interactive","arguments":{"sessionId":"' + $sessionId + '","interactive":true}}}'
Invoke-WebRequest -Uri "$BaseUrl/mcp/call" -Method Post -Body $ibody -ContentType "application/json" -UseBasicParsing | Out-Null

Write-Host "Press ENTER to close session and exit..." -ForegroundColor Yellow
$null = Read-Host

# Close session
Write-Host "Closing session..." -ForegroundColor Cyan
$cbody = '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"web_session_close","arguments":{"sessionId":"' + $sessionId + '"}}}'
Invoke-WebRequest -Uri "$BaseUrl/mcp/call" -Method Post -Body $cbody -ContentType "application/json" -UseBasicParsing | Out-Null

Write-Host "Stopping WebHost..." -ForegroundColor Yellow
if ($process -and !$process.HasExited) { $process.Kill() }

Write-Host "Done" -ForegroundColor Cyan
