package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

// remoteClient talks to the dashboard you signed in to. Creating a database or bucket waits for it to start, so it gets longer than a plain call.
func remoteClient(timeout time.Duration) (*apiClient, error) {
	c, err := signedIn()
	if err != nil {
		return nil, err
	}
	return &apiClient{base: c.URL, token: c.Token, http: &http.Client{Timeout: timeout}}, nil
}

func remoteDo(method, path string, body, out any) error {
	c, err := remoteClient(3 * time.Minute)
	if err != nil {
		return err
	}
	return c.do(method, path, body, out)
}

func table(header string, rows [][]string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, header)
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	return w.Flush()
}

// mustConfirm stops a command that deletes data unless --yes was given.
func mustConfirm(yes bool, what string) error {
	if yes {
		return nil
	}
	return fmt.Errorf("this deletes %s and can't be undone, run it again with --yes", what)
}

func attachCmds(kind string) []*cobra.Command {
	path := "/" + kind + "s/"
	one := func(use, short, method, done string) *cobra.Command {
		return &cobra.Command{
			Use: use, Short: short, Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := remoteDo(method, path+url.PathEscape(args[0])+"/apps/"+url.PathEscape(args[1]), nil, nil); err != nil {
					return err
				}
				ui.Done(fmt.Sprintf(done, args[0], args[1]))
				return nil
			},
		}
	}
	return []*cobra.Command{
		one("attach <"+kind+"> <app>", "Give an app the "+kind+"'s connection variables", http.MethodPut, "Attached %s to %s, deploy it to use them"),
		one("detach <"+kind+"> <app>", "Take the "+kind+"'s variables out of an app", http.MethodDelete, "Detached %s from %s"),
	}
}

func addDBManageCmds(db *cobra.Command) {
	var engine string
	create := &cobra.Command{
		Use: "create <name>", Short: "Create a database", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := remoteDo(http.MethodPost, "/databases", map[string]string{"engine": engine, "name": args[0]}, nil); err != nil {
				return err
			}
			ui.Done("Created " + args[0] + ", attach it to an app with `skiff db attach " + args[0] + " <app>`")
			return nil
		},
	}
	create.Flags().StringVarP(&engine, "engine", "e", "postgres", "postgres, mysql, mongodb or redis")
	var yes bool
	rm := &cobra.Command{
		Use: "rm <name>", Short: "Delete a database, its data and its backups", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mustConfirm(yes, args[0]+", its data and its backups"); err != nil {
				return err
			}
			if err := remoteDo(http.MethodDelete, "/databases/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			ui.Done("Deleted " + args[0])
			return nil
		},
	}
	rm.Flags().BoolVarP(&yes, "yes", "y", false, "delete without asking")
	db.AddCommand(create, rm)
	db.AddCommand(attachCmds("database")...)
}

func newBucketCmd() *cobra.Command {
	bucket := &cobra.Command{Use: "bucket", Short: "Manage your team's buckets"}
	bucket.AddCommand(&cobra.Command{
		Use: "ls", Short: "List your team's buckets", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []struct {
				Name     string   `json:"name"`
				State    string   `json:"state"`
				Attached []string `json:"attached"`
			}
			if err := remoteDo(http.MethodGet, "/buckets", nil, &list); err != nil {
				return err
			}
			if len(list) == 0 {
				ui.Note("No buckets yet")
				return nil
			}
			rows := [][]string{}
			for _, b := range list {
				rows = append(rows, []string{b.Name, b.State, strings.Join(b.Attached, ", ")})
			}
			return table("NAME\tSTATE\tATTACHED TO", rows)
		},
	})
	bucket.AddCommand(&cobra.Command{
		Use: "create <name>", Short: "Create a bucket", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := remoteDo(http.MethodPost, "/buckets", map[string]string{"name": args[0]}, nil); err != nil {
				return err
			}
			ui.Done("Created " + args[0] + ", attach it to an app with `skiff bucket attach " + args[0] + " <app>`")
			return nil
		},
	})
	var yes bool
	rm := &cobra.Command{
		Use: "rm <name>", Short: "Delete a bucket and every file in it", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mustConfirm(yes, args[0]+" and every file in it"); err != nil {
				return err
			}
			if err := remoteDo(http.MethodDelete, "/buckets/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			ui.Done("Deleted " + args[0])
			return nil
		},
	}
	rm.Flags().BoolVarP(&yes, "yes", "y", false, "delete without asking")
	bucket.AddCommand(rm)
	bucket.AddCommand(attachCmds("bucket")...)
	return bucket
}

