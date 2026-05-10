package github

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/mitchellh/mapstructure"
	"github.com/packwiz/packwiz/core"
)

type ghUpdateData struct {
	Slug   string `mapstructure:"slug"`
	Tag    string `mapstructure:"tag"`
	Branch string `mapstructure:"branch"`
	Regex  string `mapstructure:"regex"`
}

type ghUpdater struct{}

func (u ghUpdater) ParseUpdate(updateUnparsed map[string]interface{}) (interface{}, error) {
	var updateData ghUpdateData
	err := mapstructure.Decode(updateUnparsed, &updateData)
	return updateData, err
}

type cachedStateStore struct {
	Slug    string
	Release Release
}

func (u ghUpdater) CheckUpdate(mods []*core.Mod, pack core.Pack) ([]core.UpdateCheck, error) {
	results := make([]core.UpdateCheck, len(mods))

	for i, mod := range mods {
		rawData, ok := mod.GetParsedUpdateData("github")
		if !ok {
			results[i] = core.UpdateCheck{Error: errors.New("failed to parse update metadata")}
			continue
		}

		data := rawData.(ghUpdateData)

		newRelease, err := getLatestRelease(data.Slug, data.Branch)
		if err != nil {
			results[i] = core.UpdateCheck{Error: fmt.Errorf("failed to get latest release: %v", err)}
			continue
		}

		if newRelease.TagName == data.Tag { // The latest release is the same as the installed one
			results[i] = core.UpdateCheck{UpdateAvailable: false}
			continue
		}

		expr := regexp2.MustCompile(data.Regex, 0)

		if len(newRelease.Assets) == 0 {
			results[i] = core.UpdateCheck{Error: errors.New("new release doesn't have any assets")}
			continue
		}

		var newFiles []Asset

		for _, v := range newRelease.Assets {
			bl, _ := expr.MatchString(v.Name)
			if bl {
				newFiles = append(newFiles, v)
			}
		}

		if len(newFiles) == 0 {
			results[i] = core.UpdateCheck{Error: errors.New("release doesn't have any assets matching regex")}
			continue
		}

		if len(newFiles) > 1 {
			// TODO: also print file names
			results[i] = core.UpdateCheck{Error: errors.New("release has more than one asset matching regex")}
			continue
		}

		newFile := newFiles[0]

	// Build cumulative changelog from all releases between installed tag and latest
	changelog := buildGitHubChangelog(data.Slug, data.Tag, newRelease)

	results[i] = core.UpdateCheck{
		UpdateAvailable: true,
		UpdateString:    mod.FileName + " -> " + newFile.Name,
		Changelog:       changelog,
		CachedState:     cachedStateStore{data.Slug, newRelease},
		NewVersionID:    newRelease.TagName,
	}
	}

	return results, nil
}

// buildGitHubChangelog constructs a cumulative changelog from all GitHub releases
// between oldTag and newRelease. If no cumulative changelog can be built, falls
// back to the new release's body.
func buildGitHubChangelog(slug, oldTag string, newRelease Release) string {
	owner, repo, found := strings.Cut(slug, "/")
	if !found {
		return newRelease.Body
	}

	releases, err := FetchAllReleaseChangelogs(owner, repo, 30)
	if err != nil || len(releases) == 0 {
		return newRelease.Body
	}

	// Find the index of the currently installed tag
	oldIdx := -1
	for i, r := range releases {
		if r.TagName == oldTag {
			oldIdx = i
			break
		}
	}

	if oldIdx == -1 {
		return newRelease.Body
	}

	if oldIdx == 0 {
		return ""
	}

	// Collect releases from 0 (newest) to oldIdx (installed version),
	// skipping the installed version itself.
	var parts []string
	for i := 0; i < oldIdx; i++ {
		body := strings.TrimSpace(releases[i].Body)
		if body != "" {
			parts = append(parts, fmt.Sprintf("--- %s ---\n%s", releases[i].TagName, body))
		}
	}

	if len(parts) == 0 {
		return newRelease.Body
	}

	return strings.Join(parts, "\n\n")
}

func (u ghUpdater) DoUpdate(mods []*core.Mod, cachedState []interface{}) error {
	for i, mod := range mods {
		modState := cachedState[i].(cachedStateStore)
		var release = modState.Release

		// yes, this is duplicated - i guess we should just cache asset + tag instead of entire release...?
		var file = release.Assets[0]
		for _, v := range release.Assets {
			if strings.HasSuffix(v.Name, ".jar") {
				file = v
			}
		}

		hash, err := file.getSha256()
		if err != nil {
			return err
		}

		mod.FileName = file.Name
		mod.Download = core.ModDownload{
			URL:        file.BrowserDownloadURL,
			HashFormat: "sha256",
			Hash:       hash,
		}
		mod.Update["github"]["tag"] = release.TagName
	}

	return nil
}
