package alerts

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed vm-metrics-alerts-template.yaml
var vmMetricsTemplate string

//go:embed vm-loki-failure-alerts-template.yaml
var vmLokiTemplate string

func RenderVMMetrics(namespace string) ([]byte, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	return []byte(
		strings.ReplaceAll(
			vmMetricsTemplate,
			"__NAMESPACE__",
			namespace,
		),
	), nil
}

func RenderVMLoki(namespace string) ([]byte, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	rendered := strings.ReplaceAll(
		vmLokiTemplate,
		"__NAMESPACE__",
		namespace,
	)

	return []byte(rendered), nil
}
