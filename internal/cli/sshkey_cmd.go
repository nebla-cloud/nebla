package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

func newSSHKeyCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "ssh-key", Short: "Manage SSH keys"}

	var name, pubKey, pubKeyFile string
	create := &cobra.Command{
		Use:   "create",
		Short: "Add an SSH key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (pubKey == "") == (pubKeyFile == "") {
				return errors.New("one of --public-key or --public-key-from-file is required")
			}
			key := pubKey
			if pubKeyFile != "" {
				b, err := os.ReadFile(pubKeyFile)
				if err != nil {
					return err
				}
				key = string(b)
			}
			api, err := a.api()
			if err != nil {
				return err
			}
			res, err := api.C.PostSshKeysWithResponse(cmd.Context(), apiclient.PostSshKeysJSONRequestBody{Name: name, PublicKey: strings.TrimSpace(key)})
			if err != nil {
				return err
			}
			k, err := result(res.StatusCode(), res.Body, res.JSON201)
			if err != nil {
				return err
			}
			if a.flags.output != "table" {
				return a.printer().Object(k)
			}
			fmt.Fprintf(a.env.Stdout, "SSH key %d created\n", k.Id)
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "key name")
	create.Flags().StringVar(&pubKey, "public-key", "", "public key text")
	create.Flags().StringVar(&pubKeyFile, "public-key-from-file", "", "read the public key from a file")
	_ = create.MarkFlagRequired("name")

	var noHeader bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List SSH keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			keys, err := api.SSHKeys(cmd.Context())
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				if keys == nil {
					keys = []apiclient.SshKey{}
				}
				return p.Object(keys)
			}
			p.NoHeader = noHeader
			now := time.Now()
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{strconv.FormatInt(k.Id, 10), k.Name, k.Fingerprint, age(k.Created, now)})
			}
			p.Table([]string{"ID", "NAME", "FINGERPRINT", "AGE"}, rows)
			return nil
		},
	}
	list.Flags().BoolVar(&noHeader, "no-header", false, "omit the table header")

	del := &cobra.Command{
		Use:   "delete <ref>",
		Short: "Delete an SSH key by id or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			k, err := api.SSHKey(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			res, err := api.C.DeleteSshKeyWithResponse(cmd.Context(), k.Id)
			if err != nil {
				return err
			}
			if s := res.StatusCode(); s < 200 || s > 299 {
				return apiErr(s, res.Body)
			}
			fmt.Fprintf(a.env.Stdout, "SSH key %s deleted\n", k.Name)
			return nil
		},
	}

	cmd.AddCommand(create, list, del)
	return cmd
}
