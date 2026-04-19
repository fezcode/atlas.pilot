//go:build gobake
package bake_recipe

import (
	"fmt"
	"github.com/fezcode/gobake"
)

func Run(bake *gobake.Engine) error {
	if err := bake.LoadRecipeInfo("recipe.piml"); err != nil {
		return err
	}

	bake.Task("build", "Builds the binary for Windows x64", func(ctx *gobake.Context) error {
		ctx.Log("Building %s v%s...", bake.Info.Name, bake.Info.Version)

		// Restricted to Windows x64 as robotgo/win dependencies are platform-specific
		targets := []struct {
			os   string
			arch string
		}{
			{"windows", "amd64"},
		}

		err := ctx.Mkdir("build")
		if err != nil {
			return err
		}

		ldflags := fmt.Sprintf("-X main.Version=%s", bake.Info.Version)

		for _, t := range targets {
			output := "build/" + bake.Info.Name + "-" + t.os + "-" + t.arch + ".exe"

			ctx.Env = []string{
				"GOOS=" + t.os,
				"GOARCH=" + t.arch,
			}

			ctx.Log("Building target: %s/%s", t.os, t.arch)
			err := ctx.Run("go", "build", "-ldflags", ldflags, "-o", output, ".")
			if err != nil {
				ctx.Log("Warning: Failed to build for %s/%s: %v", t.os, t.arch, err)
				continue
			}
		}
		return nil
	})

	bake.Task("clean", "Removes build artifacts", func(ctx *gobake.Context) error {
		return ctx.Remove("build")
	})

	return nil
}