func newVolumeCmd() *cobra.Command {
	volume := &cobra.Command{Use: "volume", Short: "Manage an app's volumes"}
	volume.AddCommand(&cobra.Command{
		Use: "ls <app>", Short: "List an app's volumes", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []struct {
				Name string `json:"name"`
				Path string `json:"path"`
				Size int64  `json:"size"`
			}
			if err := remoteDo(http.MethodGet, "/apps/"+url.PathEscape(args[0])+"/volumes", nil, &list); err != nil {
				return err
			}
			if len(list) == 0 {
				ui.Note("No volumes yet")
				return nil
			}
			rows := [][]string{}
			for _, v := range list {
				rows = append(rows, []string{v.Name, v.Path, humanBytes(v.Size)})
			}
			return table("NAME\tPATH\tSIZE", rows)
		},
	})
	volume.AddCommand(&cobra.Command{
		Use: "add <app> <name> <path>", Short: "Give an app a volume, mounted on its next deploy", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := remoteDo(http.MethodPost, "/apps/"+url.PathEscape(args[0])+"/volumes", map[string]string{"name": args[1], "path": args[2]}, nil); err != nil {
				return err
			}
			ui.Done("Added " + args[1] + " at " + args[2] + ", deploy " + args[0] + " to mount it")
			return nil
		},
	})
	var yes bool
	rm := &cobra.Command{
		Use: "rm <app> <name>", Short: "Stop mounting a volume; its data is deleted after the next deploy", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mustConfirm(yes, "the data in "+args[1]+" once "+args[0]+" deploys"); err != nil {
				return err
			}
			if err := remoteDo(http.MethodDelete, "/apps/"+url.PathEscape(args[0])+"/volumes/"+url.PathEscape(args[1]), nil, nil); err != nil {
				return err
			}
			ui.Done("Removed " + args[1] + " from " + args[0])
			return nil
		},
	}
	rm.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")
	volume.AddCommand(rm)
	return volume
}

func newWorkerCmd() *cobra.Command {
	worker := &cobra.Command{Use: "worker", Short: "Manage an app's background workers"}
	worker.AddCommand(&cobra.Command{
		Use: "ls <app>", Short: "List an app's workers", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []struct {
				Name     string `json:"name"`
				Command  string `json:"command"`
				Replicas int    `json:"replicas"`
				Running  int    `json:"running"`
			}
			if err := remoteDo(http.MethodGet, "/apps/"+url.PathEscape(args[0])+"/workers", nil, &list); err != nil {
				return err
			}
			if len(list) == 0 {
				ui.Note("No workers yet")
				return nil
			}
			rows := [][]string{}
			for _, w := range list {
				rows = append(rows, []string{w.Name, fmt.Sprintf("%d/%d", w.Running, w.Replicas), w.Command})
			}
			return table("NAME\tRUNNING\tCOMMAND", rows)
		},
	})
	var command string
	var replicas int
	set := &cobra.Command{
		Use: "set <app> <worker>", Short: "Create a worker or change it", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"command": command, "replicas": replicas}
			if err := remoteDo(http.MethodPut, "/apps/"+url.PathEscape(args[0])+"/workers/"+url.PathEscape(args[1]), body, nil); err != nil {
				return err
			}
			ui.Done(fmt.Sprintf("%s runs %d of %s", args[0], replicas, args[1]))
			return nil
		},
	}
	set.Flags().StringVarP(&command, "command", "c", "", "what the worker runs, like \"node queue.js\"")
	set.Flags().IntVarP(&replicas, "replicas", "r", 1, "how many copies, from 1 to 10")
	_ = set.MarkFlagRequired("command")
	worker.AddCommand(set)
	worker.AddCommand(&cobra.Command{
		Use: "rm <app> <worker>", Short: "Stop and remove a worker", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := remoteDo(http.MethodDelete, "/apps/"+url.PathEscape(args[0])+"/workers/"+url.PathEscape(args[1]), nil, nil); err != nil {
				return err
			}
			ui.Done("Removed " + args[1] + " from " + args[0])
			return nil
		},
	})
	return worker
}

