package remediation

func WindowsExporterInstallScript() string {
	return `$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

New-Item -ItemType Directory -Path C:\ProgramData\KubeVirtObservability -Force | Out-Null

$version = "0.31.5"
$msi = "windows_exporter-$version-amd64.msi"
$url = "https://github.com/prometheus-community/windows_exporter/releases/download/v$version/$msi"
$out = "C:\ProgramData\KubeVirtObservability\$msi"

curl.exe -L -f -o $out $url

Start-Process -FilePath "msiexec.exe" -ArgumentList @(
  "/i",
  $out,
  "ENABLED_COLLECTORS=[defaults],cpu,cs,logical_disk,memory,net,os,service,system,textfile,mssql",
  "LISTEN_PORT=9182",
  "/qn",
  "/norestart"
) -Wait -NoNewWindow

if (-not (Get-Service windows_exporter -ErrorAction SilentlyContinue)) {
  throw "windows_exporter service was not created"
}

Set-Service windows_exporter -StartupType Automatic
Start-Service windows_exporter

New-NetFirewallRule -DisplayName "Allow windows_exporter 9182" -Direction Inbound -Protocol TCP -LocalPort 9182 -Action Allow -ErrorAction SilentlyContinue | Out-Null

curl.exe -f http://127.0.0.1:9182/metrics | Out-Null
`
}
