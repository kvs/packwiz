package modrinth

import (
	"errors"
	"fmt"

	modrinthApi "codeberg.org/jmansfield/go-modrinth/modrinth"
	"github.com/packwiz/packwiz/core"
)

func init() {
	core.Migrators["modrinth"] = MigrateMod
}

// MigrateMod looks up a Modrinth project by ID, finds the best version for the pack,
// and returns the migration result with download info and update metadata.
func MigrateMod(projectID string, pack core.Pack) (core.MigrationResult, error) {
	// Get project info for name
	project, err := mrDefaultClient.Projects.Get(projectID)
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to get Modrinth project: %w", err)
	}

	if project.Title == nil {
		return core.MigrationResult{}, errors.New("project has no title")
	}

	// Find the best version for this pack
	version, err := getLatestVersion(projectID, *project.Title, pack)
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to find version: %w", err)
	}

	return migrateFromVersion(projectID, version)
}

// MigrateModWithVersion migrates a mod to Modrinth using a specific version ID.
func MigrateModWithVersion(projectID string, versionID string, pack core.Pack) (core.MigrationResult, error) {
	version, err := mrDefaultClient.Versions.Get(versionID)
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to get Modrinth version: %w", err)
	}

	return migrateFromVersion(projectID, version)
}

func migrateFromVersion(projectID string, version *modrinthApi.Version) (core.MigrationResult, error) {
	// Select the primary file
	if len(version.Files) == 0 {
		return core.MigrationResult{}, errors.New("version has no files")
	}
	file := version.Files[0]
	for _, v := range version.Files {
		if v.Primary != nil && *v.Primary {
			file = v
			break
		}
	}

	if file.Filename == nil || file.URL == nil {
		return core.MigrationResult{}, errors.New("file missing filename or URL")
	}

	algorithm, hash := getBestHash(file)
	if algorithm == "" {
		return core.MigrationResult{}, errors.New("file doesn't have a valid hash")
	}

	// Build update data
	updateData := map[string]interface{}{
		"mod-id":  projectID,
		"version": version.ID,
	}

	return core.MigrationResult{
		FileName:   *file.Filename,
		URL:        *file.URL,
		HashFormat: algorithm,
		Hash:       hash,
		UpdateData: updateData,
	}, nil
}

// ResolveModrinthProjectID resolves any Modrinth project identifier (ID or slug) to the canonical project ID.
func ResolveModrinthProjectID(idOrSlug string) (string, error) {
	project, err := mrDefaultClient.Projects.Get(idOrSlug)
	if err != nil {
		return "", fmt.Errorf("failed to resolve Modrinth project: %w", err)
	}
	if project.ID == nil {
		return "", errors.New("project has no ID")
	}
	return *project.ID, nil
}
