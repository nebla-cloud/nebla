package cli

import (
	"errors"
	"fmt"
	"os"
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
			key = strings.TrimSpace(key)
			if key == "" {
				return errors.New("public key is empty")
			}
			api, err := a.api()
			if err != nil {
				return err
			}
			res, err := api.C.PostSshKeysWithResponse(cmd.Context(), apiclient.PostSshKeysJSONRequestBody{Name: name, PublicKey: key})
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
			fmt.Fprintf(a.env.Stdout, "SSH key %s created\n", id64(k.Id))
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "key name")
	create.Flags().StringVar(&pubKey, "public-key", "", "public key text")
	create.Flags().StringVar(&pubKeyFile, "public-key-from-file", "", "read the public key from a file")
	_ = create.MarkFlagRequired("name")

	list := listCmd(a, "list", "List SSH keys", []string{"ID", "NAME", "FINGERPRINT", "AGE"}, nil,
		func(c *cobra.Command, api *API) ([]apiclient.SshKey, error) { return api.SSHKeys(c.Context()) },
		func(k apiclient.SshKey) []string {
			return []string{id64(k.Id), k.Name, k.Fingerprint, age(k.Created, time.Now())}
		})

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
			if a.flags.output != "table" {
				return a.printer().Object(k)
			}
			fmt.Fprintf(a.env.Stdout, "SSH key %s deleted\n", k.Name)
			return nil
		},
	}

	cmd.AddCommand(create, list, del)
	return cmd
}
