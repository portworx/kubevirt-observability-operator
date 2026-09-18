package grafana

const (
	DefaultNamespace = "grafana"

	DefaultGrafanaImage = "grafana/grafana:11.5.0"

	DefaultPrometheusUID = "kvo-prometheus"
	DefaultLokiUID       = "kvo-loki"

	DefaultPrometheusURL = "https://thanos-querier.openshift-monitoring.svc:9091"
	DefaultLokiURL       = "https://logging-loki-gateway-http.openshift-logging.svc:8080"

	GrafanaDeploymentName = "grafana"
	GrafanaServiceAccount = "grafana"

	DatasourceConfigMapName        = "grafana-source-config"
	DashboardProviderConfigMapName = "grafana-dashboard-config"
	DashboardsConfigMapName        = "grafana-dashboards"

	PrometheusTokenSecretName = "grafana-prometheus-token"
	LokiTokenSecretName       = "grafana-loki-token"

	DefaultDashboardDir = "config/grafana/dashboards"
)

type Config struct {
	Namespace string

	GrafanaImage string

	PrometheusURL string
	LokiURL       string

	PrometheusUID string
	LokiUID       string

	DashboardDir string

	PrometheusTokenNamespace      string
	PrometheusTokenServiceAccount string

	LokiTokenNamespace      string
	LokiTokenServiceAccount string
}

func DefaultConfig() Config {
	return Config{
		Namespace: DefaultNamespace,

		GrafanaImage: DefaultGrafanaImage,

		PrometheusURL: DefaultPrometheusURL,
		LokiURL:       DefaultLokiURL,

		PrometheusUID: DefaultPrometheusUID,
		LokiUID:       DefaultLokiUID,

		DashboardDir: DefaultDashboardDir,

		PrometheusTokenNamespace:      DefaultNamespace,
		PrometheusTokenServiceAccount: GrafanaServiceAccount,

		LokiTokenNamespace:      DefaultNamespace,
		LokiTokenServiceAccount: GrafanaServiceAccount,
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Namespace == "" {
		cfg.Namespace = DefaultNamespace
	}

	if cfg.GrafanaImage == "" {
		cfg.GrafanaImage = DefaultGrafanaImage
	}

	if cfg.PrometheusURL == "" {
		cfg.PrometheusURL = DefaultPrometheusURL
	}

	if cfg.LokiURL == "" {
		cfg.LokiURL = DefaultLokiURL
	}

	if cfg.PrometheusUID == "" {
		cfg.PrometheusUID = DefaultPrometheusUID
	}

	if cfg.LokiUID == "" {
		cfg.LokiUID = DefaultLokiUID
	}

	if cfg.DashboardDir == "" {
		cfg.DashboardDir = DefaultDashboardDir
	}

	if cfg.PrometheusTokenNamespace == "" {
		cfg.PrometheusTokenNamespace = cfg.Namespace
	}

	if cfg.PrometheusTokenServiceAccount == "" {
		cfg.PrometheusTokenServiceAccount = GrafanaServiceAccount
	}

	if cfg.LokiTokenNamespace == "" {
		cfg.LokiTokenNamespace = cfg.Namespace
	}

	if cfg.LokiTokenServiceAccount == "" {
		cfg.LokiTokenServiceAccount = GrafanaServiceAccount
	}
}
