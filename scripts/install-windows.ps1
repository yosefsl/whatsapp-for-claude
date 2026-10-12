# Installs whatsapp-claude on Windows for the current user. No admin rights needed.
# Run: powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
$ErrorActionPreference = "Stop"

$Pkg     = Split-Path -Parent $PSScriptRoot
$HomeDir = if ($env:WA_CLAUDE_HOME) { $env:WA_CLAUDE_HOME } else { Join-Path $env:USERPROFILE ".whatsapp-claude" }
$Port    = if ($env:WA_CLAUDE_PORT) { $env:WA_CLAUDE_PORT } else { "47321" }
$Bin     = Join-Path $HomeDir "bin\whatsapp-claude.exe"
New-Item -ItemType Directory -Force -Path (Join-Path $HomeDir "bin"), (Join-Path $HomeDir "logs"), (Join-Path $HomeDir "toolchain") | Out-Null

Write-Host "==> 1/5 Go toolchain"
$Go = Join-Path $HomeDir "toolchain\go\bin\go.exe"
if (-not (Test-Path $Go)) {
  $Ver = (Invoke-WebRequest -UseBasicParsing "https://go.dev/VERSION?m=text").Content.Split("`n")[0].Trim()
  $Zip = Join-Path $HomeDir "toolchain\go.zip"
  Invoke-WebRequest -UseBasicParsing "https://go.dev/dl/$Ver.windows-amd64.zip" -OutFile $Zip
  Expand-Archive -Force $Zip (Join-Path $HomeDir "toolchain")
  Remove-Item $Zip
}
& $Go version

Write-Host "==> 2/5 Build"
$env:GOPATH = Join-Path $HomeDir "toolchain\gopath"; $env:GOCACHE = Join-Path $HomeDir "toolchain\cache"
$env:CGO_ENABLED = "0"; $env:GOFLAGS = "-mod=mod -modcacherw"
Get-Process whatsapp-claude -ErrorAction SilentlyContinue | Stop-Process -Force
Push-Location (Join-Path $Pkg "engine"); & $Go build -o $Bin .; Pop-Location
& $Bin version

Write-Host "==> 3/5 Background service (Startup folder, hidden window)"
$Vbs = Join-Path ([Environment]::GetFolderPath("Startup")) "whatsapp-claude.vbs"
@"
Set sh = CreateObject("WScript.Shell")
sh.Environment("PROCESS")("WA_CLAUDE_HOME") = "$HomeDir"
sh.Environment("PROCESS")("WA_CLAUDE_PORT") = "$Port"
Do
  sh.Run "cmd /c """"$Bin"" serve >> ""$HomeDir\logs\service.log"" 2>&1""", 0, True
  WScript.Sleep 10000
Loop
"@ | Set-Content -Encoding ASCII $Vbs
Start-Process wscript.exe -ArgumentList "`"$Vbs`""
$ok = $false
for ($i = 0; $i -lt 20; $i++) { try { Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$Port/status" | Out-Null; $ok = $true; break } catch { Start-Sleep 1 } }
if (-not $ok) { Get-Content (Join-Path $HomeDir "logs\service.log") -Tail 20; throw "service did not start" }

Write-Host "==> 4/5 Claude Code"
$Claude = Get-Command claude -ErrorAction SilentlyContinue
if ($Claude) {
  & claude mcp remove --scope user whatsapp 2>$null
  & claude mcp add --scope user whatsapp -- $Bin mcp
  & $Bin register
} else {
  & $Bin register --mcp
}
$Skill = Join-Path $env:USERPROFILE ".claude\skills\whatsapp"
New-Item -ItemType Directory -Force -Path $Skill | Out-Null
Copy-Item (Join-Path $Pkg "skill\SKILL.md") (Join-Path $Skill "SKILL.md") -Force

Write-Host "==> 5/5 Status"
& $Bin status
Write-Host "INSTALL_OK  link page: http://127.0.0.1:$Port/link"
