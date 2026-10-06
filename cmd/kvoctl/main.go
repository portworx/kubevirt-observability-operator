package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/portworx/kubevirt-observability-operator/pkg/deploy"
	"github.com/portworx/kubevirt-observability-operator/pkg/onboarding"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()

	var err error

	switch os.Args[1] {
	case "preflight":
		err = deploy.RunPreflight(
			ctx,
			os.Stdout,
		)

	case "deploy":
		err = deploy.Run(
			ctx,
			os.Stdin,
			os.Stdout,
		)

	case "onboard":
		err = runOnboard(
			ctx,
			os.Args[2:],
		)

	case "version":
		fmt.Printf(
			"kvoctl %s\ncommit: %s\nbuild date: %s\n",
			version,
			commit,
			buildDate,
		)

	default:
		fmt.Printf(
			"unknown command: %s\n\n",
			os.Args[1],
		)

		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"\nERROR: %v\n",
			err,
		)
		os.Exit(1)
	}
}

func runOnboard(
	ctx context.Context,
	args []string,
) error {
	fs := flag.NewFlagSet(
		"onboard",
		flag.ContinueOnError,
	)

	fs.SetOutput(os.Stderr)

	cfg := onboarding.Config{}

	cfg.LinuxUser = os.Getenv("KVO_LINUX_USER")
	cfg.LinuxPassword = os.Getenv("KVO_LINUX_PASSWORD")

	cfg.WindowsUser = os.Getenv("KVO_WINDOWS_USER")
	cfg.WindowsPassword = os.Getenv("KVO_WINDOWS_PASSWORD")

	cfg.JumpHost = os.Getenv("KVO_OCP_JUMP_HOST")
	cfg.JumpUser = os.Getenv("KVO_OCP_JUMP_USER")
	cfg.JumpKey = os.Getenv("KVO_OCP_JUMP_KEY")

	fs.StringVar(
		&cfg.Namespace,
		"namespace",
		"",
		"limit onboarding to one namespace",
	)

	fs.StringVar(
		&cfg.VMName,
		"vm",
		"",
		"limit onboarding to one VM",
	)

	fs.StringVar(
		&cfg.LinuxSelector,
		"linux-selector",
		"",
		"Kubernetes label selector identifying Linux VMs",
	)

	fs.StringVar(
		&cfg.WindowsSelector,
		"windows-selector",
		"",
		"Kubernetes label selector identifying Windows VMs",
	)

	fs.BoolVar(
		&cfg.DryRun,
		"dry-run",
		false,
		"show onboarding plan without making changes",
	)

	if err := fs.Parse(args); err != nil {
		return err
	}

	if cfg.VMName != "" && cfg.Namespace == "" {
		return fmt.Errorf(
			"--vm requires --namespace",
		)
	}

	return onboarding.Run(
		ctx,
		os.Stdout,
		cfg,
	)
}

func printUsage() {
	fmt.Print(`kvoctl - KubeVirt Observability CLI

Usage:
  kvoctl <command>

Commands:
  preflight    Check cluster prerequisites
  deploy       Deploy the KubeVirt Observability Platform
  onboard      Integrate existing VMs with KubeVirt Observability
  version      Print kvoctl version information

Examples:
  kvoctl preflight
  kvoctl deploy
  kvoctl onboard --dry-run
  kvoctl onboard --namespace my-vms
  kvoctl onboard --linux-selector 'kubevirt.io/os=ubuntu'
  kvoctl version

Environment variables for non-interactive deployment:
  LOKI_S3_BUCKET
  LOKI_S3_ENDPOINT
  LOKI_S3_REGION
  LOKI_S3_ACCESS_KEY
  LOKI_S3_SECRET_KEY
  KVO_STORAGE_CLASS
  KVO_OPERATOR_IMAGE
  KVO_CONFIG_DIR

Optional alerting:

  KVO_SLACK_WEBHOOK_URL
      Slack incoming webhook used for alert notifications.
      When omitted, alert integration is not configured and deployment continues normally.
`)
}
