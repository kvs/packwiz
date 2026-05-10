package core

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Mod stores metadata about a mod. This is written to a TOML file for each mod.
type Mod struct {
	metaFile string      // The file for the metadata file, used as an ID
	Name     string      `toml:"name"`
	FileName string      `toml:"filename"`
	Side     string      `toml:"side,omitempty"`
	Pin      bool        `toml:"pin,omitempty"`
	Mirror   bool        `toml:"mirror,omitempty"`
	Download ModDownload `toml:"download"`
	// Update is a map of map of stuff, so you can store arbitrary values on string keys to define updating
	Update     map[string]map[string]interface{} `toml:"update"`
	updateData map[string]interface{}
	// SkipVersions stores version IDs that should be skipped during updates.
	// This is useful for skipping beta/alpha releases without pinning the mod.
	SkipVersions []string `toml:"skip-versions,omitempty"`

	Option *ModOption `toml:"option,omitempty"`
}

const (
	ModeURL    string = "url"
	ModeCF     string = "metadata:curseforge"
	ModeMirror string = "mirror"
)

// ModDownload specifies how to download the mod file
type ModDownload struct {
	URL        string `toml:"url,omitempty"`
	HashFormat string `toml:"hash-format"`
	Hash       string `toml:"hash"`
	// Mode defaults to modeURL (i.e. use URL when omitted or empty)
	Mode string `toml:"mode,omitempty"`
	// MirrorURL stores the original download URL before mirroring, so unmirror can restore it.
	MirrorURL string `toml:"mirror-url,omitempty"`
	// MirrorMode stores the original download mode before mirroring.
	MirrorMode string `toml:"mirror-mode,omitempty"`
}

// ModOption specifies optional metadata for this mod file
type ModOption struct {
	Optional    bool   `toml:"optional"`
	Description string `toml:"description,omitempty"`
	Default     bool   `toml:"default,omitempty"`
}

// The four possible values of Side (the side that the mod is on) are "server", "client", "both", and "" (equivalent to "both")
const (
	ServerSide    = "server"
	ClientSide    = "client"
	UniversalSide = "both"
	EmptySide     = ""
)

// LoadMod attempts to load a mod file from a path
func LoadMod(modFile string) (Mod, error) {
	var mod Mod
	if _, err := toml.DecodeFile(modFile, &mod); err != nil {
		return Mod{}, err
	}
	mod.updateData = make(map[string]interface{})
	// Horrible reflection library to convert map[string]interface to proper struct
	for k, v := range mod.Update {
		updater, ok := Updaters[k]
		if ok {
			updateData, err := updater.ParseUpdate(v)
			if err != nil {
				return mod, err
			}
			mod.updateData[k] = updateData
		} else {
			return mod, errors.New("Update plugin " + k + " not found!")
		}
	}
	mod.metaFile = modFile
	return mod, nil
}

// SetMetaPath sets the file path of a metadata file
func (m *Mod) SetMetaPath(metaFile string) string {
	m.metaFile = metaFile
	return m.metaFile
}

// Write saves the mod file, returning a hash format and the value of the hash of the saved file
func (m Mod) Write() (string, string, error) {
	f, err := os.Create(m.metaFile)
	if err != nil {
		// Attempt to create the containing directory
		err2 := os.MkdirAll(filepath.Dir(m.metaFile), os.ModePerm)
		if err2 == nil {
			f, err = os.Create(m.metaFile)
		}
		if err != nil {
			return "sha256", "", err
		}
	}

	h, err := GetHashImpl("sha256")
	if err != nil {
		_ = f.Close()
		return "", "", err
	}
	w := io.MultiWriter(h, f)

	enc := toml.NewEncoder(w)
	// Disable indentation
	enc.Indent = ""
	err = enc.Encode(m)
	hashString := h.HashToString(h.Sum(nil))
	if err != nil {
		_ = f.Close()
		return "sha256", hashString, err
	}
	return "sha256", hashString, f.Close()
}

// GetParsedUpdateData can be used to retrieve updater-specific information after parsing a mod file
func (m Mod) GetParsedUpdateData(updaterName string) (interface{}, bool) {
	upd, ok := m.updateData[updaterName]
	return upd, ok
}

// GetFilePath is a clumsy hack that I made because Mod already stores it's path anyway
func (m Mod) GetFilePath() string {
	return m.metaFile
}

