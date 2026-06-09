package profiles

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
)

const DefaultFile = "appliance-profiles.json"

type Store struct {
	Profiles []Profile `json:"profiles"`
}

func Load(path string) (*Store, error) {
	if path == "" {
		path = DefaultFile
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return migrateLegacyStores(path)
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func migrateLegacyStores(path string) (*Store, error) {
	s := &Store{Profiles: []Profile{}}
	seen := map[string]struct{}{}

	// Old migration profiles (connections.json): url + token only
	if data, err := os.ReadFile("connections.json"); err == nil {
		var legacy struct {
			Profiles []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				URL     string `json:"url"`
				Token   string `json:"token"`
				SkipTLS bool   `json:"skipTls"`
			} `json:"profiles"`
		}
		if json.Unmarshal(data, &legacy) == nil {
			for _, p := range legacy.Profiles {
				pr := Profile{
					ID: p.ID, Name: p.Name, URL: p.URL, Token: p.Token, SkipTLS: p.SkipTLS,
				}
				if pr.ID == "" {
					pr.ID = NewID()
				}
				key := strings.ToLower(pr.URL)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				s.Profiles = append(s.Profiles, pr)
			}
		}
	}

	// Old fleet appliances (appliances.json)
	if data, err := os.ReadFile("appliances.json"); err == nil {
		var legacy struct {
			Appliances []Profile `json:"appliances"`
		}
		if json.Unmarshal(data, &legacy) == nil {
			for _, a := range legacy.Appliances {
				if a.ID == "" {
					a.ID = NewID()
				}
				key := strings.ToLower(a.URL)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				s.Profiles = append(s.Profiles, a)
			}
		}
	}

	if len(s.Profiles) > 0 {
		_ = Save(path, s)
	}
	return s, nil
}

func Save(path string, s *Store) error {
	if path == "" {
		path = DefaultFile
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "profile-" + hex.EncodeToString(b)
}

func (s *Store) Find(id string) *Profile {
	for i := range s.Profiles {
		if s.Profiles[i].ID == id {
			return &s.Profiles[i]
		}
	}
	return nil
}

func (s *Store) Upsert(p Profile) Profile {
	for i := range s.Profiles {
		if s.Profiles[i].ID == p.ID {
			if strings.TrimSpace(p.Password) == "" {
				p.Password = s.Profiles[i].Password
			}
			if strings.TrimSpace(p.Token) == "" && p.Username == "" {
				p.Token = s.Profiles[i].Token
			}
			if p.Token != "" {
				p.Password = ""
				p.Username = ""
			} else if p.Username != "" && p.Password != "" {
				p.Token = ""
			}
			s.Profiles[i] = p
			return p
		}
	}
	if p.ID == "" {
		p.ID = NewID()
	}
	s.Profiles = append(s.Profiles, p)
	return p
}

func (s *Store) Delete(id string) bool {
	out := s.Profiles[:0]
	found := false
	for _, p := range s.Profiles {
		if p.ID == id {
			found = true
			continue
		}
		out = append(out, p)
	}
	s.Profiles = out
	return found
}

func (s *Store) ListPublic() []PublicView {
	v := make([]PublicView, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		v = append(v, p.Public())
	}
	return v
}
