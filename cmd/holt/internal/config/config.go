// Package config loads the operator CLI's connection profiles, used by
// the admin commands to reach a remote hub (e.g. one fronted by an
// authenticating proxy). A profile carries the admin URL and any extra
// HTTP headers to send; header values expand ${ENV} references so
// secrets stay out of the file. It is optional — with no config file,
// the commands fall back to their built-in defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// Config is the on-disk CLI config: named profiles and which one is
// used when none is requested.
type Config struct {
	DefaultProfile string             `yaml:"default_profile"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

// Profile is one hub connection: its admin URL and extra headers.
type Profile struct {
	AdminURL string            `yaml:"admin_url"`
	Headers  map[string]string `yaml:"headers"`
}

// DefaultPath is ~/.holt/config.yaml.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".holt", "config.yaml")
}

// Load reads the config at path (or DefaultPath when empty). A missing
// file is not an error: it returns an empty config so the CLI keeps its
// built-in defaults.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	var c Config
	if unmarshalErr := yaml.Unmarshal(data, &c); unmarshalErr != nil {
		return nil, fmt.Errorf("config %s: %w", path, unmarshalErr)
	}

	return &c, nil
}

// Pick returns the named profile, or the default profile when name is
// empty. An unknown or empty selection yields a zero Profile (built-in
// defaults apply).
func (c *Config) Pick(name string) Profile {
	if name == "" {
		name = c.DefaultProfile
	}

	if name == "" {
		return Profile{}
	}

	return c.Profiles[name]
}

// ResolvedHeaders returns the profile headers with ${ENV} / $ENV
// references expanded from the environment, so secrets can live in env
// rather than in the file.
func (p Profile) ResolvedHeaders() map[string]string {
	out := make(map[string]string, len(p.Headers))
	for k, v := range p.Headers {
		out[k] = os.Expand(v, os.Getenv)
	}

	return out
}
