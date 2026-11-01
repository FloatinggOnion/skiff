package cmd

import (
	"fmt"

	"github.com/nuelScript/skiff/internal/auth"
	"github.com/nuelScript/skiff/internal/db"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newServerReset2FACmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset-2fa <email>",
		Short: "Turn off two-factor sign-in for an account that lost its device and recovery codes",
		Long: `Turns off two-factor sign-in for one dashboard account, so it can sign in
with its password and set two-factor up again. Run it on the server, where
access to the machine is the proof of identity.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()
			store := auth.NewStore(database)
			u, ok := store.UserByEmail(args[0])
			if !ok {
				return fmt.Errorf("no account with the email %s", args[0])
			}
			if !store.HasTwoFactor(u.ID) {
				ui.Note(u.Email + " doesn't have two-factor on")
				return nil
			}
			if err := store.DisableTwoFactor(u.ID); err != nil {
				return err
			}
			ui.Done("Two-factor is off for " + u.Email + " — sign in with the password and turn it back on in Settings")
			return nil
		},
	}
}
