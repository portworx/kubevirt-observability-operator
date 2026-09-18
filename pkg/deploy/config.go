package deploy

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Config contains user-supplied and discovered deployment configuration.
type Config struct {
	S3           S3Config
	StorageClass string
}

// S3Config contains Loki object-storage configuration.
//
// Secret values are kept in memory only and must never be logged.
type S3Config struct {
	Bucket    string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
}

// LoadConfig obtains deployment configuration from environment variables
// when provided, otherwise it interactively prompts the user.
func LoadConfig(in io.Reader, out io.Writer) (*Config, error) {
	cfg := &Config{
		S3: S3Config{
			Bucket:    strings.TrimSpace(os.Getenv("LOKI_S3_BUCKET")),
			Endpoint:  strings.TrimSpace(os.Getenv("LOKI_S3_ENDPOINT")),
			Region:    strings.TrimSpace(os.Getenv("LOKI_S3_REGION")),
			AccessKey: os.Getenv("LOKI_S3_ACCESS_KEY"),
			SecretKey: os.Getenv("LOKI_S3_SECRET_KEY"),
		},
		StorageClass: strings.TrimSpace(os.Getenv("KVO_STORAGE_CLASS")),
	}

	reader := bufio.NewReader(in)

	var err error

	if cfg.S3.Bucket == "" {
		cfg.S3.Bucket, err = prompt(reader, out, "S3 bucket")
		if err != nil {
			return nil, err
		}
	}

	if cfg.S3.Endpoint == "" {
		cfg.S3.Endpoint, err = promptOptional(
			reader,
			out,
			"S3 endpoint (leave empty for AWS default)",
		)
		if err != nil {
			return nil, err
		}
	}

	if cfg.S3.Region == "" {
		cfg.S3.Region, err = promptOptional(
			reader,
			out,
			"S3 region (leave empty if not required)",
		)
		if err != nil {
			return nil, err
		}
	}

	if cfg.S3.AccessKey == "" {
		cfg.S3.AccessKey, err = promptSecret(
			reader,
			out,
			"S3 access key",
		)
		if err != nil {
			return nil, err
		}
	}

	if cfg.S3.SecretKey == "" {
		cfg.S3.SecretKey, err = promptSecret(
			reader,
			out,
			"S3 secret key",
		)
		if err != nil {
			return nil, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate validates the required deployment configuration.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.S3.Bucket) == "" {
		return fmt.Errorf("S3 bucket is required")
	}

	if strings.TrimSpace(c.S3.AccessKey) == "" {
		return fmt.Errorf("S3 access key is required")
	}

	if strings.TrimSpace(c.S3.SecretKey) == "" {
		return fmt.Errorf("S3 secret key is required")
	}

	return nil
}

func prompt(
	reader *bufio.Reader,
	out io.Writer,
	label string,
) (string, error) {
	for {
		fmt.Fprintf(out, "%s: ", label)

		value, err := reader.ReadString('\n')
		if err != nil && len(value) == 0 {
			return "", fmt.Errorf("read %s: %w", label, err)
		}

		value = strings.TrimSpace(value)
		if value != "" {
			return value, nil
		}

		fmt.Fprintln(out, "  Value is required.")
	}
}

func promptOptional(
	reader *bufio.Reader,
	out io.Writer,
	label string,
) (string, error) {
	fmt.Fprintf(out, "%s: ", label)

	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", fmt.Errorf("read %s: %w", label, err)
	}

	return strings.TrimSpace(value), nil
}

func promptSecret(
	reader *bufio.Reader,
	out io.Writer,
	label string,
) (string, error) {
	fmt.Fprintf(out, "%s: ", label)

	if term.IsTerminal(int(os.Stdin.Fd())) {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out)

		if err != nil {
			return "", fmt.Errorf("read %s: %w", label, err)
		}

		secret := strings.TrimSpace(string(value))
		if secret == "" {
			return "", fmt.Errorf("%s is required", label)
		}

		return secret, nil
	}

	// Non-interactive fallback. This primarily supports tests and redirected
	// input. Environment variables are preferred for automation.
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", fmt.Errorf("read %s: %w", label, err)
	}

	secret := strings.TrimSpace(value)
	if secret == "" {
		return "", fmt.Errorf("%s is required", label)
	}

	return secret, nil
}
