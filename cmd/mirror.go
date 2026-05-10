package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var mirrorCmd = &cobra.Command{
	Use:   "mirror <mod-name>",
	Short: "Mirror a mod's JAR file into the pack data directory",
	Long: `Mirror a mod's JAR file into the pack's data directory.

When a mod is mirrored, its JAR file is copied to .packwiz-data/ (by
default) and tracked in the index. If a base URL is configured (via
"packwiz settings base-url"), the mod's download URL is updated to
point to the data directory file under that base URL, making the
modpack self-contained -- the installer downloads mirrored JARs
directly from your server instead of external sites.

To set the base URL:
  packwiz settings base-url https://example.com/pack/

WARNING: Not all mods allow redistribution. Mirroring mod JARs in your
modpack may violate the mod's license. Only mirror mods that you have
permission to redistribute, or use this feature strictly for private
server modpacks.`,
	Args: cobra.ExactArgs(1),
	Run:  runMirror,
}

var mirrorUndoCmd = &cobra.Command{
	Use:   "unmirror <mod-name>",
	Short: "Remove a mod's mirror status and data directory file",
	Long: `Remove a mod's mirror status, deleting the data directory file and
restoring the original download URL. The mod will be downloaded from
its original source URL on next install/update.`,
	Args: cobra.ExactArgs(1),
	Run:  runUnmirror,
}

func init() {
	rootCmd.AddCommand(mirrorCmd)
	mirrorCmd.AddCommand(mirrorUndoCmd)
}

// getBaseURL returns the pack's configured base URL from pack options.
// Returns empty string if not set.
func getBaseURL(pack *core.Pack) string {
	if pack.Options == nil {
		return ""
	}
	url, ok := pack.Options["base-url"].(string)
	if !ok {
		return ""
	}
	return url
}

func runMirror(cmd *cobra.Command, args []string) {
	modName := args[0]

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

	// Find the mod
	modPath, found := index.FindMod(modName)
	if !found {
		fmt.Printf("Mod %q not found in the modpack.\n", modName)
		os.Exit(1)
	}

	mod, err := core.LoadMod(modPath)
	if err != nil {
		fmt.Printf("Failed to load mod %q: %v\n", modName, err)
		os.Exit(1)
	}

	if mod.Mirror {
		fmt.Printf("Mod %q is already mirrored.\n", mod.Name)
		return
	}

	// Print redistribution warning
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  ⚠  REDISTRIBUTION WARNING  ⚠                                ║")
	fmt.Println("║                                                              ║")
	fmt.Println("║  Not all mods allow redistribution. Mirroring a mod's JAR    ║")
	fmt.Println("║  in your modpack may violate the mod's license. Only         ║")
	fmt.Println("║  mirror mods that you have permission to redistribute, or    ║")
	fmt.Println("║  use this feature strictly for private server modpacks.      ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	if !cmdshared.PromptYesNo("Are you sure you want to mirror this mod? [Y/n]: ") {
		fmt.Println("Mirror cancelled.")
		return
	}

	// Find or download the JAR
	jarPath := core.FindModJar(&mod, pack)
	if jarPath == "" {
		if mod.Download.URL == "" && mod.Download.Mode != core.ModeCF && mod.Download.Mode != core.ModeMirror {
			fmt.Printf("Mod %q has no download URL and JAR is not available locally.\n", mod.Name)
			fmt.Println("Try running \"packwiz refresh\" first to download mod files, or check that the mod has a valid download URL.")
			os.Exit(1)
		}
		fmt.Printf("Downloading %s...\n", mod.Name)
		jarPath, err = core.DownloadModJar(&mod)
		if err != nil {
			fmt.Printf("Failed to download %s: %v\n", mod.Name, err)
			os.Exit(1)
		}
	}

	// Create data directory
	dataDir := pack.GetDataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Printf("Failed to create data directory: %v\n", err)
		os.Exit(1)
	}

	// Copy JAR to data directory
	destPath := filepath.Join(dataDir, filepath.FromSlash(mod.FileName))
	if err := copyJARFile(jarPath, destPath); err != nil {
		fmt.Printf("Failed to copy JAR to data directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Copied JAR to %s\n", destPath)

	// Calculate hash of the data file for the index
	hashFormat, hash, err := core.HashFile(destPath)
	if err != nil {
		fmt.Printf("Failed to hash data file: %v\n", err)
		os.Exit(1)
	}

	// Update the mod: mark as mirrored
	mod.Mirror = true

	// Save original download URL and mode so unmirror can restore them
	mod.Download.MirrorURL = mod.Download.URL
	mod.Download.MirrorMode = mod.Download.Mode

	// If a base URL is configured, update the download URL to point to the mirrored file
	baseURL := getBaseURL(&pack)
	if baseURL != "" {
		packRoot := filepath.Dir(viper.GetString("pack-file"))
		relPath, err := filepath.Rel(packRoot, destPath)
		if err != nil {
			fmt.Printf("Failed to compute relative path: %v\n", err)
			os.Exit(1)
		}
		mod.Download.URL = strings.TrimRight(baseURL, "/") + "/" + filepath.ToSlash(relPath)
		mod.Download.Mode = core.ModeURL
		fmt.Printf("Download URL set to: %s\n", mod.Download.URL)
	}

	// Write updated mod file
	if _, _, err := mod.Write(); err != nil {
		fmt.Printf("Failed to write mod file: %v\n", err)
		os.Exit(1)
	}

	// Add the data directory file to the index
	if err := index.RefreshFileWithHash(destPath, hashFormat, hash, false); err != nil {
		fmt.Printf("Failed to add data file to index: %v\n", err)
		os.Exit(1)
	}

	// Update the metadata file hash in the index
	metaPath := mod.GetFilePath()
	if err := index.RefreshFileWithHash(metaPath, "sha256", "", true); err != nil {
		fmt.Printf("Failed to update index for metadata file: %v\n", err)
		os.Exit(1)
	}

	if err := index.Write(); err != nil {
		fmt.Printf("Failed to write index: %v\n", err)
		os.Exit(1)
	}
	if err := pack.UpdateIndexHash(); err != nil {
		fmt.Printf("Failed to update pack hash: %v\n", err)
		os.Exit(1)
	}
	if err := pack.Write(); err != nil {
		fmt.Printf("Failed to write pack file: %v\n", err)
		os.Exit(1)
	}

	// Remove the original JAR from the mods directory (if it exists there)
	origPath := mod.GetDestFilePath()
	if origPath != destPath && origPath != "" {
		if _, err := os.Stat(origPath); err == nil {
			os.Remove(origPath)
			fmt.Printf("Removed original JAR from %s (now using mirror)\n", origPath)
		}
	}

	// Add data directory to .gitignore
	gitignorePath := filepath.Join(filepath.Dir(viper.GetString("pack-file")), ".gitignore")
	addToGitignore(gitignorePath, ".packwiz-data/")

	fmt.Printf("\nSuccessfully mirrored %s!\n", mod.Name)
	fmt.Printf("Data file: %s\n", destPath)
	if baseURL != "" {
		fmt.Printf("Download URL: %s\n", mod.Download.URL)
	} else {
		fmt.Println("\nNote: No base URL configured. The original download URL is preserved.")
		fmt.Println("To make the modpack self-contained, set a base URL with:")
		fmt.Println("  packwiz settings base-url <url>")
		fmt.Println("Then mirror again (unmirror + mirror) to update download URLs.")
	}
}

