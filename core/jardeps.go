package core

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// JarDepData holds dependency information extracted from a mod's JAR file.
type JarDepData struct {
	// ModIDs lists the mod IDs declared in this JAR (e.g., "vista", "moonlight").
	ModIDs []string
	// DisplayName is the human-readable name from the JAR metadata.
	DisplayName string
	// Deps lists all dependencies, deduplicated by mod ID.
	Deps []JarDep
}

// JarDep represents a single dependency extracted from a JAR file.
type JarDep struct {
	ModID   string // dependency mod ID (e.g., "moonlight")
	DepType string // "required", "optional", "incompatible", "discouraged"
}

// neoforgeModsToml represents the top-level structure of a
// META-INF/neoforge.mods.toml or META-INF/mods.toml file.
type neoforgeModsToml struct {
	ModLoader     string                       `toml:"modLoader"`
	LoaderVersion string                       `toml:"loaderVersion"`
	License       string                       `toml:"license"`
	Mods          []neoforgeModEntry            `toml:"mods"`
	Dependencies  map[string][]neoforgeDepEntry `toml:"dependencies"`
	Properties    map[string]string             `toml:"properties"`
}

// neoforgeModEntry represents a [[mods]] entry.
type neoforgeModEntry struct {
	ModId       string `toml:"modId"`
	Version     string `toml:"version"`
	DisplayName string `toml:"displayName"`
	Description string `toml:"description"`
}

// neoforgeDepEntry represents a [[dependencies.<modid>]] entry.
type neoforgeDepEntry struct {
	ModId        string `toml:"modId"`
	Type         string `toml:"type"`
	VersionRange string `toml:"versionRange"`
	Ordering     string `toml:"ordering"`
	Side         string `toml:"side"`
	Reason       string `toml:"reason"`
}

// fabricModJson represents the structure of a fabric.mod.json file.
type fabricModJson struct {
	Id          string                 `json:"id"`
	Version     string                 `json:"version"`
	Name        string                 `json:"name"`
	Depends     map[string]interface{} `json:"depends"`
	Recommends  map[string]interface{} `json:"recommends"`
	Suggests    map[string]interface{} `json:"suggests"`
	Breaks      map[string]interface{} `json:"breaks"`
}

// quiltModJson represents the relevant parts of a quilt.mod.json file.
type quiltModJson struct {
	QuiltLoader quiltLoaderSection `json:"quilt_loader"`
}

type quiltLoaderSection struct {
	Id      string          `json:"id"`
	Version string          `json:"version"`
	Depends []quiltDepEntry `json:"depends"`
}

type quiltDepEntry struct {
	Id       string `json:"id"`
	Versions string `json:"versions"`
	Optional  bool   `json:"optional"`
}

// Platform dependencies that are always present and should not be shown
// in the dependency tree (they're not modpack mods).
var platformDependencies = map[string]bool{
	"minecraft":    true,
	"neoforge":     true,
	"forge":        true,
	"fml":          true,
	"fabricloader": true,
	"quiltloader":  true,
	"quilt_loader": true,
	"java":         true,
}

// ParseJarDependencies opens a mod JAR file and extracts dependency information
// from its embedded metadata files. It supports:
//   - META-INF/neoforge.mods.toml (NeoForge)
//   - META-INF/mods.toml (Forge)
//   - fabric.mod.json (Fabric)
//   - quilt.mod.json (Quilt)
func ParseJarDependencies(jarPath string) (*JarDepData, error) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open jar: %w", err)
	}
	defer r.Close()

	// Try NeoForge first, then Forge, then Fabric, then Quilt
	if data := parseModsToml(r, "META-INF/neoforge.mods.toml"); data != nil {
		return data, nil
	}
	if data := parseModsToml(r, "META-INF/mods.toml"); data != nil {
		return data, nil
	}
	if data := parseFabricModJson(r); data != nil {
		return data, nil
	}
	if data := parseQuiltModJson(r); data != nil {
		return data, nil
	}

	return nil, fmt.Errorf("no recognized mod metadata found in jar")
}

