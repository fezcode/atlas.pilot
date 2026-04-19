//go:build gobake
package bake_recipe

import (
	"fmt"
	"runtime"
	"github.com/fezcode/gobake"
)

func Run(bake *gobake.Engine) error {
	if err := bake.LoadRecipeInfo("recipe.piml"); err != nil {
		return err
	}

	bake.Task("build", "Builds the binary for the current platform", func(ctx *gobake.Context) error {
		ctx.Log("Building %s v%s...", bake.Info.Name, bake.Info.Version)

		output := "build/atlas.pilot"
		if runtime.GOOS == "windows" {
			output += ".exe"
		}

		err := ctx.Mkdir("build")
		if err != nil {
			return err
		}

		ldflags := fmt.Sprintf("-X main.Version=%s", bake.Info.Version)
		
		err = ctx.Run("go", "build", "-ldflags", ldflags, "-o", output, ".")
		if err != nil {
			return err
		}
		return nil
	})

	bake.Task("clean", "Removes build artifacts", func(ctx *gobake.Context) error {
		return ctx.Remove("build")
	})

	return nil
}
