package cmd

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nuelScript/skiff/internal/cpbackup"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newServerRestoreCmd() *cobra.Command {
	var key string
	cmd := &cobra.Command{
		Use:   "restore <backup.skiffbak>",
		Short: "Restore Skiff's own state onto this server from a control-plane backup",
		Long: `Restores the projects, settings, domains, tokens and history a control-plane
backup holds into ~/.skiff. Run it on the new server before starting the panel.
The recovery key is in the old dashboard under Server → Control-plane backup.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := cpbackup.ParseKey(key)
			if err != nil {
				return err
			}
			sealed, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			plain, err := cpbackup.Open(k, sealed)
			if err != nil {
				return err
			}
			home, _ := os.UserHomeDir()
			dir := filepath.Join(home, ".skiff")
			if _, err := os.Stat(filepath.Join(dir, cpbackup.DBName)); err == nil {
				aside := fmt.Sprintf("%s.before-restore-%d", dir, time.Now().Unix())
				if err := os.Rename(dir, aside); err != nil {
					return err
				}
				ui.Note("moved the existing state to " + aside)
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			names, err := cpbackup.Unpack(bytes.NewReader(plain), dir)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "recovery.key"), []byte(hex.EncodeToString(k)), 0o600); err != nil {
				return err
			}
			ui.Done(fmt.Sprintf("Restored %d files into %s", len(names), dir))
			fmt.Println()
			fmt.Println("  Next:")
			fmt.Println("  1. Start the panel and router (or restart them): systemctl restart skiff-router 'skiff-panel@*'")
			fmt.Println("  2. Point your domain's DNS at this server.")
			fmt.Println("  3. Redeploy each git project from the dashboard; run `skiff deploy` again for CLI ones.")
			fmt.Println("  4. Recreate each database from the Databases page and restore its latest offsite backup.")
			return nil
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "the recovery key (64 hex characters)")
	_ = cmd.MarkFlagRequired("key")
	return cmd
}
