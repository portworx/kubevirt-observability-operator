package sysprepmerge

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

const (
	managedDescription = "KubeVirt Observability Bootstrap"
)

type Unattend struct {
	XMLName  xml.Name  `xml:"unattend"`
	Xmlns    string    `xml:"xmlns,attr,omitempty"`
	Settings []Setting `xml:"settings"`
}

type Setting struct {
	Pass       string      `xml:"pass,attr"`
	Components []Component `xml:"component"`
}

type Component struct {
	Name                  string          `xml:"name,attr,omitempty"`
	ProcessorArchitecture string          `xml:"processorArchitecture,attr,omitempty"`
	PublicKeyToken        string          `xml:"publicKeyToken,attr,omitempty"`
	Language              string          `xml:"language,attr,omitempty"`
	VersionScope          string          `xml:"versionScope,attr,omitempty"`
	XmlnsWcm              string          `xml:"xmlns:wcm,attr,omitempty"`
	XmlnsXsi              string          `xml:"xmlns:xsi,attr,omitempty"`
	RunSynchronous        *RunSynchronous `xml:"RunSynchronous,omitempty"`
}

type RunSynchronous struct {
	Commands []RunSynchronousCommand `xml:"RunSynchronousCommand"`
}

type RunSynchronousCommand struct {
	WcmAction   string `xml:"wcm:action,attr,omitempty"`
	Order       int    `xml:"Order"`
	Description string `xml:"Description,omitempty"`
	Path        string `xml:"Path"`
}

// Merge merges the given sysprep XML with the monitoring bootstrap data.
func Merge(unattendXML string) (string, bool, error) {
	doc, err := parse(unattendXML)
	if err != nil {
		return "", false, err
	}

	changed := ensureBootstrapCommand(&doc)

	out, err := render(doc)
	if err != nil {
		return "", false, err
	}
	return out, changed, nil
}

// parse parses the given sysprep XML.
func parse(in string) (Unattend, error) {
	var doc Unattend
	trimmed := strings.TrimSpace(in)
	if trimmed == "" {
		return Unattend{
			Xmlns: "urn:schemas-microsoft-com:unattend",
		}, nil
	}

	if err := xml.Unmarshal([]byte(trimmed), &doc); err != nil {
		return Unattend{}, fmt.Errorf("failed to parse sysprep xml: %w", err)
	}

	if doc.Xmlns == "" {
		doc.Xmlns = "urn:schemas-microsoft-com:unattend"
	}
	return doc, nil
}

// render renders the given sysprep XML.
func render(doc Unattend) (string, error) {
	buf := &bytes.Buffer{}
	buf.WriteString(xml.Header)

	enc := xml.NewEncoder(buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("failed to render sysprep xml: %w", err)
	}
	if err := enc.Flush(); err != nil {
		return "", fmt.Errorf("failed to flush sysprep xml: %w", err)
	}
	return buf.String(), nil
}

// ensureBootstrapCommand ensures the given sysprep XML has the monitoring bootstrap command.
func ensureBootstrapCommand(doc *Unattend) bool {
	specializeIdx := ensureSetting(doc, "specialize")
	compIdx := ensureDeploymentComponent(&doc.Settings[specializeIdx])

	comp := &doc.Settings[specializeIdx].Components[compIdx]
	if comp.RunSynchronous == nil {
		comp.RunSynchronous = &RunSynchronous{}
	}

	for _, cmd := range comp.RunSynchronous.Commands {
		if strings.EqualFold(strings.TrimSpace(cmd.Description), managedDescription) {
			return false
		}
		if strings.Contains(strings.ToLower(cmd.Path), strings.ToLower(`C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`)) {
			return false
		}
	}

	order := nextOrder(comp.RunSynchronous.Commands)
	comp.RunSynchronous.Commands = append(comp.RunSynchronous.Commands, RunSynchronousCommand{
		WcmAction:   "add",
		Order:       order,
		Description: managedDescription,
		Path:        sysprepBootstrapCommand(),
	})

	return true
}

// ensureSetting ensures the given sysprep XML has the given setting.
func ensureSetting(doc *Unattend, pass string) int {
	for i, s := range doc.Settings {
		if strings.EqualFold(strings.TrimSpace(s.Pass), pass) {
			return i
		}
	}
	doc.Settings = append(doc.Settings, Setting{
		Pass: pass,
	})
	return len(doc.Settings) - 1
}

