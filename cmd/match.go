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

// matchCmd represents the match command
var matchCmd = &cobra.Command{
	Use:   "match [mod]",
	Short: "Match mods to their cross-platform equivalents",
	Long: `Find and store cross-platform project IDs for mods.
For example, match a CurseForge mod to its Modrinth equivalent,
or vice versa. This enables better dependency resolution and
changelog lookup.

Matches are stored as non-primary [update.*] sections in the mod's
.pw.toml file. The primary source is determined by [download] mode:
"metadata:curseforge" means CurseForge is primary; "url" means
Modrinth or GitHub is primary.

If a mod name is provided, only match that mod.
If --all is provided, match all mods that don't already have
a cross-platform match (auto-accept first search result).

Use --remove to remove a stored match.

Examples:
  packwiz match my-mod          # Interactively match a specific mod
  packwiz match --all           # Auto-match all mods (accepts first search result)
  packwiz match --remove my-mod # Remove stored cross-platform match`,
	Args: cobra.MaximumNArgs(1),
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

		removeMode, _ := cmd.Flags().GetBool("remove")
		allMode, _ := cmd.Flags().GetBool("all")

		if removeMode {
			if len(args) < 1 {
				fmt.Println("You must specify a mod to remove the match from")
				os.Exit(1)
			}
			removeMatch(args[0], pack, &index)
			return
		}

		mods, err := index.LoadAllMods()
		if err != nil {
			fmt.Printf("Failed to read mods: %v\n", err)
			os.Exit(1)
		}

		var toMatch []*core.Mod
		if len(args) == 1 {
			// Match a specific mod
			modPath, ok := index.FindMod(args[0])
			if !ok {
				fmt.Printf("Can't find mod \"%s\"; please ensure you have run packwiz refresh and use the name of the .pw.toml file\n", args[0])
				os.Exit(1)
			}
			mod, err := core.LoadMod(modPath)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			toMatch = []*core.Mod{&mod}
		} else {
			// Find mods needing matches
			for _, mod := range mods {
				source := mod.GetUpdateSource()
				if source == "" {
					continue
				}
				// Check if already has a match for the opposite platform
				if alreadyHasCrossMatch(mod) {
					continue
				}
				toMatch = append(toMatch, mod)
			}
			if len(toMatch) == 0 {
				fmt.Println("All mods already have cross-platform matches!")
				return
			}
		}

		autoAccept := allMode || viper.GetBool("non-interactive")

		for _, mod := range toMatch {
			err := matchMod(mod, pack, &index, autoAccept)
			if err != nil {
				fmt.Printf("Error matching \"%s\": %v\n", mod.Name, err)
			}
		}
	},
}

// alreadyHasCrossMatch checks if a mod already has a non-primary update source
// (i.e., a cross-platform match stored in a second [update.*] section).
func alreadyHasCrossMatch(mod *core.Mod) bool {
	secondaries := mod.GetSecondarySources()
	return len(secondaries) > 0
}

// getOppositeSources returns the sources that this mod should be matched against.
// For a CurseForge mod, that's "modrinth". For a Modrinth mod, that's "curseforge".
func getOppositeSources(source string) []string {
	switch source {
	case "curseforge":
		return []string{"modrinth"}
	case "modrinth":
		return []string{"curseforge"}
	default:
		return nil
	}
}