// parseModsToml reads and parses a Forge/NeoForge mods.toml file from a zip archive.
func parseModsToml(r *zip.ReadCloser, path string) *JarDepData {
	var file *zip.File
	for _, f := range r.File {
		if f.Name == path {
			file = f
			break
		}
	}
	if file == nil {
		return nil
	}

	rc, err := file.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	var tomlData neoforgeModsToml
	if _, err := toml.NewDecoder(rc).Decode(&tomlData); err != nil {
		return nil
	}

	data := &JarDepData{}

	// Extract mod IDs and display name
	for _, mod := range tomlData.Mods {
		data.ModIDs = append(data.ModIDs, mod.ModId)
		if data.DisplayName == "" && mod.DisplayName != "" {
			data.DisplayName = mod.DisplayName
		}
	}

	// Collect all dependencies across all mods in the JAR
	depMap := make(map[string]string) // modID -> depType (strongest type wins)
	depOrder := []string{}            // preserve insertion order

	for _, deps := range tomlData.Dependencies {
		for _, dep := range deps {
			// Skip platform dependencies
			if platformDependencies[strings.ToLower(dep.ModId)] {
				continue
			}

			depType := strings.ToLower(dep.Type)
			if depType == "" {
				depType = "required" // default
			}

			existing, exists := depMap[dep.ModId]
			if !exists {
				depMap[dep.ModId] = depType
				depOrder = append(depOrder, dep.ModId)
			} else {
				// Keep the strongest dependency type
				if depTypePriority(depType) > depTypePriority(existing) {
					depMap[dep.ModId] = depType
				}
			}
		}
	}

	for _, modID := range depOrder {
		data.Deps = append(data.Deps, JarDep{
			ModID:   modID,
			DepType: depMap[modID],
		})
	}

	return data
}

// parseFabricModJson reads and parses a fabric.mod.json file from a zip archive.
func parseFabricModJson(r *zip.ReadCloser) *JarDepData {
	var file *zip.File
	for _, f := range r.File {
		if f.Name == "fabric.mod.json" {
			file = f
			break
		}
	}
	if file == nil {
		return nil
	}

	rc, err := file.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	var fabricMod fabricModJson
	if err := json.NewDecoder(rc).Decode(&fabricMod); err != nil {
		return nil
	}

	data := &JarDepData{
		ModIDs:      []string{fabricMod.Id},
		DisplayName: fabricMod.Name,
	}

	depMap := make(map[string]string)
	depOrder := []string{}

	// Process depends (required), recommends (optional), suggests (optional), breaks (incompatible)
	extractFabricDeps(fabricMod.Depends, "required", &depMap, &depOrder)
	extractFabricDeps(fabricMod.Recommends, "optional", &depMap, &depOrder)
	extractFabricDeps(fabricMod.Suggests, "optional", &depMap, &depOrder)
	extractFabricDeps(fabricMod.Breaks, "incompatible", &depMap, &depOrder)

	for _, modID := range depOrder {
		data.Deps = append(data.Deps, JarDep{
			ModID:   modID,
			DepType: depMap[modID],
		})
	}

	return data
}

// extractFabricDeps extracts dependency mod IDs from a Fabric dependency map.
func extractFabricDeps(deps map[string]interface{}, depType string, depMap *map[string]string, depOrder *[]string) {
	if deps == nil {
		return
	}
	for modID := range deps {
		modIDLower := strings.ToLower(modID)
		if platformDependencies[modIDLower] {
			continue
		}
		if _, exists := (*depMap)[modIDLower]; !exists {
			(*depMap)[modIDLower] = depType
			*depOrder = append(*depOrder, modIDLower)
		}
	}
}

