package curseforge

import (
	"fmt"
	"strconv"

	"github.com/packwiz/packwiz/core"
)

func init() {
	core.MatchSearchers["curseforge"] = SearchMatchCandidates
}

// SearchMatchCandidates searches CurseForge for mods matching the given name,
// returning candidates for cross-platform matching.
func SearchMatchCandidates(name string, pack core.Pack) ([]core.MatchCandidate, error) {
	mcVersions, err := pack.GetSupportedMCVersions()
	if err != nil {
		return nil, fmt.Errorf("failed to get supported MC versions: %w", err)
	}
	mcVersion := ""
	if len(mcVersions) > 0 {
		mcVersion = mcVersions[0]
	}

	loaderType := getSearchLoaderType(pack)
	results, err := cfDefaultClient.getSearch(name, "", 432, 0, 0, mcVersion, loaderType)
	if err != nil {
		return nil, fmt.Errorf("failed to search CurseForge: %w", err)
	}

	candidates := make([]core.MatchCandidate, 0, len(results))
	for _, result := range results {
		candidates = append(candidates, core.MatchCandidate{
			Name:    result.Name,
			ID:      strconv.FormatUint(uint64(result.ID), 10),
			Summary: result.Summary,
		})
	}
	return candidates, nil
}

// GetModInfoLookup retrieves mod info by project ID string.
// This is a convenience wrapper for cross-platform matching.
func GetModInfoLookup(projectID string) (string, string, error) {
	id, err := strconv.ParseUint(projectID, 10, 32)
	if err != nil {
		return "", "", fmt.Errorf("invalid CurseForge project ID: %s", projectID)
	}
	info, err := cfDefaultClient.getModInfo(uint32(id))
	if err != nil {
		return "", "", fmt.Errorf("failed to get CurseForge project info: %w", err)
	}
	return info.Name, info.Slug, nil
}