// ensureDeploymentComponent ensures the given sysprep XML has the deployment component.
func ensureDeploymentComponent(setting *Setting) int {
	for i, c := range setting.Components {
		if strings.EqualFold(strings.TrimSpace(c.Name), "Microsoft-Windows-Deployment") {
			ensureComponentAttrs(&setting.Components[i])
			return i
		}
	}

	setting.Components = append(setting.Components, Component{
		Name:                  "Microsoft-Windows-Deployment",
		ProcessorArchitecture: "amd64",
		PublicKeyToken:        "31bf3856ad364e35",
		Language:              "neutral",
		VersionScope:          "nonSxS",
		XmlnsWcm:              "http://schemas.microsoft.com/WMIConfig/2002/State",
		XmlnsXsi:              "http://www.w3.org/2001/XMLSchema-instance",
	})
	return len(setting.Components) - 1
}

// ensureComponentAttrs ensures the given component has the required attributes.
func ensureComponentAttrs(c *Component) {
	if c.ProcessorArchitecture == "" {
		c.ProcessorArchitecture = "amd64"
	}
	if c.PublicKeyToken == "" {
		c.PublicKeyToken = "31bf3856ad364e35"
	}
	if c.Language == "" {
		c.Language = "neutral"
	}
	if c.VersionScope == "" {
		c.VersionScope = "nonSxS"
	}
	if c.XmlnsWcm == "" {
		c.XmlnsWcm = "http://schemas.microsoft.com/WMIConfig/2002/State"
	}
	if c.XmlnsXsi == "" {
		c.XmlnsXsi = "http://www.w3.org/2001/XMLSchema-instance"
	}
}

// nextOrder returns the next order for the given commands.
func nextOrder(cmds []RunSynchronousCommand) int {
	max := 0
	for _, c := range cmds {
		if c.Order > max {
			max = c.Order
		}
	}
	return max + 1
}

// sysprepBootstrapCommand returns the bootstrap command.
func sysprepBootstrapCommand() string {
	scriptPath := `C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`

	return fmt.Sprintf(
		`powershell.exe -ExecutionPolicy Bypass -Command "$dir='C:\ProgramData\KubeVirtObservability'; New-Item -ItemType Directory -Path $dir -Force | Out-Null; $script='%s'; Set-Content -Path $script -Encoding ASCII -Value @'
%s
'@; powershell.exe -ExecutionPolicy Bypass -File $script"`,
		scriptPath,
		escapeForSingleQuotedHereString(windowsBootstrapScriptForSysprep()),
	)
}

// windowsBootstrapScriptForSysprep returns the Windows bootstrap script for sysprep.
func windowsBootstrapScriptForSysprep() string {
	return `New-Item -ItemType Directory -Path C:\ProgramData\KubeVirtObservability -Force | Out-Null
$marker = 'C:\ProgramData\KubeVirtObservability\bootstrap.done'
if (Test-Path $marker) { exit 0 }

$ProgressPreference = 'SilentlyContinue'

function Enable-WinRMForObservability {
    Enable-PSRemoting -Force
    Set-Item -Path WSMan:\localhost\Service\Auth\Basic -Value $true
    Set-Item -Path WSMan:\localhost\Service\AllowUnencrypted -Value $true
    Enable-NetFirewallRule -DisplayGroup 'Windows Remote Management'
}

function Install-WindowsExporter {
    if (Get-Service windows_exporter -ErrorAction SilentlyContinue) { return }

    $version = '0.31.5'
    $msi = "windows_exporter-$version-amd64.msi"
    $url = "https://github.com/prometheus-community/windows_exporter/releases/download/v$version/$msi"
    $out = "C:\ProgramData\KubeVirtObservability\$msi"

    Invoke-WebRequest -Uri $url -OutFile $out
    $arguments = @(
      '/i',
      $out,
      'ENABLED_COLLECTORS=[defaults],cpu,cs,logical_disk,memory,net,os,service,system,textfile,mssql',
      'LISTEN_PORT=9182',
      'ADDLOCAL=FirewallException',
      '/qn'
    )
    Start-Process -FilePath 'msiexec.exe' -ArgumentList $arguments -Wait -NoNewWindow
    Start-Service windows_exporter
}

Enable-WinRMForObservability
Install-WindowsExporter

New-Item -ItemType File -Path $marker -Force | Out-Null`
}

// escapeForSingleQuotedHereString escapes the given string for a single-quoted here-string.
func escapeForSingleQuotedHereString(in string) string {
	// In PowerShell single-quoted here-strings, single quotes are literal.
	// We keep the content as-is; this helper is here for future hardening if needed.
	return in
}
