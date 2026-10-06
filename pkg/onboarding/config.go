package onboarding

type Config struct {
	Namespace       string
	VMName          string
	LinuxSelector   string
	WindowsSelector string
	DryRun          bool

	LinuxUser     string
	LinuxPassword string

	WindowsUser     string
	WindowsPassword string

	SSHPort int

	JumpHost string
	JumpUser string
	JumpPort int
	JumpKey  string

	KeySecretNamespace string
	KeySecretName      string
}

type VMState string

const (
	StateReady          VMState = "READY TO ONBOARD"
	StateAlreadyManaged VMState = "ALREADY MANAGED"
	StateInProgress     VMState = "IN PROGRESS"
	StateSkipped        VMState = "SKIPPED"
	StateConflict       VMState = "CONFLICT"
	StateOnboarded      VMState = "ONBOARDED"
	StateFailed         VMState = "FAILED"
)

type Candidate struct {
	Namespace string
	Name      string
	IP        string
	OS        string
	OSSource  string
	State     VMState
	Reason    string
}

func applyDefaults(cfg *Config) {
	if cfg.SSHPort == 0 {
		cfg.SSHPort = 22
	}

	if cfg.JumpUser == "" {
		cfg.JumpUser = "core"
	}

	if cfg.JumpPort == 0 {
		cfg.JumpPort = 22
	}

	if cfg.WindowsUser == "" {
		cfg.WindowsUser = "Administrator"
	}

	if cfg.KeySecretNamespace == "" {
		cfg.KeySecretNamespace = "kubevirt-observability-system"
	}

	if cfg.KeySecretName == "" {
		cfg.KeySecretName = "lin-vm-mon-secret"
	}
}
