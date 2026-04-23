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

	bake.Task("build", "Builds the binary for Windows x64 (pure Go, no CGO required)", func(ctx *gobake.Context) error {
		ctx.Log("Building %s v%s...", bake.Info.Name, bake.Info.Version)

		// Windows x64 only: the window/input layer uses Win32 syscalls directly.
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
				"CGO_ENABLED=0",
				"GOOS=" + t.os,
				"GOARCH=" + t.arch,
			}

			ctx.Log("Building target: %s/%s (CGO_ENABLED=0)", t.os, t.arch)
			if err := ctx.Run("go", "build", "-ldflags", ldflags, "-o", output, "."); err != nil {
				return fmt.Errorf("failed to build for %s/%s: %w (if a cached build is causing issues, try `atlas.hub --clear-go-cache`)", t.os, t.arch, err)
			}
		}
		return nil
	})

	bake.Task("clean", "Removes build artifacts", func(ctx *gobake.Context) error {
		return ctx.Remove("build")
	})

	return nil
}