// parseQuiltModJson reads and parses a quilt.mod.json file from a zip archive.
func parseQuiltModJson(r *zip.ReadCloser) *JarDepData {
	var file *zip.File
	for _, f := range r.File {
		if f.Name == "quilt.mod.json" {
			file = f
			break
		}
	}
	if file == nil {
		return nil
	}

	rc, err := file.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	var quiltMod quiltModJson
	if err := json.NewDecoder(rc).Decode(&quiltMod); err != nil {
		return nil
	}

	data := &JarDepData{
		ModIDs:      []string{quiltMod.QuiltLoader.Id},
		DisplayName: quiltMod.QuiltLoader.Id, // Quilt mod JSON doesn't have a display name in quilt_loader
	}

	depMap := make(map[string]string)
	depOrder := []string{}

	for _, dep := range quiltMod.QuiltLoader.Depends {
		modIDLower := strings.ToLower(dep.Id)
		if platformDependencies[modIDLower] {
			continue
		}
		depType := "required"
		if dep.Optional {
			depType = "optional"
		}
		if _, exists := depMap[modIDLower]; !exists {
			depMap[modIDLower] = depType
			depOrder = append(depOrder, modIDLower)
		} else {
			if depTypePriority(depType) > depTypePriority(depMap[modIDLower]) {
				depMap[modIDLower] = depType
			}
		}
	}

	for _, modID := range depOrder {
		data.Deps = append(data.Deps, JarDep{
			ModID:   modID,
			DepType: depMap[modID],
		})
	}

	return data
}

// depTypePriority returns a numeric priority for dependency types.
// Higher priority wins when the same mod appears as both required and optional.
func depTypePriority(depType string) int {
	switch depType {
	case "required":
		return 3
	case "optional":
		return 1
	case "incompatible":
		return 4 // incompatible is the "strongest" type
	case "discouraged":
		return 2
	default:
		return 0
	}
}

// isLikelyLibrary uses heuristics to determine if a mod name suggests it's a library/API mod.
func isLikelyLibrary(name string) bool {
	lower := strings.ToLower(name)
	libraryPatterns := []string{"lib", "library", "api", "framework", "platform", "core"}
	for _, pattern := range libraryPatterns {
		// Match as whole word or suffix (e.g., "Moonlight Lib", "Fabric API")
		if strings.Contains(lower, pattern) {
			// Check it's a word boundary - the character before pattern should be
			// a space, dash, underscore, or start of string
			idx := strings.Index(lower, pattern)
			if idx == 0 || lower[idx-1] == ' ' || lower[idx-1] == '-' || lower[idx-1] == '_' {
				return true
			}
			// Or the pattern is at the end of the name
			if idx+len(pattern) == len(lower) {
				return true
			}
		}
	}
	return false
}

// FindModJar locates a mod's JAR file by checking:
// 1. The installed location (alongside the .pw.toml)
// 2. The mirror/data directory (if the mod is mirrored)
// 3. The download cache (where packwiz refresh puts files)
// 4. The deps cache (where packwiz deps caches downloaded JARs)
// 5. The deps cache by filename (fallback if hash-based path doesn't match)
//
// Returns the path to the JAR file, or an empty string if not found.
func FindModJar(mod *Mod, pack Pack) string {
	// 1. Check installed location
	if path := mod.GetDestFilePath(); fileExists(path) {
		return path
	}

	// 2. Check mirror/data directory
	if mod.Mirror {
		dataPath := filepath.Join(pack.GetDataDir(), filepath.FromSlash(mod.FileName))
		if fileExists(dataPath) {
			return dataPath
		}
	}

	// 3. Check the download cache (where packwiz refresh stores files)
	if path := findInDownloadCache(mod); fileExists(path) {
		return path
	}

	// 4. Check deps cache by hash-based path
	if path := cachedJarPath(mod); path != "" && fileExists(path) {
		return path
	}

	// 5. Check deps cache by filename (fallback for hash mismatches)
	cacheDir, err := GetDepsCacheDir()
	if err == nil && cacheDir != "" {
		byName := filepath.Join(cacheDir, filepath.FromSlash(mod.FileName))
		if fileExists(byName) {
			return byName
		}
	}

	return ""
}

