package profiles

import (
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// Profile is a saved Morpheus appliance connection (credentials stored locally).
type Profile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	SkipTLS  bool   `json:"skipTls"`
}

// PublicView is returned to the UI (secrets omitted).
type PublicView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Username   string `json:"username,omitempty"`
	SkipTLS    bool   `json:"skipTls"`
	AuthMethod string `json:"authMethod"` // "token" | "password"
}

func (p Profile) AuthMethod() string {
	if strings.TrimSpace(p.Token) != "" {
		return "token"
	}
	return "password"
}

func (p Profile) Public() PublicView {
	return PublicView{
		ID:         p.ID,
		Name:       p.Name,
		URL:        p.URL,
		Username:   p.Username,
		SkipTLS:    p.SkipTLS,
		AuthMethod: p.AuthMethod(),
	}
}

// Client builds a Morpheus API client from the profile credentials.
func (p Profile) Client() (*morpheus.Client, error) {
	url := strings.TrimRight(strings.TrimSpace(p.URL), "/")
	if url == "" {
		return nil, ErrMissingURL
	}
	if strings.TrimSpace(p.Token) != "" {
		return morpheus.NewClient(url, p.Token, p.SkipTLS), nil
	}
	user := strings.TrimSpace(p.Username)
	if user == "" || p.Password == "" {
		return nil, ErrMissingCredentials
	}
	return morpheus.NewClientFromPassword(url, user, p.Password, p.SkipTLS)
}

// ValidateSave checks required fields for create/update.
func (p Profile) ValidateSave(isNew bool, hadPassword bool, hadToken bool) error {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.URL) == "" {
		return ErrNameURLRequired
	}
	hasToken := strings.TrimSpace(p.Token) != ""
	hasPassword := strings.TrimSpace(p.Username) != "" && (p.Password != "" || (!isNew && hadPassword))
	if hasToken {
		return nil
	}
	if hasPassword {
		return nil
	}
	if isNew {
		return ErrAuthRequired
	}
	if hadToken || hadPassword {
		return nil // update with empty password keeps stored password via upsert
	}
	return ErrAuthRequired
}
