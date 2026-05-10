package settings

import (
	"fmt"
	"os"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
)

var baseURLCmd = &cobra.Command{
	Use:   "base-url [url]",
	Short: "Set the base URL for mirrored mod downloads",
	Long: `Set the base URL where your modpack is served. When mods are mirrored,
their download URLs will point to this base URL, making the modpack
self-contained and installable without external download sources.

For example, if you serve your pack at https://my-modpack.netlify.app/,
set the base URL to that. Mirrored mods will have download URLs like
https://my-modpack.netlify.app/.packwiz-data/vista-1.0.0.jar.

To unset the base URL, pass an empty string: packwiz settings base-url ""`,
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

		baseURL := args[0]

		if pack.Options == nil {
			pack.Options = make(map[string]interface{})
		}

		if baseURL == "" {
			delete(pack.Options, "base-url")
			fmt.Println("Base URL cleared.")
		} else {
			// Ensure trailing slash for consistency
			last := baseURL[len(baseURL)-1]
			if last != '/' {
				baseURL += "/"
			}
			pack.Options["base-url"] = baseURL
			fmt.Printf("Base URL set to: %s\n", baseURL)
		}

		err = pack.Write()
		if err != nil {
			fmt.Printf("Error writing pack: %s\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	settingsCmd.AddCommand(baseURLCmd)
}