// findInDownloadCache looks up a mod's JAR in the packwiz download cache
// using the hash from the mod's metadata.
func findInDownloadCache(mod *Mod) string {
	cachePath, err := GetPackwizCache()
	if err != nil {
		return ""
	}

	// Load the cache index
	indexData, err := os.ReadFile(filepath.Join(cachePath, "index.json"))
	if err != nil {
		return ""
	}

	var cacheIndex CacheIndex
	if err := json.Unmarshal(indexData, &cacheIndex); err != nil {
		return ""
	}
	cacheIndex.cachePath = cachePath

	// Try to find the file using the mod's hash
	hashFormat := mod.Download.HashFormat
	if hashFormat == "" {
		hashFormat = "sha256"
	}
	hash := mod.Download.Hash
	if hash == "" {
		return ""
	}

	handle := cacheIndex.GetHandleFromHash(hashFormat, hash)
	if handle == nil {
		// Try with sha256 since the cache always stores sha256
		if hashFormat != "sha256" {
			handle = cacheIndex.GetHandleFromHash("sha256", hash)
			if handle == nil {
				return ""
			}
		} else {
			return ""
		}
	}

	// Verify the file actually exists on disk
	path := handle.Path()
	if fileExists(path) {
		return path
	}

	return ""
}

// DownloadModJar downloads a mod's JAR file to the deps cache directory.
// Returns the path to the cached JAR file.
func DownloadModJar(mod *Mod) (string, error) {
	// Determine download URL
	url := mod.Download.URL
	if url == "" {
		return "", fmt.Errorf("mod %s has no download URL", mod.Name)
	}

	// Check deps cache first
	cachePath := cachedJarPath(mod)
	if fileExists(cachePath) {
		return cachePath, nil
	}

	// Download the JAR
	resp, err := GetWithUA(url, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", mod.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: HTTP %d", mod.Name, resp.StatusCode)
	}

	// Create deps cache directory
	cacheDir, err := GetDepsCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to create deps cache directory: %w", err)
	}

	// Create a temp file in the cache dir, then rename
	tmpFile, err := os.CreateTemp(cacheDir, "download-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Download and optionally verify hash
	hasher, err := GetHashImpl(mod.Download.HashFormat)
	if err != nil {
		// Fall back to sha256 if hash format is unknown
		hasher, err = GetHashImpl("sha256")
		if err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return "", fmt.Errorf("failed to create hasher: %w", err)
		}
	}

	w := io.MultiWriter(tmpFile, hasher)
	if _, err := io.Copy(w, resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to download %s: %w", mod.Name, err)
	}
	tmpFile.Close()

	// Verify hash if available
	if mod.Download.Hash != "" {
		calculatedHash := hasher.HashToString(hasher.Sum(nil))
		if !strings.EqualFold(calculatedHash, mod.Download.Hash) {
			os.Remove(tmpPath)
			return "", fmt.Errorf("hash mismatch for %s: expected %s, got %s", mod.Name, mod.Download.Hash, calculatedHash)
		}
	}

	// Rename to final cache path
	if err := os.Rename(tmpPath, cachePath); err != nil {
		// On cross-device rename, copy instead
		if err := copyFile(tmpPath, cachePath); err != nil {
			os.Remove(tmpPath)
			os.Remove(cachePath)
			return "", fmt.Errorf("failed to cache %s: %w", mod.Name, err)
		}
		os.Remove(tmpPath)
	}

	// Also save by filename for fallback lookups
	namePath := filepath.Join(cacheDir, filepath.FromSlash(mod.FileName))
	if namePath != cachePath {
		_ = os.Remove(namePath)
		if err := os.Link(cachePath, namePath); err != nil {
			_ = copyFile(cachePath, namePath)
		}
	}

	return cachePath, nil
}

