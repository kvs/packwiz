package settings

import (
	"fmt"
	"os"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
)

var dataDirCmd = &cobra.Command{
	Use:   "data-dir [path]",
	Short: "Set the data directory for mirrored mod files",
	Long: `Set the data directory where mirrored mod JARs are stored. This
directory is relative to the pack root (where pack.toml is located).

Mirrored mods have their JAR files copied to this directory so the
modpack is self-contained. The data directory is tracked in the
index and served by "packwiz serve".

The default is ".packwiz-data/".

To reset to the default, pass an empty string: packwiz settings data-dir ""`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pack, err := core.LoadPack()
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No pack.toml file found, run 'packwiz init' to create one!")
				os.Exit(1)
			}
			fmt.Printf("Error loading pack: %s\n", err)
			os.Exit(1)
		}

		dataDir := args[0]

		if pack.Options == nil {
			pack.Options = make(map[string]interface{})
		}

		if dataDir == "" {
			delete(pack.Options, "data-dir")
			fmt.Println("Data directory reset to default (.packwiz-data/).")
		} else {
			pack.Options["data-dir"] = dataDir
			fmt.Printf("Data directory set to: %s\n", dataDir)
		}

		err = pack.Write()
		if err != nil {
			fmt.Printf("Error writing pack: %s\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	settingsCmd.AddCommand(dataDirCmd)
}