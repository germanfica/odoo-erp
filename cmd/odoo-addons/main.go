// odoo-addons prepares Odoo addon archives without requiring Git.
// Written using the Go 1.17 standard library only.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: odoo-addons <fetch|update|generate|validate> [options]")
	}
	switch args[0] {
	case "fetch":
		fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
		modPath := fs.String("mod", "config/addons.mod", "source repository manifest")
		lockPath := fs.String("lock", "config/addons.lock", "optional lockfile")
		requiredPath := fs.String("required", "config/addons.required", "mandatory Odoo modules manifest")
		destinationRoot := fs.String("destination", "build/odoo-addons", "new directory for extracted addon repositories")
		configSource := fs.String("config-source", "config/odoo.conf", "user-maintained Odoo configuration")
		configDestination := fs.String("config-destination", "build/odoo.conf", "new generated Odoo configuration")
		installedPrefix := fs.String("installed-prefix", "/opt/odoo-addons", "directory where addons will live in final image")
		basePaths := fs.String("base-paths", "/usr/lib/python3/dist-packages/odoo/addons,/mnt/extra-addons", "comma-separated base addon paths")
		dryRun := fs.Bool("dry-run", false, "validate and print planned changes without downloads or writes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		return fetch(FetchOptions{*modPath, *lockPath, *requiredPath, *destinationRoot, *configSource, *configDestination, *installedPrefix, *basePaths, *dryRun})
	case "update":
		fs := flag.NewFlagSet("update", flag.ContinueOnError)
		modPath := fs.String("mod", "config/addons.mod", "source repository manifest")
		lockPath := fs.String("lock", "config/addons.lock", "lockfile to generate or replace")
		requiredPath := fs.String("required", "config/addons.required", "mandatory Odoo modules manifest")
		dryRun := fs.Bool("dry-run", false, "resolve branches without saving lockfile")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		return update(*modPath, *lockPath, *requiredPath, *dryRun)
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		modPath := fs.String("mod", "config/addons.mod", "source repository manifest")
		configSource := fs.String("config-source", "config/odoo.conf", "user-maintained Odoo configuration")
		configDestination := fs.String("config-destination", "build/odoo.conf", "generated Odoo configuration")
		installedPrefix := fs.String("installed-prefix", "/opt/odoo-addons", "final installed addon path prefix")
		basePaths := fs.String("base-paths", "/usr/lib/python3/dist-packages/odoo/addons,/mnt/extra-addons", "comma-separated base addon paths")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		entries, err := readManifest(*modPath)
		if err != nil {
			return err
		}
		rendered, err := renderConfiguration(*configSource, entries, *installedPrefix, *basePaths)
		if err != nil {
			return err
		}
		fmt.Printf("PLAN: generate %s from %s (source is never changed)\n", *configDestination, *configSource)
		return writeNewFileAtomically(*configDestination, rendered, 0644)
	case "validate":
		fs := flag.NewFlagSet("validate", flag.ContinueOnError)
		modPath := fs.String("mod", "config/addons.mod", "source repository manifest")
		lockPath := fs.String("lock", "config/addons.lock", "optional lockfile")
		requiredPath := fs.String("required", "config/addons.required", "mandatory modules")
		configSource := fs.String("config-source", "config/odoo.conf", "user config")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		entries, err := readManifest(*modPath)
		if err != nil {
			return err
		}
		if _, err := readRequirements(*requiredPath); err != nil {
			return err
		}
		if _, err := loadOptionalLock(*lockPath, entries); err != nil {
			return err
		}
		if _, err := renderConfiguration(*configSource, entries, "/opt/odoo-addons", "/usr/lib/python3/dist-packages/odoo/addons,/mnt/extra-addons"); err != nil {
			return err
		}
		fmt.Printf("Validated %d repositories, optional lock and user configuration\n", len(entries))
		return nil
	default:
		return fmt.Errorf("unknown command %q (use fetch, update, generate or validate)", args[0])
	}
}