// cachedJarPath returns the expected cache path for a mod's JAR file.
func cachedJarPath(mod *Mod) string {
	cacheDir, err := GetDepsCacheDir()
	if err != nil {
		return ""
	}
	// Use hash format and hash as the filename for uniqueness
	hashStr := mod.Download.Hash
	if hashStr == "" {
		// Fall back to slugified filename
		hashStr = SlugifyName(mod.Name)
	}
	// Truncate to avoid filesystem issues; keep first 32 chars
	if len(hashStr) > 32 {
		hashStr = hashStr[:32]
	}
	// Sanitize for filesystem
	hashStr = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, strings.ToLower(hashStr))

	ext := filepath.Ext(mod.FileName)
	if ext == "" {
		ext = ".jar"
	}
	return filepath.Join(cacheDir, hashStr+ext)
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// ResolveJarDeps resolves dependencies for all mods by parsing their JAR files.
// It returns:
//   - results: map of mod filepath -> DepResult for mods whose JARs were successfully parsed
//   - modIDLookup: map of mod ID (lowercase) -> pack mod info for matching dependencies to installed mods
func ResolveJarDeps(mods []*Mod, jarPaths map[string]string) (results map[string]*DepResult, modIDLookup map[string]*Mod) {
	results = make(map[string]*DepResult)
	modIDLookup = make(map[string]*Mod)

	// First pass: parse all JARs and build the mod ID -> mod mapping
	type jarInfo struct {
		mod     *Mod
		jarData *JarDepData
	}
	var jarInfos []jarInfo

	for _, mod := range mods {
		jarPath, ok := jarPaths[mod.GetFilePath()]
		if !ok {
			// No JAR available for this mod; register fallback identifiers
			// so other mods' dependencies can still match it
			modIDLookup[strings.ToLower(mod.Name)] = mod
			modIDLookup[SlugifyName(mod.Name)] = mod
			// Also register the filename stem (e.g. "octolib-0.6.0" -> "octolib")
			stem := strings.ToLower(strings.SplitN(mod.FileName, "-", 2)[0])
			if stem != "" && stem != strings.ToLower(mod.Name) {
				if _, exists := modIDLookup[stem]; !exists {
					modIDLookup[stem] = mod
				}
			}
			continue
		}

		jarData, err := ParseJarDependencies(jarPath)
		if err != nil {
			// Can't parse this JAR; register fallback identifiers
			modIDLookup[strings.ToLower(mod.Name)] = mod
			modIDLookup[SlugifyName(mod.Name)] = mod
			stem := strings.ToLower(strings.SplitN(mod.FileName, "-", 2)[0])
			if stem != "" && stem != strings.ToLower(mod.Name) {
				if _, exists := modIDLookup[stem]; !exists {
					modIDLookup[stem] = mod
				}
			}
			results[mod.GetFilePath()] = &DepResult{
				Mod:          mod,
				Dependencies: nil,
				Error:        fmt.Errorf("failed to parse JAR: %w", err),
			}
			continue
		}

		jarInfos = append(jarInfos, jarInfo{mod: mod, jarData: jarData})

		// Register mod IDs in the lookup
		for _, modID := range jarData.ModIDs {
			modIDLookup[strings.ToLower(modID)] = mod
		}
		// Also register fallback identifiers from the mod metadata
		modIDLookup[strings.ToLower(mod.Name)] = mod
		modIDLookup[SlugifyName(mod.Name)] = mod
	}

	// Second pass: resolve dependencies using the mod ID lookup
	for _, info := range jarInfos {
		mod := info.mod
		jarData := info.jarData

		// Convert JAR dependencies to DepInfo
		var deps []DepInfo
		for _, dep := range jarData.Deps {
			depInfo := DepInfo{
				Name:      dep.ModID, // Will be updated below if the dep is in the pack
				ModID:     strings.ToLower(dep.ModID),
				DepType:   dep.DepType,
				Installed: false,
			}

			// Check if this dependency is installed in the pack
			if depMod, ok := modIDLookup[strings.ToLower(dep.ModID)]; ok {
				depInfo.Installed = true
				// Use the display name from the dep's JAR data
				depInfo.Name = depMod.Name
			}

			deps = append(deps, depInfo)
		}

		// Use the display name from the JAR as the mod name if available
		modName := mod.Name
		if jarData.DisplayName != "" {
			modName = jarData.DisplayName
		}

		results[mod.GetFilePath()] = &DepResult{
			Mod:          mod,
			Dependencies: deps,
			IsLibrary:    isLikelyLibrary(modName),
		}
	}

	return results, modIDLookup
}

