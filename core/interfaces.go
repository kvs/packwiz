package core

import "io"

// Updaters stores all the updaters that packwiz can use. Add your own update systems to this map, keyed by the configuration name.
var Updaters = make(map[string]Updater)

// Updater is used to process updates on mods
type Updater interface {
	// ParseUpdate takes an unparsed interface{} (as a map[string]interface{}), and returns an Updater for a mod file.
	// This can be done using the mapstructure library or your own parsing methods.
	ParseUpdate(map[string]interface{}) (interface{}, error)
	// CheckUpdate checks whether there is an update for each of the mods in the given slice,
	// called for all of the mods that this updater handles
	CheckUpdate([]*Mod, Pack) ([]UpdateCheck, error)
	// DoUpdate carries out the update previously queried in CheckUpdate, on each Mod's metadata,
	// given pointers to Mods and the value of CachedState for each mod
	DoUpdate([]*Mod, []interface{}) error
}

// UpdateCheck represents the data returned from CheckUpdate for each mod
type UpdateCheck struct {
	// UpdateAvailable is true if an update is available for this mod
	UpdateAvailable bool
	// UpdateString is a string that details the update in some way to the user. Usually this will be in the form of
	// a version change (1.0.0 -> 1.0.1), or a file name change (thanos-skin-1.0.0.jar -> thanos-skin-1.0.1.jar).
	UpdateString string
	// Changelog contains the changelog for the update, if available.
	Changelog string
	// CachedState can be used to preserve per-mod state between CheckUpdate and DoUpdate (e.g. file metadata)
	CachedState interface{}
	// NewVersionID stores the version identifier of the new version for skip-version tracking.
	// For Modrinth: the version ID. For CurseForge: the file ID. For GitHub: the tag name.
	NewVersionID string
	// Error stores an error for this specific mod
	// Errors can also be returned from CheckUpdate directly, if the whole operation failed completely (so only 1 error is printed)
	// If an error is returned for a mod, or from CheckUpdate, DoUpdate is not called on that mod / at all
	Error error
}

// MetaDownloaders stores all the metadata-based installers that packwiz can use. Add your own downloaders to this map, keyed by the source name.
var MetaDownloaders = make(map[string]MetaDownloader)

// DepInfo represents a dependency of a mod, as parsed from its JAR file metadata.
type DepInfo struct {
	// Name is a human-readable name for the dependency, if known
	Name string
	// ModID is the mod ID of the dependency (e.g., "moonlight", "jei")
	ModID string
	// DepType is the dependency type, e.g. "required", "optional", "incompatible", "discouraged"
	DepType string
	// Installed is true when the dependency is present in the pack
	Installed bool
	// IsLibrary is true when the dependency appears to be a library/API mod (based on name heuristics)
	IsLibrary bool
}

// DepResult holds the dependency resolution result for a single mod
type DepResult struct {
	// Mod is the mod this result is for
	Mod *Mod
	// Dependencies lists the dependencies found for this mod
	Dependencies []DepInfo
	// IsLibrary is true when the mod itself appears to be a library/API mod
	IsLibrary bool
	// Error is set when dependency resolution failed for this mod
	Error error
}

// MatchCandidate represents a potential cross-platform match for a mod.
type MatchCandidate struct {
	// Name is the human-readable project name
	Name string
	// ID is the platform-specific project identifier (Modrinth project ID, CurseForge mod ID string, etc.)
	ID string
	// Summary is a short description of the project
	Summary string
}

// MatchSearcher searches a platform for mods by name, returning potential matches.
// Implementations are registered by updater packages (modrinth, curseforge) in init().
type MatchSearcher func(name string, pack Pack) ([]MatchCandidate, error)

// MatchSearchers stores registered match searchers for cross-platform matching.
// For example, "modrinth" and "curseforge" are keys.
var MatchSearchers = make(map[string]MatchSearcher)

// MigrationResult holds the data needed to migrate a mod to a different source.
type MigrationResult struct {
	// FileName is the new file name for the mod
	FileName string
	// URL is the download URL for the new file
	URL string
	// HashFormat is the hash algorithm used (e.g., "sha1", "sha512")
	HashFormat string
	// Hash is the hash value of the new file
	Hash string
	// UpdateData is the update metadata for the new source (e.g., {"project-id": ..., "file-id": ...})
	UpdateData map[string]interface{}
}

// Migrator migrates a mod to this source using a project ID.
// It finds the best version for the pack, downloads the file, and returns metadata.
type Migrator func(projectID string, pack Pack) (MigrationResult, error)

// Migrators stores registered migrators for each source.
// When migrating a mod from one source to another, the target source's migrator is used.
var Migrators = make(map[string]Migrator)

// MetaDownloader specifies a downloader for a Mod using a "metadata:source" mode
// The calling code should handle caching and hash validation.
type MetaDownloader interface {
	GetFilesMetadata([]*Mod) ([]MetaDownloaderData, error)
}

// MetaDownloaderData specifies the per-Mod metadata retrieved for downloading
type MetaDownloaderData interface {
	GetManualDownload() (bool, ManualDownload)
	DownloadFile() (io.ReadCloser, error)
}

type ManualDownload struct {
	Name     string
	FileName string
	URL      string
}