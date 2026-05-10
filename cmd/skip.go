package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// skipCmd represents the skip command
var skipCmd = &cobra.Command{
	Use:   "skip <mod> [<version>]",
	Short: "Skip specific versions of a mod during updates",
	Long: `Add version(s) to a mod's skip list. When checking for updates,
skipped versions are ignored, so you can skip beta/alpha releases
without having to pin the mod to a specific version.

If no version is specified, the currently available update version is skipped.
Use --remove to un-skip a previously skipped version.

Examples:
  packwiz skip my-mod                    # Skip the latest available version of my-mod
  packwiz skip my-mod 1.5.0-beta.1       # Skip a specific version
  packwiz skip --remove my-mod 1.5.0-beta.1  # Remove a version from the skip list`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
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

		modName := args[0]
		modPath, ok := index.FindMod(modName)
		if !ok {
			fmt.Printf("Can't find mod \"%s\"; please ensure you have run packwiz refresh and use the name of the .pw.toml file (defaults to the project slug)\n", modName)
			os.Exit(1)
		}
		mod, err := core.LoadMod(modPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		removeMode, _ := cmd.Flags().GetBool("remove")

		if removeMode {
			if len(args) < 2 {
				fmt.Println("You must specify a version ID to remove from the skip list")
				os.Exit(1)
			}
			versionsToRemove := args[1:]
			removedCount := 0
			for _, v := range versionsToRemove {
				for i, sv := range mod.SkipVersions {
					if sv == v {
						mod.SkipVersions = append(mod.SkipVersions[:i], mod.SkipVersions[i+1:]...)
						removedCount++
						break
					}
				}
			}
			if removedCount == 0 {
				fmt.Printf("No matching versions found in skip list for \"%s\"\n", mod.Name)
				return
			}
			fmt.Printf("Removed %d version(s) from skip list for \"%s\"\n", removedCount, mod.Name)
		} else {
			// Add mode
			var versions []string
			if len(args) >= 2 {
				// Version(s) specified on command line
				versions = args[1:]
			} else {
				// No version specified: find the latest available version and skip it
				version, err := findLatestUpdateVersion(&mod, pack)
				if err != nil {
					fmt.Printf("Error finding latest version: %v\n", err)
					os.Exit(1)
				}
				if version == "" {
					fmt.Printf("\"%s\" is already up to date; nothing to skip\n", mod.Name)
					return
				}
				versions = []string{version}
			}

			// Add versions to skip list (avoid duplicates)
			existing := make(map[string]bool)
			for _, sv := range mod.SkipVersions {
				existing[sv] = true
			}
			addedCount := 0
			var newVersions []string
			for _, v := range versions {
				if !existing[v] {
					mod.SkipVersions = append(mod.SkipVersions, v)
					existing[v] = true
					newVersions = append(newVersions, v)
					addedCount++
				}
			}

			if addedCount == 0 {
				fmt.Printf("All specified versions are already in the skip list for \"%s\"\n", mod.Name)
				return
			}
			fmt.Printf("Added %d version(s) to skip list for \"%s\": %s\n", addedCount, mod.Name, strings.Join(newVersions, ", "))
		}

		_, _, err = mod.Write()
		if err != nil {
			fmt.Printf("Error writing mod file: %v\n", err)
			os.Exit(1)
		}
		err = index.RefreshFileWithHash(modPath, "sha256", "", true)
		if err != nil {
			fmt.Printf("Error refreshing index: %v\n", err)
			os.Exit(1)
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

// findLatestUpdateVersion finds the version ID of the latest available update for a mod.
// Returns empty string if the mod is up to date.
func findLatestUpdateVersion(mod *core.Mod, pack core.Pack) (string, error) {
	for source := range mod.Update {
		updater, ok := core.Updaters[source]
		if !ok {
			continue
		}
		checks, err := updater.CheckUpdate([]*core.Mod{mod}, pack)
		if err != nil {
			return "", fmt.Errorf("failed to check updates for %s: %w", source, err)
		}
		if len(checks) > 0 && checks[0].UpdateAvailable && checks[0].NewVersionID != "" {
			return checks[0].NewVersionID, nil
		}
	}
	return "", nil
}

func init() {
	rootCmd.AddCommand(skipCmd)

	skipCmd.Flags().BoolP("remove", "r", false, "Remove a version from the skip list instead of adding it")
	_ = viper.BindPFlag("skip.remove", skipCmd.Flags().Lookup("remove"))
}