// FindAllJars locates JAR files for all mods, optionally downloading them to cache.
// It returns:
//   - jarPaths: map of mod filepath -> JAR path for mods where JARs were found or downloaded
//   - missingCount: number of mods whose JARs could not be found or downloaded
//   - downloadCount: number of mods whose JARs were downloaded to cache
func FindAllJars(mods []*Mod, pack Pack, cacheJars bool) (jarPaths map[string]string, missingCount int, downloadCount int) {
	jarPaths = make(map[string]string)
	missingCount = 0
	downloadCount = 0

	// Try metadata-based downloads first (e.g. metadata:curseforge)
	// These need to be resolved through the MetaDownloader to get actual file content.
	var metaMods []*Mod // mods that need metadata resolution
	var urlMods []*Mod  // mods that have a direct URL

	for _, mod := range mods {
		jarPath := FindModJar(mod, pack)
		if jarPath != "" {
			jarPaths[mod.GetFilePath()] = jarPath
			continue
		}

		// JAR not found locally - categorize by download mode
		if strings.HasPrefix(mod.Download.Mode, "metadata:") {
			metaMods = append(metaMods, mod)
		} else if mod.Download.URL != "" {
			urlMods = append(urlMods, mod)
		} else {
			// No URL and no metadata - can't download
			fmt.Printf("  %s: no download URL available\n", mod.Name)
			missingCount++
		}
	}

	// Download URL-based mods
	if len(urlMods) > 0 && cacheJars {
		fmt.Printf("Downloading %d mod JARs...\n", len(urlMods))
		for i, mod := range urlMods {
			fmt.Printf("  [%d/%d] %s\n", i+1, len(urlMods), mod.Name)
			cachedPath, err := DownloadModJar(mod)
			if err != nil {
				fmt.Printf("  Warning: failed to download %s: %v\n", mod.Name, err)
				missingCount++
			} else {
				jarPaths[mod.GetFilePath()] = cachedPath
				downloadCount++
			}
		}
	}

	// Resolve metadata-based mods through their respective downloaders
	if len(metaMods) > 0 && cacheJars {
		fmt.Printf("Resolving %d metadata-based mods...\n", len(metaMods))
		// Group by metadata type
		metaGroups := make(map[string][]*Mod)
		for _, mod := range metaMods {
			dlID := strings.TrimPrefix(mod.Download.Mode, "metadata:")
			metaGroups[dlID] = append(metaGroups[dlID], mod)
		}

		for dlID, group := range metaGroups {
			downloader, ok := MetaDownloaders[dlID]
			if !ok {
				fmt.Printf("  Warning: unknown metadata downloader %q for %d mods\n", dlID, len(group))
				missingCount += len(group)
				continue
			}

			fmt.Printf("  Resolving %d mods via %s...\n", len(group), dlID)
			meta, err := downloader.GetFilesMetadata(group)
			if err != nil {
				fmt.Printf("  Warning: failed to get metadata from %s: %v\n", dlID, err)
				missingCount += len(group)
				continue
			}

			for i, mod := range group {
				isManual, manualDL := meta[i].GetManualDownload()
				if isManual {
					// Try to find the file locally before asking the user
					localPath := findManualDownload(mod, manualDL)
					if localPath != "" {
						jarPaths[mod.GetFilePath()] = localPath
						downloadCount++
						continue
					}
					// Not found locally — print the URL for the user
					fmt.Printf("    %s: manual download required\n", mod.Name)
					fmt.Printf("      Visit %s to download %s\n", manualDL.URL, manualDL.FileName)
					fmt.Printf("      Then re-run packwiz deps to pick it up.\n")
					missingCount++
					continue
				}

				fmt.Printf("    [%d/%d] %s\n", i+1, len(group), mod.Name)

				// Use the metadata downloader to get the file content
				reader, err := meta[i].DownloadFile()
				if err != nil {
					fmt.Printf("    Warning: failed to download %s: %v\n", mod.Name, err)
					missingCount++
					continue
				}

				// Save to deps cache
				cachePath, err := saveToDepsCache(mod, reader)
				reader.Close()
				if err != nil {
					fmt.Printf("    Warning: failed to cache %s: %v\n", mod.Name, err)
					missingCount++
					continue
				}

				jarPaths[mod.GetFilePath()] = cachePath
				downloadCount++
			}
		}
	} else if len(metaMods) > 0 && !cacheJars {
		missingCount += len(metaMods)
	}

	return jarPaths, missingCount, downloadCount
}

