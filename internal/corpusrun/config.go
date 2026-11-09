// Package corpusrun compares a Glade binary's results on pinned corpus repos
// with committed golden files. The code names no repo: tier lists, overlays
// and goldens live in a private config directory.
package corpusrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TiersFile is the tier list inside the config directory.
const TiersFile = "tiers.json"

// PathList is a JSON string or a list of strings.
type PathList []string

func (p *PathList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*p = PathList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("want a string or a list of strings")
	}
	*p = PathList(many)
	return nil
}

// OverlayConfig is the glade.yml content the runner writes into the copy.
type OverlayConfig struct {
	Features       []string `json:"features,omitempty"`
	PermissionSets []string `json:"permissionSets,omitempty"`
}

// Overlay is what the runner adds to a clone before it runs.
type Overlay struct {
	Deps   []string      `json:"deps,omitempty"`
	Config OverlayConfig `json:"config"`
}

// DepSource says where a dependency's source lives.
type DepSource struct {
	Paths PathList `json:"paths,omitempty"`
	Dirs  []string `json:"dirs,omitempty"`
	Mode  string   `json:"mode,omitempty"`
	Note  string   `json:"note,omitempty"`
}

// Repo is one pinned corpus repo.
type Repo struct {
	Name      string   `json:"name"`
	Tier      string   `json:"tier"`
	Clone     PathList `json:"clone"`
	Pin       string   `json:"pin"`
	SourceAPI string   `json:"sourceApi,omitempty"`
	License   string   `json:"license,omitempty"`
	Overlay   Overlay  `json:"overlay"`
}

// Config is the parsed tiers file.
type Config struct {
	SchemaVersion int                  `json:"schemaVersion"`
	DepSources    map[string]DepSource `json:"depSources,omitempty"`
	Repos         []Repo               `json:"repos"`

	dir string
}

// Dir is the config directory.
func (c *Config) Dir() string { return c.dir }

var tierRank = map[string]int{"A": 1, "B": 2, "C": 3}

// LoadConfig reads and validates dir/tiers.json.
func LoadConfig(dir string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, TiersFile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", TiersFile, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	config := &Config{dir: dir}
	if err := dec.Decode(config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", TiersFile, err)
	}
	if config.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: schemaVersion %d is not supported", TiersFile, config.SchemaVersion)
	}
	for name, dep := range config.DepSources {
		switch dep.Mode {
		case "", "source", "managed", "stub":
		default:
			return nil, fmt.Errorf("%s: dependency %q has unknown mode %q", TiersFile, name, dep.Mode)
		}
	}
	seen := map[string]bool{}
	for _, repo := range config.Repos {
		if repo.Name == "" || strings.ContainsAny(repo.Name, `/\`) || strings.Contains(repo.Name, "..") {
			return nil, fmt.Errorf("%s: repo name %q is not a plain name", TiersFile, repo.Name)
		}
		if seen[repo.Name] {
			return nil, fmt.Errorf("%s: duplicate repo %q", TiersFile, repo.Name)
		}
		seen[repo.Name] = true
		if tierRank[repo.Tier] == 0 {
			return nil, fmt.Errorf("%s: repo %q has tier %q, want A, B or C", TiersFile, repo.Name, repo.Tier)
		}
		if repo.Pin == "" {
			return nil, fmt.Errorf("%s: repo %q has no pin", TiersFile, repo.Name)
		}
		if len(repo.Clone) == 0 {
			return nil, fmt.Errorf("%s: repo %q has no clone", TiersFile, repo.Name)
		}
		for _, dep := range repo.Overlay.Deps {
			if _, ok := config.DepSources[dep]; !ok {
				return nil, fmt.Errorf("%s: repo %q uses undeclared dependency %q", TiersFile, repo.Name, dep)
			}
		}
	}
	return config, nil
}

// Select returns the repos of a tier (tiers are nested: C includes B and A),
// narrowed by only, sorted by name.
func (c *Config) Select(tier string, only []string) ([]Repo, error) {
	rank := tierRank[tier]
	if rank == 0 {
		return nil, fmt.Errorf("unknown tier %q, want A, B or C", tier)
	}
	byName := map[string]Repo{}
	for _, repo := range c.Repos {
		byName[repo.Name] = repo
	}
	var out []Repo
	if len(only) == 0 {
		for _, repo := range c.Repos {
			if tierRank[repo.Tier] <= rank {
				out = append(out, repo)
			}
		}
	} else {
		for _, name := range only {
			repo, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("repo %q is not in %s", name, TiersFile)
			}
			if tierRank[repo.Tier] > rank {
				return nil, fmt.Errorf("repo %q is tier %s, outside tier %s", name, repo.Tier, tier)
			}
			out = append(out, repo)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ResolvePath returns the first listed directory that exists. Relative paths
// are relative to the config directory; ~/ is the home directory.
func (c *Config) ResolvePath(paths PathList) (string, bool) {
	for _, p := range paths {
		if strings.HasPrefix(p, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			p = filepath.Join(home, p[2:])
		} else if !filepath.IsAbs(p) {
			p = filepath.Join(c.dir, p)
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p, true
		}
	}
	return "", false
}
