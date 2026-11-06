package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/kevin/jira-tui/internal/i18n"
)

type Filter struct {
	Name string `yaml:"name"`
	JQL  string `yaml:"jql"`
}

type Config struct {
	URL     string   `yaml:"url"`
	Email   string   `yaml:"email"`
	Token   string   `yaml:"token"`
	JQL     string   `yaml:"default_jql,omitempty"`
	Filters []Filter `yaml:"filters,omitempty"`

	// Tempo (optional): without a token, time tracking is disabled.
	TempoToken  string `yaml:"tempo_token,omitempty"`
	TempoURL    string `yaml:"tempo_url,omitempty"`
	HoursPerDay int    `yaml:"hours_per_day,omitempty"`

	// Jira Automation (manual rules). All optional.
	CloudID       string       `yaml:"cloud_id,omitempty"`       // detected through /_edge/tenant_info when empty
	AutomationURL string       `yaml:"automation_url,omitempty"` // API root override, e.g. https://api.atlassian.com/automation/public
	Automations   []Automation `yaml:"automations,omitempty"`    // rules always offered (fallback when the search fails)
}

// Automation declares a manual rule by hand.
type Automation struct {
	Name string `yaml:"name"`
	ID   string `yaml:"id"`
}

const (
	DefaultTempoURL    = "https://api.tempo.io/4"
	DefaultHoursPerDay = 8
)

const DefaultJQL = "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"

func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "jira-tui", "config.yaml")
}

// LoadFile reads only the config file (no environment, no defaults). A missing file is not an error.
func LoadFile(path string) (Config, error) {
	var c Config
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &c); err != nil {
			return c, fmt.Errorf("%s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return c, err
	}
	return c, nil
}

// Load reads the config file (optional), overrides it with environment variables and applies defaults.
func Load(path string) (Config, error) {
	c, err := LoadFile(path)
	if err != nil {
		return c, err
	}
	for env, dst := range map[string]*string{
		"JIRA_URL": &c.URL, "JIRA_EMAIL": &c.Email, "JIRA_TOKEN": &c.Token,
		"TEMPO_TOKEN": &c.TempoToken, "TEMPO_URL": &c.TempoURL,
	} {
		if v := os.Getenv(env); v != "" {
			*dst = v
		}
	}
	if c.JQL == "" {
		c.JQL = DefaultJQL
	}
	if c.TempoURL == "" {
		c.TempoURL = DefaultTempoURL
	}
	if c.HoursPerDay <= 0 {
		c.HoursPerDay = DefaultHoursPerDay
	}
	return c, nil
}

// Save writes the config to path (mode 0600, directory 0700). The default JQL is
// not written so that it can evolve with the app.
func Save(path string, c Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if c.JQL == DefaultJQL {
		c.JQL = ""
	}
	if c.TempoURL == DefaultTempoURL {
		c.TempoURL = ""
	}
	if c.HoursPerDay == DefaultHoursPerDay {
		c.HoursPerDay = 0
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600) // WriteFile keeps the mode of an existing file
}

// Validate reports which required settings are missing.
func (c Config) Validate() error {
	var missing []string
	if c.URL == "" {
		missing = append(missing, "JIRA_URL")
	}
	if c.Email == "" {
		missing = append(missing, "JIRA_EMAIL")
	}
	if c.Token == "" {
		missing = append(missing, "JIRA_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf(i18n.T("configuration incomplète : %v (variables d'env ou %s)", "incomplete configuration: %v (environment variables or %s)"), missing, DefaultPath())
	}
	return nil
}