// saveToDepsCache saves a downloaded JAR to the deps cache.
// It saves the file using both the hash-based path (for exact matching) and
// the original filename (for fallback lookup when hash paths differ).
func saveToDepsCache(mod *Mod, content io.ReadCloser) (string, error) {
	cacheDir, err := GetDepsCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get deps cache dir: %w", err)
	}

	cachePath := cachedJarPath(mod)

	// Create a temp file in the cache dir, then write and rename
	tmpFile, err := os.CreateTemp(cacheDir, "download-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	_, err = io.Copy(tmpFile, content)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write cache file: %w", err)
	}

	// Rename to final hash-based cache path
	if err := os.Rename(tmpPath, cachePath); err != nil {
		// On cross-device rename, copy instead
		if err := copyFile(tmpPath, cachePath); err != nil {
			os.Remove(tmpPath)
			os.Remove(cachePath)
			return "", fmt.Errorf("failed to cache file: %w", err)
		}
		os.Remove(tmpPath)
	}

	// Also create a symlink/hardlink by filename for fallback lookups.
	// This ensures FindModJar can find the file by mod.FileName even if
	// the hash-based path doesn't match on subsequent runs.
	namePath := filepath.Join(cacheDir, filepath.FromSlash(mod.FileName))
	if namePath != cachePath {
		_ = os.Remove(namePath) // Remove existing link/file if any
		// Try hard link first (cheaper than copy)
		if err := os.Link(cachePath, namePath); err != nil {
			// Fall back to copy if hard link fails (e.g. cross-device)
			if err := copyFile(cachePath, namePath); err != nil {
				// Non-fatal: the hash-based path still works
			}
		}
	}

	return cachePath, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findManualDownload looks for a manually-downloaded JAR file for mods that
// require manual download (e.g. CurseForge mods with no distribution permission).
// It checks:
// 1. The deps cache (already processed)
// 2. The download cache import folder (~/.cache/packwiz/cache/import/)
// 3. The current working directory
// If found, the file is copied to the deps cache and the cache path is returned.
// If not found, returns empty string.
func findManualDownload(mod *Mod, manualDL ManualDownload) string {
	cacheDir, _ := GetDepsCacheDir()
	downloadCache, _ := GetPackwizCache()

	// Build a list of candidate filenames to search for
	candidates := []string{filepath.FromSlash(mod.FileName)}
	if manualDL.FileName != "" && manualDL.FileName != mod.FileName {
		candidates = append(candidates, filepath.FromSlash(manualDL.FileName))
	}

	searchDirs := []string{}
	if cacheDir != "" {
		searchDirs = append(searchDirs, cacheDir)
	}
	if downloadCache != "" {
		searchDirs = append(searchDirs, filepath.Join(downloadCache, DownloadCacheImportFolder))
	}
	// Current working directory (where packwiz runs)
	searchDirs = append(searchDirs, ".")

	for _, candidate := range candidates {
		for _, dir := range searchDirs {
			p := filepath.Join(dir, candidate)
			if fileExists(p) {
				// Found it — copy to deps cache if not already there
				destPath := filepath.Join(cacheDir, filepath.FromSlash(mod.FileName))
				if p == destPath {
					return destPath
				}
				if cacheDir == "" {
					return p
				}
				if err := copyFile(p, destPath); err != nil {
					// If copy fails, just use the original path
					return p
				}
				return destPath
			}
		}
	}

	return ""
}