func removeMatch(modName string, pack core.Pack, index *core.Index) {
	modPath, ok := index.FindMod(modName)
	if !ok {
		fmt.Printf("Can't find mod \"%s\"\n", modName)
		os.Exit(1)
	}
	mod, err := core.LoadMod(modPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	secondaries := mod.GetSecondarySources()
	if len(secondaries) == 0 {
		fmt.Printf("No cross-platform matches stored for \"%s\"\n", mod.Name)
		return
	}

	fmt.Printf("Removing cross-platform matches for \"%s\":\n", mod.Name)
	for _, source := range secondaries {
		id, _ := mod.GetMatchID(source)
		fmt.Printf("  %s: %s\n", source, id)
	}

	if !cmdshared.PromptYesNo("Are you sure? [Y/n]: ") {
		fmt.Println("Cancelled!")
		return
	}

	// Remove all non-primary update sections
	for _, source := range secondaries {
		delete(mod.Update, source)
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
	fmt.Printf("Removed cross-platform matches for \"%s\"\n", mod.Name)
}

func matchMod(mod *core.Mod, pack core.Pack, index *core.Index, autoAccept bool) error {
	source := mod.GetUpdateSource()
	if source == "" {
		fmt.Printf("Skipping \"%s\": no update source\n", mod.Name)
		return nil
	}

	oppositeSources := getOppositeSources(source)
	if len(oppositeSources) == 0 {
		fmt.Printf("Skipping \"%s\": cross-platform matching not supported for source \"%s\"\n", mod.Name, source)
		return nil
	}

	// Try each opposite source
	for _, targetSource := range oppositeSources {
		searcher, ok := core.MatchSearchers[targetSource]
		if !ok {
			fmt.Printf("  No searcher registered for \"%s\"\n", targetSource)
			continue
		}

		// Check if already matched
		if id, ok := mod.GetMatchID(targetSource); ok {
			fmt.Printf("  \"%s\" already has a %s match: %s\n", mod.Name, targetSource, id)
			continue
		}

		fmt.Printf("Matching \"%s\" (%s) → %s...\n", mod.Name, source, targetSource)

		candidates, err := searcher(mod.Name, pack)
		if err != nil {
			return fmt.Errorf("failed to search %s: %w", targetSource, err)
		}
		if len(candidates) == 0 {
			fmt.Printf("  No %s match found for \"%s\"\n", targetSource, mod.Name)
			continue
		}

		if autoAccept {
			// Auto-accept first result
			saveMatch(mod, targetSource, candidates[0].ID, index, pack)
			fmt.Printf("  Matched \"%s\" → %s \"%s\" (%s)\n", mod.Name, targetSource, candidates[0].Name, candidates[0].ID)
			continue
		}

		// Interactive selection
		selectedID := selectMatch(mod.Name, targetSource, candidates)
		if selectedID == "" {
			fmt.Printf("  Skipped matching for \"%s\"\n", mod.Name)
			continue
		}

		saveMatch(mod, targetSource, selectedID, index, pack)
	}

	return nil
}

func selectMatch(modName string, targetSource string, candidates []core.MatchCandidate) string {
	if len(candidates) == 0 {
		return ""
	}

	fmt.Printf("Select a %s match for \"%s\":\n", targetSource, modName)
	for i, c := range candidates {
		desc := ""
		if c.Summary != "" {
			// Truncate long descriptions
			desc = c.Summary
			if len(desc) > 80 {
				desc = desc[:77] + "..."
			}
			desc = " - " + desc
		}
		fmt.Printf("  %d) %s%s [%s]\n", i+1, c.Name, desc, c.ID)
	}
	fmt.Printf("  0) Skip (no match)\n")

	for {
		fmt.Printf("Enter choice (0-%d): ", len(candidates))
		var choice int
		_, err := fmt.Scanln(&choice)
		if err != nil {
			// Try reading as string for "skip"
			var line string
			fmt.Scanln(&line)
			if strings.ToLower(line) == "s" || strings.ToLower(line) == "skip" {
				return ""
			}
			fmt.Println("Invalid choice, try again")
			continue
		}
		if choice == 0 {
			return ""
		}
		if choice < 1 || choice > len(candidates) {
			fmt.Println("Invalid choice, try again")
			continue
		}
		return candidates[choice-1].ID
	}
}

func saveMatch(mod *core.Mod, source string, id string, index *core.Index, pack core.Pack) {
	mod.SetMatchID(source, id)
	_, _, err := mod.Write()
	if err != nil {
		fmt.Printf("  Error writing mod file: %v\n", err)
		return
	}
	err = index.RefreshFileWithHash(mod.GetFilePath(), "sha256", "", true)
	if err != nil {
		fmt.Printf("  Error refreshing index: %v\n", err)
		return
	}
	err = index.Write()
	if err != nil {
		fmt.Println(err)
		return
	}
	err = pack.UpdateIndexHash()
	if err != nil {
		fmt.Println(err)
		return
	}
	err = pack.Write()
	if err != nil {
		fmt.Println(err)
		return
	}
}

func init() {
	rootCmd.AddCommand(matchCmd)

	matchCmd.Flags().BoolP("all", "a", false, "Match all mods that don't already have cross-platform matches (auto-accept first result)")
	matchCmd.Flags().BoolP("remove", "r", false, "Remove stored cross-platform match for a mod")
	_ = viper.BindPFlag("match.all", matchCmd.Flags().Lookup("all"))
	_ = viper.BindPFlag("match.remove", matchCmd.Flags().Lookup("remove"))
}
