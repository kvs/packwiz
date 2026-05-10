package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// depsCmd represents the deps command
var depsCmd = &cobra.Command{
	Use:   "deps",
	Short: "Show dependency tree for mods in the modpack",
	Long: `Show the dependency tree for all mods in the modpack.

Dependencies are parsed from the mods' JAR files (from neoforge.mods.toml,
mods.toml, fabric.mod.json, or quilt.mod.json). By default, shows a
"required-by" tree: each mod is listed with the other mods in the pack
that depend on it, all under a "modpack" root. This makes it easy to see
which mods are libraries (depended on by many others) and which mods could
potentially be removed (are leaves that nothing else depends on).

Libraries are marked with "(library)".

Use --forward to show the traditional "depends-on" tree instead.

If mod JARs are not available locally, packwiz will offer to download them
to a cache directory for dependency resolution. Set deps.cache-jars in
pack.toml [options] to control this behavior (true/false/"ask").`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Loading modpack...")
		pack, err := core.LoadPack()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		index, err := pack.LoadIndex()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		forward := viper.GetBool("deps.forward")

		fmt.Println("Reading metadata files...")
		mods, err := index.LoadAllMods()
		if err != nil {
			fmt.Printf("Failed to read mods: %v\n", err)
			os.Exit(1)
		}

		if len(mods) == 0 {
			fmt.Println("No mods found in the modpack.")
			return
		}

		fmt.Printf("Found %d mods. Locating JAR files...\n", len(mods))

		// Determine if we should cache JARs
		cacheJars := shouldCacheJars()

		// Find or download JAR files
		jarPaths, missing, downloaded := core.FindAllJars(mods, pack, cacheJars)

		if downloaded > 0 {
			fmt.Printf("  Downloaded %d JARs to cache.\n", downloaded)
		}
		if missing > 0 {
			fmt.Printf("  Warning: %d mod JARs could not be located. Dependencies for these mods will be unavailable.\n", missing)
		}

		// Parse JARs for dependencies
		fmt.Println("Parsing dependencies from JAR files...")
		results, modIDLookup := core.ResolveJarDeps(mods, jarPaths)

		parsedCount := 0
		for _, r := range results {
			if r.Error == nil {
				parsedCount++
			}
		}
		fmt.Printf("  Parsed %d JARs successfully.\n", parsedCount)

		// Print tree
		if forward {
			printForwardTree(results, modIDLookup)
		} else {
			printRequiredByTree(results, modIDLookup)
		}
	},
}

// shouldCacheJars determines whether JAR files should be downloaded to cache
// when they're not available locally.
func shouldCacheJars() bool {
	setting := viper.GetString("deps.cache-jars")
	switch strings.ToLower(setting) {
	case "true", "always", "yes":
		return true
	case "false", "never", "no":
		return false
	default:
		// "ask" or unset - prompt the user
		// Check if any JARs are missing first
		return true // Default to caching; the FindAllJars function will handle this
	}
}

// countMissingJars counts how many mods don't have their JAR files available locally.
func countMissingJars(mods []*core.Mod, pack core.Pack) int {
	missing := 0
	for _, mod := range mods {
		if core.FindModJar(mod, pack) == "" {
			missing++
		}
	}
	return missing
}

// printRequiredByTree shows a tree rooted at "modpack" where each mod is listed
// with the other pack mods that depend on it. Mods that nothing depends on and
// that don't depend on anything else in the pack are standalone leaves.
func printRequiredByTree(results map[string]*core.DepResult, modIDLookup map[string]*core.Mod) {
	// Build a map: for each installed mod, which other installed mods depend on it?
	dependedOnBy := make(map[string][]string) // mod filepath -> list of mod names that depend on it

	for _, r := range results {
		if r.Mod == nil || r.Dependencies == nil {
			continue
		}
		for _, dep := range r.Dependencies {
			if !dep.Installed {
				continue
			}
			if depMod, ok := modIDLookup[dep.ModID]; ok {
				depPath := depMod.GetFilePath()
				// Don't add duplicate entries
				found := false
				for _, name := range dependedOnBy[depPath] {
					if name == r.Mod.Name {
						found = true
						break
					}
				}
				if !found {
					dependedOnBy[depPath] = append(dependedOnBy[depPath], r.Mod.Name)
				}
			}
		}
	}

	// Sort all results by name
	sortedResults := make([]*core.DepResult, 0, len(results))
	for _, r := range results {
		if r.Mod != nil {
			sortedResults = append(sortedResults, r)
		}
	}
	sort.Slice(sortedResults, func(i, j int) bool {
		return strings.ToLower(sortedResults[i].Mod.Name) < strings.ToLower(sortedResults[j].Mod.Name)
	})

	// Determine which mods have dependents
	hasDependents := make(map[string]bool)
	for depPath := range dependedOnBy {
		hasDependents[depPath] = true
	}

	// Filter to show only top-level entries:
	// - Mods that have dependents (shown with their dependents underneath)
	// - Mods that don't depend on any other pack mod AND nothing depends on them (standalone)
	var topLevel []*core.DepResult
	for _, r := range sortedResults {
		path := r.Mod.GetFilePath()

		if hasDependents[path] {
			topLevel = append(topLevel, r)
			continue
		}

		// Check if this mod depends on any other pack mod
		dependsOnPackMod := false
		if r.Dependencies != nil {
			for _, dep := range r.Dependencies {
				if dep.Installed {
					if _, ok := modIDLookup[dep.ModID]; ok {
						dependsOnPackMod = true
						break
					}
				}
			}
		}
		if !dependsOnPackMod {
			topLevel = append(topLevel, r)
		}
	}

	// Print tree rooted at "modpack"
	fmt.Println("modpack")

	for i, r := range topLevel {
		isLast := i == len(topLevel)-1
		topConnector := "├── "
		if isLast {
			topConnector = "└── "
		}

		name := r.Mod.Name
		if r.IsLibrary {
			name += " (library)"
		}
		if r.Error != nil {
			name += " [!]"
		}

		dependents := dependedOnBy[r.Mod.GetFilePath()]
		sort.Strings(dependents)

		if len(dependents) == 0 {
			// Leaf: nothing depends on this
			fmt.Printf("%s%s\n", topConnector, name)
		} else {
			fmt.Printf("%s%s\n", topConnector, name)
			prefix := "│   "
			if isLast {
				prefix = "    "
			}
			for j, depName := range dependents {
				isLastDep := j == len(dependents)-1
				depConnector := prefix + "├── "
				if isLastDep {
					depConnector = prefix + "└── "
				}
				fmt.Printf("%s%s\n", depConnector, depName)
			}
		}
	}
}

