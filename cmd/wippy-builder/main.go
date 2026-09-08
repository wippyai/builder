// SPDX-License-Identifier: MIT
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wippyai/builder/internal/assemble"
)

func color(text, code string) string {
	info, err := os.Stderr.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	return text
}

var version = "dev"

func newCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "wippy-builder",
		Version:       version,
		Short:         "Assemble native Wippy applications",
		Long:          "Assemble a pinned Wippy runtime, application packs and native Go components.\nStart by validating your manifest, then build the toolchain, pack and executable.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Example: "  wippy-builder validate wippy.build.json\n" +
			"  wippy-builder toolchain wippy.build.json -o dist/wippy\n" +
			"  wippy-builder pack wippy.build.json --toolchain ./dist/wippy\n" +
			"  wippy-builder build wippy.build.json -o dist/app\n" +
			"  wippy-builder package dist/app -o dist/app.tar.gz",
	}
	root.AddCommand(newBuildCommand(false), newBuildCommand(true), newPackCommand(), newSealCommand(), newPackageCommand())
	root.AddCommand(&cobra.Command{
		Use:     "validate MANIFEST",
		Short:   "Validate manifest fields and version pins",
		Example: "  wippy-builder validate wippy.build.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if _, err := assemble.ReadManifest(args[0]); err != nil {
				return err
			}
			completed(command, "Validated manifest", args[0])
			return nil
		},
	})
	return root
}

func completed(command *cobra.Command, action, path string) {
	fmt.Fprintf(command.ErrOrStderr(), "%s %s\n", color(action, "32"), path)
}

func newBuildCommand(toolchain bool) *cobra.Command {
	name, description, exampleOutput := "build", "Build a standalone application", "dist/app"
	if toolchain {
		name, description = "toolchain", "Build the selected native Wippy toolchain"
		exampleOutput = "dist/wippy"
	}
	var output string
	command := &cobra.Command{
		Use:     name + " MANIFEST",
		Short:   description,
		Example: "  wippy-builder " + name + " wippy.build.json --output " + exampleOutput,
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output == "" {
				return errors.New("--output is required")
			}
			fmt.Fprintln(command.ErrOrStderr(), color(description+"…", "36"))
			if err := assemble.Build(args[0], output, toolchain); err != nil {
				return err
			}
			completed(command, "Built", output)
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "", "Executable output path")
	return command
}

func newPackCommand() *cobra.Command {
	var toolchain, version string
	command := &cobra.Command{
		Use:     "pack MANIFEST",
		Short:   "Pack a self-contained application and record its checksum",
		Example: "  wippy-builder pack wippy.build.json --toolchain ./dist/wippy --version 0.1.0",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := assemble.PackRoot(args[0], toolchain, version); err != nil {
				return err
			}
			completed(command, "Packed application and updated", args[0])
			return nil
		},
	}
	command.Flags().StringVar(&toolchain, "toolchain", "wippy", "Native Wippy toolchain path")
	command.Flags().StringVar(&version, "version", "", "Application version")
	return command
}

func newSealCommand() *cobra.Command {
	var version, mode string
	command := &cobra.Command{
		Use:     "seal MANIFEST",
		Short:   "Record checksums for intentionally regenerated packs",
		Example: "  wippy-builder seal wippy.build.json --mode bootstrap",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := assemble.Seal(args[0], version, mode); err != nil {
				return err
			}
			completed(command, "Updated pack checksums in", args[0])
			return nil
		},
	}
	command.Flags().StringVar(&version, "version", "", "Root application version")
	command.Flags().StringVar(&mode, "mode", "", "Embedded deployment mode: base or bootstrap")
	return command
}

func newPackageCommand() *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:     "package BINARY",
		Short:   "Verify and archive a complete build",
		Example: "  wippy-builder package dist/app --output dist/app.tar.gz",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output == "" {
				return errors.New("--output is required")
			}
			if err := assemble.Package(args[0], output); err != nil {
				return err
			}
			completed(command, "Packaged", output)
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "", "Release archive path")
	return command
}

func execute(args []string) error {
	command := newCommand()
	command.SetArgs(args)
	return command.Execute()
}

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, color("wippy-builder: "+err.Error(), "31"))
		os.Exit(1)
	}
}
