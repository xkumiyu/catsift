package skillinventory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xkumiyu/catsift/internal/usage"
)

// NameSource identifies how an inventory entry got its canonical name.
type NameSource string

const (
	NameSourceFrontmatter NameSource = "frontmatter"
	NameSourceDirectory   NameSource = "directory"
)

// InventoryEntry identifies one physical installed Skill.
type InventoryEntry struct {
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	NameSource   NameSource `json:"name_source"`
	NameMismatch bool       `json:"name_mismatch"`
}

// InventorySnapshot is the filesystem state used by the unused report.
type InventorySnapshot struct {
	Roots          []string
	InstalledCount int
	Entries        []InventoryEntry
	Warnings       []usage.Warning
}

// DiscoverOptions controls filesystem inventory discovery.
type DiscoverOptions struct {
	Roots             []string
	AllowMissingRoots bool
}

type skillManifest struct {
	Name         string
	Namespace    string
	ManifestPath string
	SkillPath    string
}

// ResolveRoots resolves either the default user Skill root or explicit roots.
func ResolveRoots(explicit []string, userHome string) ([]string, error) {
	if len(explicit) == 0 {
		if strings.TrimSpace(userHome) == "" {
			return nil, errors.New("user home is empty")
		}
		root, err := filepath.Abs(filepath.Join(userHome, ".agents", "skills"))
		if err != nil {
			return nil, fmt.Errorf("resolve default skill root: %w", err)
		}
		return []string{filepath.Clean(root)}, nil
	}

	roots := make([]string, 0, len(explicit))
	seen := make(map[string]struct{}, len(explicit))
	for _, raw := range explicit {
		if strings.TrimSpace(raw) == "" {
			return nil, errors.New("skill root is empty")
		}
		root, err := filepath.Abs(filepath.Clean(raw))
		if err != nil {
			return nil, fmt.Errorf("resolve skill root %q: %w", raw, err)
		}
		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots, nil
}

// Discover validates roots and returns their current inventory. The actual
// recursive walk is intentionally kept in the same entry point so callers can
// apply one consistent validation policy to every root.
func Discover(options DiscoverOptions) (InventorySnapshot, error) {
	roots := uniqueSortedRoots(options.Roots)
	if len(roots) == 0 {
		return InventorySnapshot{}, errors.New("no skill roots specified")
	}
	snapshot := InventorySnapshot{Roots: roots}
	scanRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			if options.AllowMissingRoots && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return InventorySnapshot{}, fmt.Errorf("stat skill root %q: %w", root, err)
		}
		if !info.IsDir() {
			return InventorySnapshot{}, fmt.Errorf("skill root %q is not a directory", root)
		}
		scanRoots = append(scanRoots, root)
	}
	entries := make(map[string]InventoryEntry)
	warnings := make([]usage.Warning, 0)
	for _, root := range scanRoots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if filepath.Clean(path) == root {
					return walkErr
				}
				warnings = append(warnings, usage.Warning{
					Reason: "cannot read skill inventory path",
					Type:   "skill_inventory_walk",
					Path:   filepath.Clean(path),
					Count:  1,
				})
				return nil
			}
			if entry == nil {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			candidate, ok := classifySkillManifest(path)
			if !ok {
				return nil
			}
			if _, exists := entries[candidate.SkillPath]; exists {
				return nil
			}
			name := candidate.Name
			nameSource := NameSourceDirectory
			nameMismatch := false
			if frontmatter := usage.FrontmatterSkillName(candidate.ManifestPath); frontmatter != "" {
				name = frontmatter
				nameSource = NameSourceFrontmatter
				nameMismatch = finalNameComponent(frontmatter) != filepath.Base(candidate.SkillPath)
			}
			entries[candidate.SkillPath] = InventoryEntry{
				Name:         name,
				Path:         candidate.SkillPath,
				NameSource:   nameSource,
				NameMismatch: nameMismatch,
			}
			return nil
		})
		if err != nil {
			return InventorySnapshot{}, fmt.Errorf("walk skill root %q: %w", root, err)
		}
	}

	entriesList := make([]InventoryEntry, 0, len(entries))
	for _, entry := range entries {
		entriesList = append(entriesList, entry)
	}
	sort.Slice(entriesList, func(i, j int) bool {
		if entriesList[i].Name == entriesList[j].Name {
			return entriesList[i].Path < entriesList[j].Path
		}
		return entriesList[i].Name < entriesList[j].Name
	})
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Path == warnings[j].Path {
			return warnings[i].Type < warnings[j].Type
		}
		return warnings[i].Path < warnings[j].Path
	})
	snapshot.InstalledCount = len(entriesList)
	snapshot.Entries = entriesList
	snapshot.Warnings = warnings
	return snapshot, nil
}

func finalNameComponent(name string) string {
	if separator := strings.LastIndexByte(name, ':'); separator >= 0 {
		return name[separator+1:]
	}
	return name
}

func uniqueSortedRoots(roots []string) []string {
	seen := make(map[string]struct{}, len(roots))
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		clean := filepath.Clean(root)
		if strings.TrimSpace(root) == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	sort.Strings(result)
	return result
}

// classifySkillManifest recognizes only the supported installed-Skill layouts.
// History path fallback is intentionally not sufficient for this classifier.
func classifySkillManifest(path string) (skillManifest, bool) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if clean == "." || !strings.EqualFold(filepath.Base(clean), "SKILL.md") {
		return skillManifest{}, false
	}
	parts := strings.Split(clean, "/")
	for skillsIndex, part := range parts {
		if !strings.EqualFold(part, "skills") {
			continue
		}

		if skillsIndex > 0 && (strings.EqualFold(parts[skillsIndex-1], ".agents") || strings.EqualFold(parts[skillsIndex-1], ".codex")) {
			nameIndex := skillsIndex + 1
			if strings.EqualFold(parts[skillsIndex-1], ".codex") && nameIndex < len(parts) && strings.EqualFold(parts[nameIndex], ".system") {
				nameIndex++
			}
			if candidate, ok := newSkillManifest(clean, parts, nameIndex, ""); ok {
				return candidate, true
			}
		}

		if skillsIndex >= 5 && strings.EqualFold(parts[skillsIndex-5], "plugins") && strings.EqualFold(parts[skillsIndex-4], "cache") {
			namespace := strings.TrimSpace(parts[skillsIndex-2])
			if candidate, ok := newSkillManifest(clean, parts, skillsIndex+1, namespace); ok {
				return candidate, true
			}
		}
	}
	return skillManifest{}, false
}

func newSkillManifest(path string, parts []string, nameIndex int, namespace string) (skillManifest, bool) {
	if nameIndex < 1 || nameIndex+1 != len(parts)-1 {
		return skillManifest{}, false
	}
	directoryName := strings.TrimSpace(parts[nameIndex])
	if directoryName == "" || usage.SkillNameFromPath(path) == "" {
		return skillManifest{}, false
	}
	if namespace != "" && usage.SkillNameFromPath(path) != namespace+":"+directoryName {
		return skillManifest{}, false
	}
	return skillManifest{
		Name:         usage.SkillNameFromPath(path),
		Namespace:    namespace,
		ManifestPath: filepath.FromSlash(path),
		SkillPath:    filepath.Dir(filepath.FromSlash(path)),
	}, true
}
