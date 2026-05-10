package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rehomeCmd represents the rehome command
var rehomeCmd = &cobra.Command{
	Use:   "rehome <mod> <target-source>",
	Short: "Switch a mod's update source to a different platform",
	Long: `Switch a mod from one update source to another, using its stored
cross-platform match. For example, switch a CurseForge mod to
Modrinth, or vice versa.

Before switching, use 'packwiz match' to store a cross-platform ID.

The mod's download URL, hash, and update metadata will be updated to
use the new source. The [download] mode will be updated accordingly.

Examples:
  packwiz match my-mod           # First, match the mod
  packwiz rehome my-mod modrinth # Then switch to Modrinth`,
	Args: cobra.ExactArgs(2),
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
		targetSource := args[1]

		modPath, ok := index.FindMod(modName)
		if !ok {
			fmt.Printf("Can't find mod \"%s\"; please ensure you have run packwiz refresh and use the name of the .pw.toml file\n", modName)
			os.Exit(1)
		}
		mod, err := core.LoadMod(modPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		currentSource := mod.GetUpdateSource()
		if currentSource == "" {
			fmt.Printf("Mod \"%s\" has no update source\n", mod.Name)
			os.Exit(1)
		}

		if currentSource == targetSource {
			fmt.Printf("Mod \"%s\" is already on source \"%s\"\n", mod.Name, targetSource)
			os.Exit(1)
		}

		// Check if the mod has a match for the target source
		matchID, hasMatch := mod.GetMatchID(targetSource)
		if !hasMatch || matchID == "" {
			fmt.Printf("Mod \"%s\" doesn't have a stored %s match. Run 'packwiz match %s' first.\n", mod.Name, targetSource, modName)
			os.Exit(1)
		}

		// Look up the target source's migrator
		migrator, ok := core.Migrators[targetSource]
		if !ok {
			fmt.Printf("Migration to source \"%s\" is not supported (no migrator registered)\n", targetSource)
			fmt.Printf("Supported targets: %s\n", strings.Join(getMigratorKeys(), ", "))
			os.Exit(1)
		}

		fmt.Printf("Migrating \"%s\" from %s to %s (match ID: %s)...\n", mod.Name, currentSource, targetSource, matchID)

		// Run the migrator to get download info and update data
		result, err := migrator(matchID, pack)
		if err != nil {
			fmt.Printf("Failed to migrate: %v\n", err)
			os.Exit(1)
		}

		// Confirm the migration
		fmt.Printf("  New file: %s\n", result.FileName)
		if !viper.GetBool("non-interactive") {
			if !cmdshared.PromptYesNo("Continue with migration? [Y/n]: ") {
				fmt.Println("Migration cancelled!")
				return
			}
		}

		// Update the mod metadata with the new source's data
		// The [download] mode will be set based on the new primary source
		mod.Update[targetSource] = result.UpdateData
		mod.FileName = result.FileName
		mod.Download = core.ModDownload{
			URL:        result.URL,
			HashFormat: result.HashFormat,
			Hash:       result.Hash,
		}

		// Set download mode based on new primary source
		switch targetSource {
		case "curseforge":
			mod.Download.Mode = core.ModeCF
		default:
			mod.Download.Mode = core.ModeURL
		}

		// Clear URL if download mode is metadata-based
		if strings.HasPrefix(mod.Download.Mode, "metadata:") {
			mod.Download.URL = ""
		}

		_, _, err = mod.Write()
		if err != nil {
			fmt.Printf("Error writing mod file: %v\n", err)
			os.Exit(1)
		}

		// Update the index
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

		fmt.Printf("Successfully migrated \"%s\" from %s to %s\n", mod.Name, currentSource, targetSource)
	},
}

func getMigratorKeys() []string {
	keys := make([]string, 0, len(core.Migrators))
	for k := range core.Migrators {
		keys = append(keys, k)
	}
	return keys
}

func init() {
	rootCmd.AddCommand(rehomeCmd)
}