// GetUpdateSource returns the name of the mod's primary update source
// (e.g., "modrinth", "curseforge", "github"). Returns "" if no update source is set.
// The primary source is determined by the [download] mode:
//   - "metadata:curseforge" → "curseforge"
//   - "url" or empty → the first non-curseforge update source,
//     since curseforge-primary mods always use metadata:curseforge mode.
func (m Mod) GetUpdateSource() string {
	// If download mode is "metadata:<source>", that source is primary
	if strings.HasPrefix(m.Download.Mode, "metadata:") {
		return strings.TrimPrefix(m.Download.Mode, "metadata:")
	}
	// For "url" mode, curseforge is never primary (those use metadata:curseforge)
	// so return the first non-curseforge source
	for k := range m.Update {
		if k != "curseforge" {
			return k
		}
	}
	// Fallback: return any source
	for k := range m.Update {
		return k
	}
	return ""
}

// GetMatchID returns the project ID for the given source if it's a non-primary
// update section (i.e., a cross-platform match). For example, a Modrinth-primary mod
// with [update.curseforge] { project-id = 123 } returns ("123", true).
// Returns ("", false) if the source is primary or not present.
func (m Mod) GetMatchID(source string) (string, bool) {
	if source == m.GetUpdateSource() {
		return "", false // This is the primary source, not a match
	}
	updateMap, ok := m.Update[source]
	if !ok {
		return "", false
	}
	switch source {
	case "modrinth":
		if mid, ok := updateMap["mod-id"]; ok {
			return fmt.Sprintf("%v", mid), true
		}
	case "curseforge":
		if pid, ok := updateMap["project-id"]; ok {
			return fmt.Sprintf("%v", pid), true
		}
	case "github":
		if slug, ok := updateMap["slug"]; ok {
			return fmt.Sprintf("%v", slug), true
		}
	}
	return "", false
}

// GetSecondarySources returns the names of all non-primary update sources.
// These represent cross-platform matches (e.g., [update.curseforge] on a
// Modrinth-primary mod, or vice versa).
func (m Mod) GetSecondarySources() []string {
	primary := m.GetUpdateSource()
	var sources []string
	for k := range m.Update {
		if k != primary {
			sources = append(sources, k)
		}
	}
	return sources
}

// SetMatchID stores a cross-platform project ID as a non-primary update section.
// The primary source is determined by [download] mode, not by a secondary flag.
// This also writes placeholder values for required fields (file-id, version)
// to ensure packwiz-installer compatibility.
func (m *Mod) SetMatchID(source, id string) {
	if m.Update == nil {
		m.Update = make(map[string]map[string]interface{})
	}
	// Preserve existing fields in the update section
	updateMap := m.Update[source]
	if updateMap == nil {
		updateMap = make(map[string]interface{})
	}
	switch source {
	case "modrinth":
		updateMap["mod-id"] = id
		if _, hasVersion := updateMap["version"]; !hasVersion {
			updateMap["version"] = ""
		}
	case "curseforge":
		if intID, err := strconv.ParseUint(id, 10, 32); err == nil {
			updateMap["project-id"] = intID
		} else {
			updateMap["project-id"] = id
		}
		if _, hasFileID := updateMap["file-id"]; !hasFileID {
			updateMap["file-id"] = 0
		}
	case "github":
		updateMap["slug"] = id
	}
	m.Update[source] = updateMap
}

// GetProjectID returns the project ID for the given source from the update data.
// Works for both primary and non-primary (match) update sections.
func (m Mod) GetProjectID(source string) (string, bool) {
	updateMap, ok := m.Update[source]
	if !ok {
		return "", false
	}
	switch source {
	case "modrinth":
		if mid, ok := updateMap["mod-id"]; ok {
			return fmt.Sprintf("%v", mid), true
		}
	case "curseforge":
		if pid, ok := updateMap["project-id"]; ok {
			return fmt.Sprintf("%v", pid), true
		}
	case "github":
		if slug, ok := updateMap["slug"]; ok {
			return fmt.Sprintf("%v", slug), true
		}
	}
	return "", false
}

// GetDestFilePath returns the path of the destination file of the mod
func (m Mod) GetDestFilePath() string {
	return filepath.Join(filepath.Dir(m.metaFile), filepath.FromSlash(m.FileName))
}

var slugifyRegex1 = regexp.MustCompile(`\(.*\)`)
var slugifyRegex2 = regexp.MustCompile(` - .+`)
var slugifyRegex3 = regexp.MustCompile(`[^a-z\d]`)
var slugifyRegex4 = regexp.MustCompile(`-+`)
var slugifyRegex5 = regexp.MustCompile(`^-|-$`)

func SlugifyName(name string) string {
	lower := strings.ToLower(name)
	noBrackets := slugifyRegex1.ReplaceAllString(lower, "")
	noSuffix := slugifyRegex2.ReplaceAllString(noBrackets, "")
	limitedChars := slugifyRegex3.ReplaceAllString(noSuffix, "-")
	noDuplicateDashes := slugifyRegex4.ReplaceAllString(limitedChars, "-")
	noLeadingTrailingDashes := slugifyRegex5.ReplaceAllString(noDuplicateDashes, "")
	return noLeadingTrailingDashes
}
