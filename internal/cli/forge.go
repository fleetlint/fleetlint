package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/forge"
)

// forgeFlags name the forge and its token for the commands that reach it.
type forgeFlags struct {
	forge, token string
}

func addForgeFlags(cmd *cobra.Command, f *forgeFlags) {
	cmd.Flags().StringVar(&f.forge, "forge", "", "the forge to use: github, github:<host>, gitea:<host>, gitlab:<host> or an https URL (default $FLEETLINT_FORGE)")
	cmd.Flags().StringVar(&f.token, "token", "", "token for that forge's API and for pushing (default $FLEETLINT_TOKEN, which keeps it out of the process list)")
}

// client is the forge client the flags (or their environment variables)
// describe; hosts are further hosts the user named, such as --from sources.
func (f forgeFlags) client(hosts ...string) (forge.Client, error) {
	c := forge.Client{Hosts: hosts, GivenToken: orEnv(f.token, "FLEETLINT_TOKEN")}
	if spec := orEnv(f.forge, "FLEETLINT_FORGE"); spec != "" {
		named, err := forge.ParseNamed(spec)
		if err != nil {
			return forge.Client{}, &configError{err}
		}
		c.Forge = named
	}
	return c, nil
}

func orEnv(v, key string) string {
	if v != "" {
		return v
	}
	return os.Getenv(key)
}
