package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// UpdateCmd represents the update command
var UpdateCmd = &cobra.Command{
	Use:     "update [name...]",
	Short:   "Update one or more external files (or all external files) in the modpack",
	Aliases: []string{"upgrade"},
	Args:    cobra.MinimumNArgs(0),
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

		dryRun := viper.GetBool("update.dry-run")
		verbose := viper.GetBool("update.verbose")

		if viper.GetBool("update.all") {
			doUpdateAll(pack, &index, dryRun, verbose)
		} else if len(args) == 0 {
			fmt.Println("Must specify one or more mods, or use the --all flag!")
			os.Exit(1)
		} else if len(args) == 1 {
			doUpdateSingleMod(pack, &index, args[0], dryRun, verbose)
		} else {
			doUpdateMods(pack, &index, args, dryRun, verbose)
		}

		err = index.Write()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = pack.UpdateIndexHash()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = pack.Write()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

func doUpdateAll(pack core.Pack, index *core.Index, dryRun bool, verbose bool) {
	filesWithUpdater := make(map[string][]*core.Mod)
	fmt.Println("Reading metadata files...")
	mods, err := index.LoadAllMods()
	if err != nil {
		fmt.Printf("Failed to update all files: %v\n", err)
		os.Exit(1)
	}
	for _, modData := range mods {
		source := modData.GetUpdateSource()
		if source == "" {
			fmt.Printf("A supported update system for \"%s\" cannot be found.\n", modData.Name)
			continue
		}
		if _, ok := core.Updaters[source]; !ok {
			fmt.Printf("A supported update system for \"%s\" cannot be found.\n", modData.Name)
			continue
		}
		filesWithUpdater[source] = append(filesWithUpdater[source], modData)
	}

	fmt.Println("Checking for updates...")
	updatesFound := false
	updatableFiles := make(map[string][]*core.Mod)
	updaterCachedStateMap := make(map[string][]interface{})
	for k, v := range filesWithUpdater {
		checks, err := core.Updaters[k].CheckUpdate(v, pack)
		if err != nil {
			// TODO: do we return err code 1?
			fmt.Printf("Failed to check updates for %s: %s\n", k, err.Error())
			continue
		}
		for i, check := range checks {
			if check.Error != nil {
				// TODO: do we return err code 1?
				fmt.Printf("Failed to check updates for %s: %s\n", v[i].Name, check.Error.Error())
				continue
			}
			if check.UpdateAvailable {
				if v[i].Pin {
					fmt.Printf("Update skipped for pinned mod %s\n", v[i].Name)
					continue
				}

				// Check if the new version is in the skip list
				skipped := false
				if check.NewVersionID != "" {
					for _, sv := range v[i].SkipVersions {
						if sv == check.NewVersionID {
							fmt.Printf("Update skipped for %s (version %s is in skip list)\n", v[i].Name, check.NewVersionID)
							skipped = true
							break
						}
					}
				}
				if skipped {
					continue
				}

				if !updatesFound {
					fmt.Println("Updates found:")
					updatesFound = true
				}
				if dryRun {
					slug := getModSlug(v[i])
					fmt.Printf("  %s (%s): %s\n", v[i].Name, slug, check.UpdateString)
				} else {
					fmt.Printf("%s: %s\n", v[i].Name, check.UpdateString)
				}
				if verbose {
					printChangelog(check.Changelog)
				}
				updatableFiles[k] = append(updatableFiles[k], v[i])
				updaterCachedStateMap[k] = append(updaterCachedStateMap[k], check.CachedState)
			}
		}
	}

	if !updatesFound {
		fmt.Println("All files are up to date!")
		return
	}

	if dryRun {
		fmt.Println("\n(dry run: no changes were made)")
		return
	}

	if !cmdshared.PromptYesNo("Do you want to update? [Y/n]: ") {
		fmt.Println("Cancelled!")
		return
	}

	applyUpdates(updatableFiles, updaterCachedStateMap, index)
	fmt.Println("Files updated!")
}

// doUpdateSingleMod handles updating a single named mod. This preserves the
// original single-mod UX: no confirmation prompt, and exits immediately on failure.
func doUpdateSingleMod(pack core.Pack, index *core.Index, modName string, dryRun bool, verbose bool) {
	modPath, ok := index.FindMod(modName)
	if !ok {
		fmt.Println("Can't find this file; please ensure you have run packwiz refresh and use the name of the .pw.toml file (defaults to the project slug)")
		os.Exit(1)
	}
	modData, err := core.LoadMod(modPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if modData.Pin {
		fmt.Println("Version is pinned; run the unpin command to allow updating")
		os.Exit(1)
	}

	source := modData.GetUpdateSource()
	if source == "" {
		fmt.Println("A supported update system for \"" + modData.Name + "\" cannot be found.")
		os.Exit(1)
	}
	updater, ok := core.Updaters[source]
	if !ok {
		fmt.Println("A supported update system for \"" + modData.Name + "\" cannot be found.")
		os.Exit(1)
	}

	check, err := updater.CheckUpdate([]*core.Mod{&modData}, pack)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if len(check) != 1 {
		fmt.Println("Invalid update check response")
		os.Exit(1)
	}

	if check[0].UpdateAvailable {
		// Check if the new version is in the skip list
		skipped := false
		if check[0].NewVersionID != "" {
			for _, sv := range modData.SkipVersions {
				if sv == check[0].NewVersionID {
					fmt.Printf("Update skipped for %s (version %s is in skip list)\n", modData.Name, check[0].NewVersionID)
					skipped = true
					break
				}
			}
		}
		if skipped {
			fmt.Printf("\"%s\" is up to date (update version is in skip list)!\n", modData.Name)
			return
		}
		if dryRun {
			fmt.Printf("Update available for %s (%s): %s\n", modData.Name, modName, check[0].UpdateString)
		} else {
			fmt.Printf("Update available: %s\n", check[0].UpdateString)
		}
		if verbose {
			printChangelog(check[0].Changelog)
		}

		if dryRun {
			fmt.Println("\n(dry run: no changes were made)")
			return
		}

		err = updater.DoUpdate([]*core.Mod{&modData}, []interface{}{check[0].CachedState})
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		format, hash, err := modData.Write()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = index.RefreshFileWithHash(modPath, format, hash, true)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("\"%s\" is already up to date!\n", modData.Name)
		return
	}

	fmt.Printf("\"%s\" updated!\n", modData.Name)
}

// doUpdateMods handles updating multiple named mods. It batches them by updater
// for efficiency, shows a confirmation prompt, and applies all updates together.
func doUpdateMods(pack core.Pack, index *core.Index, args []string, dryRun bool, verbose bool) {
	// Resolve all named mods upfront so we can batch them by updater
	var namedMods []*core.Mod
	var slugs []string
	for _, arg := range args {
		modPath, ok := index.FindMod(arg)
		if !ok {
			fmt.Printf("Can't find mod \"%s\"; please ensure you have run packwiz refresh and use the name of the .pw.toml file (defaults to the project slug)\n", arg)
			os.Exit(1)
		}
		modData, err := core.LoadMod(modPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if modData.Pin {
			fmt.Printf("Version is pinned for \"%s\"; run the unpin command to allow updating\n", arg)
			os.Exit(1)
		}
		namedMods = append(namedMods, &modData)
		slugs = append(slugs, arg)
	}

	// Group mods by update source for batch checking
	filesWithUpdater := make(map[string][]*core.Mod)
	modSlugs := make(map[string][]string)
	for i, modData := range namedMods {
		source := modData.GetUpdateSource()
		if source == "" {
			fmt.Printf("A supported update system for \"%s\" cannot be found.\n", modData.Name)
			os.Exit(1)
		}
		if _, ok := core.Updaters[source]; !ok {
			fmt.Printf("A supported update system for \"%s\" cannot be found.\n", modData.Name)
			os.Exit(1)
		}
		filesWithUpdater[source] = append(filesWithUpdater[source], modData)
		modSlugs[source] = append(modSlugs[source], slugs[i])
	}

	// Check for updates, grouped by updater
	updatesFound := false
	updatableFiles := make(map[string][]*core.Mod)
	updaterCachedStateMap := make(map[string][]interface{})
	for k, v := range filesWithUpdater {
		checks, err := core.Updaters[k].CheckUpdate(v, pack)
		if err != nil {
			fmt.Printf("Failed to check updates: %v\n", err)
			os.Exit(1)
		}
		if len(checks) != len(v) {
			fmt.Printf("Invalid update check response\n")
			os.Exit(1)
		}
		for i, check := range checks {
			if check.Error != nil {
				fmt.Printf("Failed to check updates for %s: %s\n", v[i].Name, check.Error.Error())
				continue
			}
			if check.UpdateAvailable {
				// Check if the new version is in the skip list
				skipped := false
				if check.NewVersionID != "" {
					for _, sv := range v[i].SkipVersions {
						if sv == check.NewVersionID {
							fmt.Printf("Update skipped for %s (version %s is in skip list)\n", v[i].Name, check.NewVersionID)
							skipped = true
							break
						}
					}
				}
				if skipped {
					continue
				}
				if dryRun {
					fmt.Printf("Update available for %s (%s): %s\n", v[i].Name, modSlugs[k][i], check.UpdateString)
				} else {
					fmt.Printf("Update available for %s: %s\n", v[i].Name, check.UpdateString)
				}
				if verbose {
					printChangelog(check.Changelog)
				}
				updatesFound = true
				updatableFiles[k] = append(updatableFiles[k], v[i])
				updaterCachedStateMap[k] = append(updaterCachedStateMap[k], check.CachedState)
			} else {
				fmt.Printf("\"%s\" is already up to date!\n", v[i].Name)
			}
		}
	}

	if !updatesFound {
		return
	}

	if dryRun {
		fmt.Println("\n(dry run: no changes were made)")
		return
	}

	if !cmdshared.PromptYesNo("Do you want to update? [Y/n]: ") {
		fmt.Println("Cancelled!")
		return
	}

	applyUpdates(updatableFiles, updaterCachedStateMap, index)

	for _, v := range updatableFiles {
		for _, modData := range v {
			fmt.Printf("\"%s\" updated!\n", modData.Name)
		}
	}
}

func applyUpdates(updatableFiles map[string][]*core.Mod, updaterCachedStateMap map[string][]interface{}, index *core.Index) {
	for k, v := range updatableFiles {
		err := core.Updaters[k].DoUpdate(v, updaterCachedStateMap[k])
		if err != nil {
			fmt.Println(err.Error())
			continue
		}
		for _, modData := range v {
			format, hash, err := modData.Write()
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			err = index.RefreshFileWithHash(modData.GetFilePath(), format, hash, true)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
		}
	}
}

func getModSlug(mod *core.Mod) string {
	_, fileName := filepath.Split(mod.GetFilePath())
	return strings.TrimSuffix(fileName, core.MetaExtension)
}

func printChangelog(changelog string) {
	if changelog == "" {
		fmt.Println("    (no changelog available)")
		return
	}
	// Print changelog indented, with each line indented
	lines := strings.Split(changelog, "\n")
	for _, line := range lines {
		fmt.Println("    " + line)
	}
	fmt.Println()
}

func init() {
	rootCmd.AddCommand(UpdateCmd)

	UpdateCmd.Flags().BoolP("all", "a", false, "Update all external files")
	_ = viper.BindPFlag("update.all", UpdateCmd.Flags().Lookup("all"))
	UpdateCmd.Flags().BoolP("dry-run", "n", false, "Check for updates without applying them")
	_ = viper.BindPFlag("update.dry-run", UpdateCmd.Flags().Lookup("dry-run"))
	UpdateCmd.Flags().BoolP("verbose", "v", false, "Show changelogs for available updates")
	_ = viper.BindPFlag("update.verbose", UpdateCmd.Flags().Lookup("verbose"))
}