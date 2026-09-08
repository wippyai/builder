// SPDX-License-Identifier: MIT
package main

import (
	"flag"
	"fmt"
	"github.com/wippyai/builder/internal/assemble"
	"os"
)

func color(text, code string) string {
	info, err := os.Stderr.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	return text
}
func execute(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Wippy Builder — assemble native Wippy applications\n\nCommands: build, toolchain, pack, seal, validate, package\nUsage: wippy-builder COMMAND INPUT [options]\n\nBuild:   wippy-builder build wippy.build.json --output dist/application\nPack:    wippy-builder pack wippy.build.json --toolchain ./dist/wippy\nArchive: wippy-builder package dist/application --output dist/application.tar.gz")
		return nil
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: wippy-builder build|toolchain|pack|seal|validate|package INPUT [--output PATH] [--toolchain PATH] [--version VERSION] [--mode base|bootstrap]")
	}
	operation, input := args[0], args[1]
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	output := flags.String("output", "", "output path")
	toolchain := flags.String("toolchain", "wippy", "native Wippy toolchain")
	version := flags.String("version", "", "root pack version")
	mode := flags.String("mode", "", "embedded deployment mode")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	switch operation {
	case "build", "toolchain":
		if *output == "" {
			return fmt.Errorf("--output is required")
		}
		fmt.Fprintln(os.Stderr, color("Building pinned "+operation+"…", "36"))
		return assemble.Build(input, *output, operation == "toolchain")
	case "validate":
		_, err := assemble.ReadManifest(input)
		return err
	case "seal":
		return assemble.Seal(input, *version, *mode)
	case "pack":
		return assemble.PackRoot(input, *toolchain, *version)
	case "package":
		if *output == "" {
			return fmt.Errorf("--output is required")
		}
		return assemble.Package(input, *output)
	default:
		return fmt.Errorf("unknown command %q", operation)
	}
}
func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, color("wippy-builder: "+err.Error(), "31"))
		os.Exit(1)
	}
}