// printForwardTree shows the traditional "depends-on" tree: each root mod
// with its dependency tree below it.
func printForwardTree(results map[string]*core.DepResult, modIDLookup map[string]*core.Mod) {
	// Find root mods: mods that no other installed mod depends on
	dependedOn := make(map[string]bool) // mod filepath -> true if something depends on it
	for _, r := range results {
		if r.Dependencies == nil {
			continue
		}
		for _, dep := range r.Dependencies {
			if !dep.Installed {
				continue
			}
			if depMod, ok := modIDLookup[dep.ModID]; ok {
				dependedOn[depMod.GetFilePath()] = true
			}
		}
	}

	// Collect root mods (not depended on by any other installed mod)
	var roots []*core.DepResult
	for _, r := range results {
		if r.Mod != nil && !dependedOn[r.Mod.GetFilePath()] {
			roots = append(roots, r)
		}
	}

	// Sort roots by name
	sort.Slice(roots, func(i, j int) bool {
		return strings.ToLower(roots[i].Mod.Name) < strings.ToLower(roots[j].Mod.Name)
	})

	// Print each root as a separate tree
	expanded := make(map[string]bool)

	for i, r := range roots {
		if i > 0 {
			fmt.Println()
		}
		printForwardSubtree(r, results, modIDLookup, expanded, "")
	}
}

func printForwardSubtree(r *core.DepResult, results map[string]*core.DepResult, modIDLookup map[string]*core.Mod, expanded map[string]bool, indent string) {
	modPath := ""
	if r.Mod != nil {
		modPath = r.Mod.GetFilePath()
	}

	name := r.Mod.Name
	if r.IsLibrary {
		name += " (library)"
	}
	if r.Error != nil {
		name += " [!]"
		fmt.Printf("%s%s\n", indent, name)
		return
	}
	fmt.Printf("%s%s\n", indent, name)

	if r.Dependencies == nil || len(r.Dependencies) == 0 {
		return
	}

	// Filter to installed dependencies only
	var installedDeps []core.DepInfo
	for _, dep := range r.Dependencies {
		if dep.Installed {
			installedDeps = append(installedDeps, dep)
		}
	}

	if len(installedDeps) == 0 {
		return
	}

	// If already expanded, don't recurse
	if expanded[modPath] && modPath != "" {
		fmt.Printf("%s    [see above]\n", indent)
		return
	}
	expanded[modPath] = true

	// Sort dependencies
	sortDepInfos(installedDeps)

	for i, dep := range installedDeps {
		isLast := i == len(installedDeps)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		depName := formatDepName(dep)

		if depMod, ok := modIDLookup[dep.ModID]; ok {
			if depResult, ok := results[depMod.GetFilePath()]; ok {
				if expanded[depMod.GetFilePath()] {
					fmt.Printf("%s%s%s [see above]\n", indent, connector, depName)
					continue
				}
				// Recurse into the dependency
				childIndent := indent
				if isLast {
					childIndent += "    "
				} else {
					childIndent += "│   "
				}
				printForwardSubtree(depResult, results, modIDLookup, expanded, childIndent)
				continue
			}
		}

		fmt.Printf("%s%s%s\n", indent, connector, depName)
	}
}

func formatDepName(dep core.DepInfo) string {
	name := dep.Name
	if dep.IsLibrary {
		name += " (library)"
	}
	if dep.DepType != "" && dep.DepType != "required" {
		name += " [" + dep.DepType + "]"
	}
	return name
}

func sortDepInfos(deps []core.DepInfo) {
	typePriority := map[string]int{
		"required":     0,
		"embedded":     1,
		"optional":     2,
		"incompatible": 3,
		"tool":         4,
		"include":      5,
		"discouraged":  2,
		"unknown":      6,
	}
	sort.SliceStable(deps, func(i, j int) bool {
		pi, oki := typePriority[deps[i].DepType]
		pj, okj := typePriority[deps[j].DepType]
		if !oki {
			pi = 6
		}
		if !okj {
			pj = 6
		}
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(deps[i].Name) < strings.ToLower(deps[j].Name)
	})
}

func init() {
	rootCmd.AddCommand(depsCmd)

	depsCmd.Flags().BoolP("forward", "f", false, "Show the traditional depends-on tree (what each mod requires)")
	_ = viper.BindPFlag("deps.forward", depsCmd.Flags().Lookup("forward"))
}