package alloyconfig

import (
	"fmt"
	"strings"
)

type VMLogConfig struct {
	Namespace string
	VMName    string
	Token     string
	LokiURL   string
}

func RenderLinux(c VMLogConfig) string {
	return fmt.Sprintf(`logging {
  level  = "info"
  format = "logfmt"
}

local.file "loki_token" {
  filename  = "/etc/alloy/secrets/loki.token"
  is_secret = true
}

loki.write "ocp_loki" {
  endpoint {
    url          = %q
    bearer_token = local.file.loki_token.content

    tls_config {
      insecure_skip_verify = true
    }
  }
}

local.file_match "linux_logs" {
  path_targets = [
    {
      "__path__" = "/var/log/messages",
      "filename" = "/var/log/messages",
      "log_file" = "messages",
    },
    {
      "__path__" = "/var/log/secure",
      "filename" = "/var/log/secure",
      "log_file" = "secure",
    },
    {
      "__path__" = "/var/log/syslog",
      "filename" = "/var/log/syslog",
      "log_file" = "syslog",
    },
  ]
}

loki.process "linux_syslog" {
  forward_to = [loki.write.ocp_loki.receiver]

  stage.static_labels {
    values = {
      log_type                  = "application",
      openshift_log_type        = "application",
      kubernetes_namespace_name = %q,
      namespace                 = %q,
      os                        = "linux",
      vm_name                   = %q,
      source                    = "linux_syslog",
    }
  }
}

loki.source.file "linux_files" {
  targets    = local.file_match.linux_logs.targets
  forward_to = [loki.process.linux_syslog.receiver]
}
`, c.LokiURL, c.Namespace, c.Namespace, c.VMName)
}

func RenderWindows(c VMLogConfig) string {
	return fmt.Sprintf(`logging {
  level  = "info"
  format = "logfmt"
}

local.file "loki_token" {
  filename  = "C:\\ProgramData\\GrafanaLabs\\Alloy\\secrets\\loki.token"
  is_secret = true
}

loki.write "ocp_loki" {
  endpoint {
    url          = %q
    bearer_token = local.file.loki_token.content

    tls_config {
      insecure_skip_verify = true
    }
  }
}

loki.process "windows_eventlog" {
  forward_to = [loki.write.ocp_loki.receiver]

  stage.static_labels {
    values = {
      log_type                  = "application",
      openshift_log_type        = "application",
      kubernetes_namespace_name = %q,
      namespace                 = %q,
      os                        = "windows",
      vm_name                   = %q,
      source                    = "windows_eventlog",
    }
  }
}

loki.source.windowsevent "application" {
  eventlog_name          = "Application"
  use_incoming_timestamp = true
  forward_to             = [loki.process.windows_eventlog.receiver]

  labels = {
    channel = "Application",
  }
}

loki.source.windowsevent "system" {
  eventlog_name          = "System"
  use_incoming_timestamp = true
  forward_to             = [loki.process.windows_eventlog.receiver]

  labels = {
    channel = "System",
  }
}
`, c.LokiURL, c.Namespace, c.Namespace, c.VMName)
}

func Validate(c VMLogConfig) error {
	if strings.TrimSpace(c.Namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(c.VMName) == "" {
		return fmt.Errorf("vm name is required")
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("loki token is required")
	}
	if strings.TrimSpace(c.LokiURL) == "" {
		return fmt.Errorf("loki url is required")
	}
	return nil
}
