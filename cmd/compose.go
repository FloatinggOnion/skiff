package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nuelScript/skiff/internal/compose"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newComposeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "compose",
		Short: "Bring a Docker Compose project onto Skiff",
	}
	var file, name string
	plan := &cobra.Command{
		Use:   "plan",
		Short: "Show what a Compose file would become, without changing anything",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := findComposeFile(file)
			if err != nil {
				return err
			}
			p, err := loadComposePlan(path, name)
			if err != nil {
				return err
			}
			printComposePlan(p)
			if len(p.Problems) > 0 {
				return fmt.Errorf("fix %d problem(s) before deploying this file", len(p.Problems))
			}
			return nil
		},
	}
	plan.Flags().StringVarP(&file, "file", "f", "", "Compose file (default: compose.yaml or docker-compose.yml here)")
	plan.Flags().StringVar(&name, "name", "", "name for the stack (default: this folder's name)")
	c.AddCommand(plan)
	return c
}

func findComposeFile(file string) (string, error) {
	if file != "" {
		return file, nil
	}
	for _, n := range compose.FileNames {
		if _, err := os.Stat(n); err == nil {
			return n, nil
		}
	}
	return "", errors.New("no Compose file here. Pass one with -f")
}

func loadComposePlan(path, name string) (*compose.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	if name == "" {
		name = filepath.Base(dir)
	}
	vars := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(dir, ".env")); err == nil {
		vars = compose.ParseEnvFile(b)
	}
	lookup := func(k string) (string, bool) {
		if v, ok := os.LookupEnv(k); ok {
			return v, true
		}
		v, ok := vars[k]
		return v, ok
	}
	f, err := compose.Parse(data, lookup)
	if err != nil {
		return nil, err
	}
	return compose.BuildPlan(name, f, func(p string) ([]byte, error) {
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return os.ReadFile(p)
	}), nil
}

// printComposePlan never prints environment values, because they often hold secrets.
func printComposePlan(p *compose.Plan) {
	fmt.Printf("\n  Stack %s\n\n", ui.Accent(p.Stack))
	for _, it := range p.Items {
		fmt.Printf("  %s  %s\n", ui.Accent(it.Service), composeSummary(it))
	}
	if len(p.Notes) > 0 {
		fmt.Println("\n  Worth knowing")
		for _, n := range p.Notes {
			fmt.Println("  · " + n)
		}
	}
	if len(p.Problems) > 0 {
		fmt.Println("\n  To fix")
		for _, n := range p.Problems {
			fmt.Println("  ✗ " + n)
		}
	}
	fmt.Println()
}

func composeSummary(it compose.Item) string {
	if it.Kind == compose.KindDatabase {
		return "managed " + it.Engine + " database, with daily backups"
	}
	if it.Kind == compose.KindWorker {
		return "worker on " + it.WorkerOf + ": " + strings.Join(it.Command, " ")
	}
	parts := []string{"app " + it.Name}
	switch {
	case it.Public:
		parts = append(parts, fmt.Sprintf("public on port %d", it.Port))
	case it.Port != 0:
		parts = append(parts, fmt.Sprintf("private on port %d", it.Port))
	default:
		parts = append(parts, "private, no port")
	}
	if it.Replicas > 1 {
		parts = append(parts, fmt.Sprintf("%d replicas", it.Replicas))
	}
	if n := len(it.Volumes); n > 0 {
		parts = append(parts, fmt.Sprintf("%d volume(s)", n))
	}
	if n := len(it.Env); n > 0 {
		parts = append(parts, fmt.Sprintf("%d env var(s)", n))
	}
	return strings.Join(parts, ", ")
}
