package profiles

import (
	"sync"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// FileRepository stores profiles on disk and snapshots in memory.
type FileRepository struct {
	path     string
	snapshots sync.Map // profileID -> *morpheus.ApplianceSnapshot
}

func (r *FileRepository) load() (*Store, error) {
	return Load(r.path)
}

func (r *FileRepository) save(s *Store) error {
	return Save(r.path, s)
}

func (r *FileRepository) List() ([]Profile, error) {
	s, err := r.load()
	if err != nil {
		return nil, err
	}
	return append([]Profile(nil), s.Profiles...), nil
}

func (r *FileRepository) Find(id string) (*Profile, error) {
	s, err := r.load()
	if err != nil {
		return nil, err
	}
	p := s.Find(id)
	if p == nil {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *FileRepository) Upsert(p Profile) (Profile, error) {
	s, err := r.load()
	if err != nil {
		return Profile{}, err
	}
	saved := s.Upsert(p)
	if err := r.save(s); err != nil {
		return Profile{}, err
	}
	return saved, nil
}

func (r *FileRepository) Delete(id string) (bool, error) {
	s, err := r.load()
	if err != nil {
		return false, err
	}
	if !s.Delete(id) {
		return false, nil
	}
	r.snapshots.Delete(id)
	if err := r.save(s); err != nil {
		return false, err
	}
	return true, nil
}

func (r *FileRepository) ListPublic() ([]PublicView, error) {
	s, err := r.load()
	if err != nil {
		return nil, err
	}
	return s.ListPublic(), nil
}

func (r *FileRepository) SaveSnapshot(profileID string, snap *morpheus.ApplianceSnapshot) error {
	if snap != nil {
		r.snapshots.Store(profileID, snap)
	}
	return nil
}

func (r *FileRepository) LatestSnapshot(profileID string) (*morpheus.ApplianceSnapshot, error) {
	v, ok := r.snapshots.Load(profileID)
	if !ok {
		return nil, ErrNotFound
	}
	snap, ok := v.(*morpheus.ApplianceSnapshot)
	if !ok {
		return nil, ErrNotFound
	}
	return snap, nil
}

func (r *FileRepository) LatestSnapshots() (map[string]*morpheus.ApplianceSnapshot, error) {
	out := make(map[string]*morpheus.ApplianceSnapshot)
	r.snapshots.Range(func(key, value any) bool {
		id, _ := key.(string)
		snap, _ := value.(*morpheus.ApplianceSnapshot)
		if id != "" && snap != nil {
			out[id] = snap
		}
		return true
	})
	return out, nil
}

func (r *FileRepository) DeleteSnapshots(profileID string) error {
	r.snapshots.Delete(profileID)
	return nil
}

func (r *FileRepository) ClearCache(opts ClearCacheOptions) (ClearCacheResult, error) {
	opts.Normalize()
	out := ClearCacheResult{Postgres: false}
	if !opts.FleetSnapshots {
		return out, nil
	}
	var keys []any
	r.snapshots.Range(func(key, _ any) bool {
		keys = append(keys, key)
		return true
	})
	for _, key := range keys {
		r.snapshots.Delete(key)
		out.FleetSnapshots++
	}
	return out, nil
}

// FileRepository delegates JSONB-only features to NoopExtras.
type fileRepo struct {
	*FileRepository
	NoopExtras
}

func NewFileRepository(path string) (Repository, error) {
	if path == "" {
		path = DefaultFile
	}
	if _, err := Load(path); err != nil {
		return nil, err
	}
	return &fileRepo{FileRepository: &FileRepository{path: path}}, nil
}
