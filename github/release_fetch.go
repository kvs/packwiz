package github

import (
	"encoding/json"
	"fmt"
	"io"
)

// GitHubReleaseInfo contains the tag name and body of a GitHub release.
type GitHubReleaseInfo struct {
	TagName string
	Body    string
}

// FetchAllReleaseChangelogs fetches all release changelogs from a GitHub repository.
// It returns up to maxReleases recent releases (newest first).
// owner is the GitHub user/org, repo is the repository name.
func FetchAllReleaseChangelogs(owner, repo string, maxReleases int) ([]GitHubReleaseInfo, error) {
	slug := fmt.Sprintf("%s/%s", owner, repo)
	resp, err := ghDefaultClient.getReleases(slug)
	if err != nil {
		return nil, fmt.Errorf("failed to get releases for %s: %w", slug, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get releases for %s: HTTP %d", slug, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response for %s: %w", slug, err)
	}

	var releases []Release
	if err := json.Unmarshal(bodyBytes, &releases); err != nil {
		return nil, fmt.Errorf("failed to parse releases for %s: %w", slug, err)
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found for %s", slug)
	}

	if maxReleases > len(releases) {
		maxReleases = len(releases)
	}

	result := make([]GitHubReleaseInfo, 0, maxReleases)
	for i := 0; i < maxReleases; i++ {
		result = append(result, GitHubReleaseInfo{
			TagName: releases[i].TagName,
			Body:    releases[i].Body,
		})
	}

	return result, nil
}