func runUnmirror(cmd *cobra.Command, args []string) {
	modName := args[0]

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

	// Find the mod
	modPath, found := index.FindMod(modName)
	if !found {
		fmt.Printf("Mod %q not found in the modpack.\n", modName)
		os.Exit(1)
	}

	mod, err := core.LoadMod(modPath)
	if err != nil {
		fmt.Printf("Failed to load mod %q: %v\n", modName, err)
		os.Exit(1)
	}

	if !mod.Mirror {
		fmt.Printf("Mod %q is not mirrored.\n", mod.Name)
		return
	}

	// Remove the data directory file
	dataDir := pack.GetDataDir()
	dataPath := filepath.Join(dataDir, filepath.FromSlash(mod.FileName))
	if _, err := os.Stat(dataPath); err == nil {
		if err := os.Remove(dataPath); err != nil {
			fmt.Printf("Warning: failed to remove data file %s: %v\n", dataPath, err)
		} else {
			fmt.Printf("Removed data file: %s\n", dataPath)
		}
	}

	// Remove the data directory file from the index
	if err := index.RemoveFile(dataPath); err != nil {
		fmt.Printf("Warning: failed to remove from index: %v\n", err)
	}

	// Update the mod file: remove mirror flag and restore original download URL
	mod.Mirror = false
	if mod.Download.MirrorURL != "" {
		mod.Download.URL = mod.Download.MirrorURL
		mod.Download.MirrorURL = ""
	}
	if mod.Download.MirrorMode != "" {
		mod.Download.Mode = mod.Download.MirrorMode
		mod.Download.MirrorMode = ""
	}

	if _, _, err := mod.Write(); err != nil {
		fmt.Printf("Failed to write mod file: %v\n", err)
		os.Exit(1)
	}

	if err := index.Write(); err != nil {
		fmt.Printf("Failed to write index: %v\n", err)
		os.Exit(1)
	}
	if err := pack.UpdateIndexHash(); err != nil {
		fmt.Printf("Failed to update pack hash: %v\n", err)
		os.Exit(1)
	}
	if err := pack.Write(); err != nil {
		fmt.Printf("Failed to write pack file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully unmirrored %s.\n", mod.Name)
	fmt.Println("Original download URL restored.")
}

func copyJARFile(src, dst string) error {
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

// addToGitignore appends a line to the .gitignore file if it doesn't already contain it.
func addToGitignore(gitignorePath, line string) {
	data, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return
	}

	content := ""
	if err == nil {
		content = string(data)
	}

	for _, existingLine := range strings.Split(content, "\n") {
		if strings.TrimSpace(existingLine) == strings.TrimSpace(line) {
			return
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		f.WriteString("\n")
	}
	f.WriteString(line + "\n")
}