package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/client-go/kubernetes"
)

const (
	KVODashboardProviderConfigKey = "kvo-dashboards.yaml"
)

const dashboardProvider = `apiVersion: 1

providers:
  - name: KubeVirt Observability
    orgId: 1
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
`

func EnsureDashboardConfigMaps(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	applyDefaults(&cfg)

	if err := ensureConfigMapData(
		ctx,
		kube,
		cfg.Namespace,
		DashboardProviderConfigMapName,
		map[string]string{
			KVODashboardProviderConfigKey: dashboardProvider,
		},
	); err != nil {
		return fmt.Errorf(
			"ensure dashboard provider ConfigMap: %w",
			err,
		)
	}

	dashboards, err := loadDashboards(
		cfg.DashboardDir,
	)
	if err != nil {
		return err
	}

	if err := ensureConfigMapData(
		ctx,
		kube,
		cfg.Namespace,
		DashboardsConfigMapName,
		dashboards,
	); err != nil {
		return fmt.Errorf(
			"ensure dashboards ConfigMap: %w",
			err,
		)
	}

	return nil
}

func loadDashboards(
	dir string,
) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf(
			"read dashboard directory %q: %w",
			dir,
			err,
		)
	}

	names := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.EqualFold(
			filepath.Ext(entry.Name()),
			".json",
		) {
			continue
		}

		names = append(
			names,
			entry.Name(),
		)
	}

	sort.Strings(names)

	if len(names) == 0 {
		return nil, fmt.Errorf(
			"no dashboard JSON files found in %q",
			dir,
		)
	}

	dashboards := make(map[string]string)

	for _, name := range names {
		path := filepath.Join(
			dir,
			name,
		)

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf(
				"read dashboard %q: %w",
				path,
				err,
			)
		}

		var dashboard interface{}

		if err := json.Unmarshal(
			data,
			&dashboard,
		); err != nil {
			return nil, fmt.Errorf(
				"dashboard %q contains invalid JSON: %w",
				path,
				err,
			)
		}

		dashboards[name] = string(data)
	}

	return dashboards, nil
}
