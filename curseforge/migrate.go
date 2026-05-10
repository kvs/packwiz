package curseforge

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/packwiz/packwiz/core"
)

func init() {
	core.Migrators["curseforge"] = MigrateMod
}

// MigrateMod looks up a CurseForge project by ID, finds the best file for the pack,
// and returns the migration result with download info and update metadata.
func MigrateMod(projectIDStr string, pack core.Pack) (core.MigrationResult, error) {
	projectID, err := strconv.ParseUint(projectIDStr, 10, 32)
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("invalid CurseForge project ID: %s", projectIDStr)
	}

	// Get project info
	modInfoData, err := cfDefaultClient.getModInfo(uint32(projectID))
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to get CurseForge project: %w", err)
	}

	// Find the best file for this pack
	mcVersions, err := pack.GetSupportedMCVersions()
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to get game versions: %w", err)
	}

	loaders := pack.GetCompatibleLoaders()
	fileInfo, err := getLatestFile(modInfoData, mcVersions, 0, loaders)
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to find file: %w", err)
	}

	return migrateFromFile(modInfoData, fileInfo)
}

// MigrateModWithFileID migrates to CurseForge using a specific file ID.
func MigrateModWithFileID(projectIDStr string, fileIDStr string, pack core.Pack) (core.MigrationResult, error) {
	projectID, err := strconv.ParseUint(projectIDStr, 10, 32)
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("invalid CurseForge project ID: %s", projectIDStr)
	}
	fileID, err := strconv.ParseUint(fileIDStr, 10, 32)
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("invalid CurseForge file ID: %s", fileIDStr)
	}

	// Get file info
	fileInfo, err := cfDefaultClient.getFileInfo(uint32(projectID), uint32(fileID))
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to get CurseForge file: %w", err)
	}

	// Get project info for the name
	modInfoData, err := cfDefaultClient.getModInfo(uint32(projectID))
	if err != nil {
		return core.MigrationResult{}, fmt.Errorf("failed to get CurseForge project: %w", err)
	}

	return migrateFromFile(modInfoData, fileInfo)
}

func migrateFromFile(modInfoData modInfo, fileInfo modFileInfo) (core.MigrationResult, error) {
	hash, hashFormat := fileInfo.getBestHash()

	if fileInfo.DownloadURL == "" {
		return core.MigrationResult{}, errors.New("file has no download URL")
	}

	// Build update data
	updateData := map[string]interface{}{
		"project-id": modInfoData.ID,
		"file-id":    fileInfo.ID,
	}

	return core.MigrationResult{
		FileName:   fileInfo.FileName,
		URL:        fileInfo.DownloadURL,
		HashFormat: hashFormat,
		Hash:       hash,
		UpdateData: updateData,
	}, nil
}
