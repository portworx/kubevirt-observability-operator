package remediation

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func WindowsAlloyInstallScript(
	configAlloy string,
	lokiToken string,
) string {

	configB64 := base64.StdEncoding.EncodeToString(
		[]byte(configAlloy),
	)

	tokenB64 := base64.StdEncoding.EncodeToString(
		[]byte(strings.TrimSpace(lokiToken)),
	)

	return fmt.Sprintf(`$ErrorActionPreference = "Stop"

$ConfigB64 = "%s"
$TokenB64  = "%s"

$ConfigPath = "C:\Program Files\GrafanaLabs\Alloy\config.alloy"

$SecretDir = "C:\ProgramData\GrafanaLabs\Alloy\secrets"
$TokenPath = Join-Path $SecretDir "loki.token"

$DataDir = "C:\ProgramData\GrafanaLabs\Alloy\data"

$Installer = "C:\alloy-installer.exe"

Set-TimeZone -Id "UTC"

Set-Service w32time -StartupType Automatic

Start-Service w32time

try {
  w32tm /resync
} catch {
  Write-Output "time sync retry skipped"
}

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

New-NetFirewallRule -DisplayName "Allow HTTPS Outbound" -Direction Outbound -Protocol TCP -RemotePort 443 -Action Allow -ErrorAction SilentlyContinue | Out-Null

New-NetFirewallRule -DisplayName "Allow DNS Outbound" -Direction Outbound -Protocol UDP -RemotePort 53 -Action Allow -ErrorAction SilentlyContinue | Out-Null

if (-not (Get-Service Alloy -ErrorAction SilentlyContinue)) {

  curl.exe -L -o $Installer https://github.com/grafana/alloy/releases/latest/download/alloy-installer-windows-amd64.exe

  Start-Process -FilePath $Installer -ArgumentList "/S" -Wait -NoNewWindow
}

New-Item -ItemType Directory -Force -Path (Split-Path $ConfigPath) | Out-Null
New-Item -ItemType Directory -Force -Path $SecretDir | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

Remove-Item "C:\ProgramData\GrafanaLabs\Alloy\data\*" -Recurse -Force -ErrorAction SilentlyContinue | Out-Null

New-Item -ItemType Directory -Force  -Path "$DataDir\loki.source.windowsevent.application" | Out-Null

New-Item -ItemType Directory -Force -Path "$DataDir\loki.source.windowsevent.system" | Out-Null

icacls "$DataDir" /grant "SYSTEM:(OI)(CI)F" /T | Out-Null

$Config = [System.Text.Encoding]::UTF8.GetString(
  [System.Convert]::FromBase64String($ConfigB64)
)

[System.IO.File]::WriteAllText(
  $ConfigPath,
  $Config,
  (New-Object System.Text.UTF8Encoding($false))
)

$Token = [System.Text.Encoding]::UTF8.GetString(
  [System.Convert]::FromBase64String($TokenB64)
)

[System.IO.File]::WriteAllText(
  $TokenPath,
  $Token,
  (New-Object System.Text.UTF8Encoding($false))
)


$alloyArgs = @(
  'run'
  'C:\Program Files\GrafanaLabs\Alloy\config.alloy'
  '--storage.path=C:\ProgramData\GrafanaLabs\Alloy\data'
)

Set-ItemProperty -Path 'HKLM:\SOFTWARE\GrafanaLabs\Alloy' -Name 'Arguments' -Value $alloyArgs

sc.exe config Alloy binPath= "\"C:\Program Files\GrafanaLabs\Alloy\alloy-service-windows-amd64.exe\""


Set-Service -Name Alloy -StartupType Automatic

Stop-Service Alloy -ErrorAction SilentlyContinue

Get-Process | Where-Object { $_.ProcessName -like "*alloy*" } | Stop-Process -Force -ErrorAction SilentlyContinue

Start-Service Alloy

for ($i = 0; $i -lt 20; $i++) {

  $svc = Get-Service Alloy -ErrorAction SilentlyContinue

  if ($svc -and $svc.Status -eq "Running") {

    Write-Output "ALLOY_WINDOWS_CONFIGURED"

    exit 0
  }

  Start-Sleep -Seconds 1
}

Get-Service Alloy

exit 1
`, configB64, tokenB64)
}

func WindowsAlloyUpdateScript(
	configAlloy string,
	lokiToken string,
) string {

	configB64 := base64.StdEncoding.EncodeToString(
		[]byte(configAlloy),
	)

	tokenB64 := base64.StdEncoding.EncodeToString(
		[]byte(strings.TrimSpace(lokiToken)),
	)

	return fmt.Sprintf(`$ErrorActionPreference = "Stop"

$ConfigB64 = "%s"
$TokenB64  = "%s"
$DataDir = "C:\ProgramData\GrafanaLabs\Alloy\data"

$ConfigPath = "C:\Program Files\GrafanaLabs\Alloy\config.alloy"

$SecretDir = "C:\ProgramData\GrafanaLabs\Alloy\secrets"
$TokenPath = Join-Path $SecretDir "loki.token"

New-Item -ItemType Directory -Force -Path $SecretDir | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

Set-TimeZone -Id "UTC"

Set-Service w32time -StartupType Automatic

Start-Service w32time

try {
  w32tm /resync
} catch {
  Write-Output "time sync retry skipped"
}

$Config = [System.Text.Encoding]::UTF8.GetString(
  [System.Convert]::FromBase64String($ConfigB64)
)

[System.IO.File]::WriteAllText(
  $ConfigPath,
  $Config,
  (New-Object System.Text.UTF8Encoding($false))
)

$Token = [System.Text.Encoding]::UTF8.GetString(
  [System.Convert]::FromBase64String($TokenB64)
)

[System.IO.File]::WriteAllText(
  $TokenPath,
  $Token,
  (New-Object System.Text.UTF8Encoding($false))
)

$alloyArgs = @(
  'run'
  'C:\Program Files\GrafanaLabs\Alloy\config.alloy'
  '--storage.path=C:\ProgramData\GrafanaLabs\Alloy\data'
)

Set-ItemProperty -Path 'HKLM:\SOFTWARE\GrafanaLabs\Alloy' -Name 'Arguments' -Value $alloyArgs

sc.exe config Alloy binPath= "\"C:\Program Files\GrafanaLabs\Alloy\alloy-service-windows-amd64.exe\""

New-Item -ItemType Directory -Force -Path "$DataDir\loki.source.windowsevent.application" | Out-Null

New-Item -ItemType Directory -Force -Path "$DataDir\loki.source.windowsevent.system" | Out-Null

icacls "$DataDir" /grant "SYSTEM:(OI)(CI)F" /T | Out-Null

Restart-Service Alloy

Start-Sleep -Seconds 5

$svc = Get-Service Alloy -ErrorAction SilentlyContinue

if (-not $svc) {
    throw "Alloy service missing"
}

if ($svc.Status -ne 'Running') {
    throw "Alloy service not running"
}

Write-Output "ALLOY_WINDOWS_UPDATED"

exit 0
`, configB64, tokenB64)
}
