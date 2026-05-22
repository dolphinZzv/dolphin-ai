package credentials

import "dolphin/internal/config"

type Credential struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type CredentialFile struct {
	Credentials []Credential `json:"credentials"`
}

type CredentialsConfig = config.CredentialsConfig

func DefaultConfig() config.CredentialsConfig {
	return config.CredentialsConfig{
		Enabled:    false,
		Store:      "file",
		Path:       ".dolphin/credentials.json",
		SafeFields: []string{"name", "type", "url", "username", "comment"},
	}
}
