package main

import (
	"context"
	"fmt"
	"os"

	"github.com/portworx/kubevirt-observability-operator/pkg/deploy"
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

func printUsage() {
	fmt.Print(`kvoctl - KubeVirt Observability CLI

Usage:
  kvoctl <command>

Commands:
  preflight    Check cluster prerequisites
  deploy       Deploy the KubeVirt Observability Platform

Examples:
  kvoctl preflight
  kvoctl deploy

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
