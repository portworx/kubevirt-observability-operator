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

	var err error

	switch os.Args[1] {
	case "preflight":
		err = deploy.RunPreflight(
			context.Background(),
			os.Stdout,
		)

	case "deploy":
		fmt.Println("kvoctl deploy is not implemented yet")

	default:
		fmt.Printf("unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`kvoctl - KubeVirt Observability CLI

Usage:
  kvoctl <command>

Commands:
  preflight    Check cluster prerequisites
  deploy       Deploy KubeVirt Observability Platform (coming soon)

Examples:
  kvoctl preflight
`)
}
