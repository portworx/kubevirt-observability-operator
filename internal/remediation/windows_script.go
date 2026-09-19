package remediation

func WindowsExporterInstallScript() string {
	return `$ErrorActionPreference = "Stop"

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$workDir = "C:\ProgramData\KubeVirtObservability"
$installDir = "C:\Program Files\windows_exporter"
$exe = "$installDir\windows_exporter.exe"

New-Item -ItemType Directory -Path $workDir -Force | Out-Null

$version = "0.31.5"
$msi = "windows_exporter-$version-amd64.msi"
$url = "https://github.com/prometheus-community/windows_exporter/releases/download/v$version/$msi"
$out = "$workDir\$msi"

$service = Get-Service windows_exporter -ErrorAction SilentlyContinue

if (-not $service -or -not (Test-Path $exe)) {
    curl.exe -L -f -o $out $url

    Start-Process -FilePath "msiexec.exe" -ArgumentList @(
        "/i",
        $out,
        "ENABLED_COLLECTORS=[defaults],mssql,textfile",
        "LISTEN_PORT=9182",
        "/qn",
        "/norestart"
    ) -Wait -NoNewWindow
}

if (-not (Test-Path $exe)) {
    throw "windows_exporter executable missing: $exe"
}

if (-not (Get-Service windows_exporter -ErrorAction SilentlyContinue)) {
    throw "windows_exporter service was not created"
}

Stop-Service windows_exporter -Force -ErrorAction SilentlyContinue

# Repair stale services created by older KVO versions.
# Older installations may contain removed collectors such as cs.
$imagePath = '"' + $exe + '" --collectors.enabled="[defaults],mssql,textfile"'

Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\windows_exporter" -Name "ImagePath" -Value $imagePath

Set-Service windows_exporter -StartupType Automatic

Start-Sleep -Seconds 2

Start-Service windows_exporter

$running = $false

for ($i = 0; $i -lt 10; $i++) {
    Start-Sleep -Seconds 2

    $service = Get-Service windows_exporter -ErrorAction SilentlyContinue

    if ($service -and $service.Status -eq "Running") {
        $running = $true
        break
    }
}

if (-not $running) {
    $imagePathCurrent = (Get-ItemProperty "HKLM:\SYSTEM\CurrentControlSet\Services\windows_exporter").ImagePath
    Write-Output "windows_exporter ImagePath: $imagePathCurrent"

    Get-WinEvent -FilterHashtable @{LogName="System"; ProviderName="Service Control Manager"; StartTime=(Get-Date).AddMinutes(-5)} -ErrorAction SilentlyContinue | Select-Object -First 10 TimeCreated,Id,Message | Format-List

    throw "windows_exporter service failed to stay running"
}

$existingRule = Get-NetFirewallRule -DisplayName "Allow windows_exporter 9182" -ErrorAction SilentlyContinue

if (-not $existingRule) {
    New-NetFirewallRule -DisplayName "Allow windows_exporter 9182" -Direction Inbound -Protocol TCP -LocalPort 9182 -Action Allow | Out-Null
}

curl.exe -f http://127.0.0.1:9182/metrics | Out-Null

Write-Output "WINDOWS_EXPORTER_READY"
`
}