type remoteJob struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

// jobID finds one of the app's jobs by name, so people don't have to copy ids around.
func jobID(app, name string) (string, error) {
	var list []remoteJob
	if err := remoteDo(http.MethodGet, "/apps/"+url.PathEscape(app)+"/jobs", nil, &list); err != nil {
		return "", err
	}
	for _, j := range list {
		if j.Name == name || j.ID == name {
			return j.ID, nil
		}
	}
	return "", fmt.Errorf("%s has no job named %s, run `skiff job ls %s` to see them", app, name, app)
}

func newJobCmd() *cobra.Command {
	job := &cobra.Command{Use: "job", Short: "Manage an app's scheduled jobs"}
	job.AddCommand(&cobra.Command{
		Use: "ls <app>", Short: "List an app's jobs", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []remoteJob
			if err := remoteDo(http.MethodGet, "/apps/"+url.PathEscape(args[0])+"/jobs", nil, &list); err != nil {
				return err
			}
			if len(list) == 0 {
				ui.Note("No jobs yet")
				return nil
			}
			rows := [][]string{}
			for _, j := range list {
				rows = append(rows, []string{j.Name, j.Schedule, j.Command})
			}
			return table("NAME\tSCHEDULE\tCOMMAND", rows)
		},
	})
	var schedule, command string
	add := &cobra.Command{
		Use: "add <app> <name>", Short: "Add a job that runs on a cron schedule", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]string{"name": args[1], "schedule": schedule, "command": command}
			if err := remoteDo(http.MethodPost, "/apps/"+url.PathEscape(args[0])+"/jobs", body, nil); err != nil {
				return err
			}
			ui.Done("Added " + args[1] + " to " + args[0])
			return nil
		},
	}
	add.Flags().StringVarP(&schedule, "schedule", "s", "", "cron schedule in the server's time zone, like \"0 3 * * *\"")
	add.Flags().StringVarP(&command, "command", "c", "", "what the job runs")
	_ = add.MarkFlagRequired("schedule")
	_ = add.MarkFlagRequired("command")
	job.AddCommand(add)
	job.AddCommand(&cobra.Command{
		Use: "run <app> <job>", Short: "Run a job now and show its output", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobID(args[0], args[1])
			if err != nil {
				return err
			}
			c, err := remoteClient(time.Hour)
			if err != nil {
				return err
			}
			var out struct {
				OK     bool   `json:"ok"`
				Output string `json:"output"`
			}
			if err := c.do(http.MethodPost, "/apps/"+url.PathEscape(args[0])+"/jobs/"+url.PathEscape(id)+"/run", nil, &out); err != nil {
				return err
			}
			fmt.Print(out.Output)
			if !out.OK {
				return fmt.Errorf("%s failed", args[1])
			}
			return nil
		},
	})
	job.AddCommand(&cobra.Command{
		Use: "rm <app> <job>", Short: "Remove a job", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobID(args[0], args[1])
			if err != nil {
				return err
			}
			if err := remoteDo(http.MethodDelete, "/apps/"+url.PathEscape(args[0])+"/jobs/"+url.PathEscape(id), nil, nil); err != nil {
				return err
			}
			ui.Done("Removed " + args[1] + " from " + args[0])
			return nil
		},
	})
	return job
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
