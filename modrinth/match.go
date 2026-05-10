package modrinth

import (
	"fmt"

	"github.com/packwiz/packwiz/core"
)

func init() {
	core.MatchSearchers["modrinth"] = SearchMatchCandidates
}

// SearchMatchCandidates searches Modrinth for projects matching the given name,
// returning candidates for cross-platform matching.
func SearchMatchCandidates(name string, pack core.Pack) ([]core.MatchCandidate, error) {
	mcVersions, err := pack.GetSupportedMCVersions()
	if err != nil {
		return nil, fmt.Errorf("failed to get supported MC versions: %w", err)
	}

	results, err := GetProjectIdsViaSearch(name, mcVersions)
	if err != nil {
		return nil, fmt.Errorf("failed to search Modrinth: %w", err)
	}

	candidates := make([]core.MatchCandidate, 0, len(results))
	for _, result := range results {
		id := ""
		if result.ProjectID != nil {
			id = *result.ProjectID
		}
		title := name
		if result.Title != nil {
			title = *result.Title
		}
		desc := ""
		if result.Description != nil {
			desc = *result.Description
		}
		candidates = append(candidates, core.MatchCandidate{
			Name:    title,
			ID:      id,
			Summary: desc,
		})
	}
	return candidates, nil
}

// GetProjectLookup retrieves project info by project ID.
// Returns (title, slug, error).
func GetProjectLookup(projectID string) (string, string, error) {
	project, err := mrDefaultClient.Projects.Get(projectID)
	if err != nil {
		return "", "", fmt.Errorf("failed to get Modrinth project: %w", err)
	}
	title := ""
	if project.Title != nil {
		title = *project.Title
	}
	slug := ""
	if project.Slug != nil {
		slug = *project.Slug
	}
	return title, slug, nil
}
