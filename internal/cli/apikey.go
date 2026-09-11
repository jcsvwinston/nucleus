package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
)

// runAPIKey manages machine credentials from the command line, which is
// where they are actually issued: a key for CI is created by an operator at
// a terminal, not by a user on a page.
func runAPIKey(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printAPIKeyUsage(stderr)
		return fmt.Errorf("usage: nucleus apikey <create|list|revoke|rotate> [flags]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		printAPIKeyUsage(stdout)
		return nil
	case "create":
		return runAPIKeyCreate(rest, stdout, stderr)
	case "list":
		return runAPIKeyList(rest, stdout, stderr)
	case "revoke":
		return runAPIKeyRevoke(rest, stdout, stderr)
	case "rotate":
		return runAPIKeyRotate(rest, stdout, stderr)
	default:
		return fmt.Errorf("unknown apikey subcommand %q (want create, list, revoke or rotate)", sub)
	}
}

// printAPIKeyUsage documents the four subcommands in one place, so `apikey`
// with no arguments and `apikey --help` say the same thing.
func printAPIKeyUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: nucleus apikey <command> [flags]

Manage the credentials programs use to call your API. A key is shown once,
at creation: only its hash is stored.

Commands:
  create   Issue a key           --name (required) --owner --scopes --expires-in
  list     Show issued keys      --owner
  revoke   Stop a key working    --id
  rotate   Replace a key         --id --grace (default 1h, the old key keeps working)

Every command takes --config to point at a nucleus config file.
`)
}

func apiKeyStore(configPath string) (*apikeys.SQLStore, func(), error) {
	cfg, database, cleanup, err := newDatabase(configPath)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := database.SqlDB()
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	flavor, err := apiKeyFlavor(cfg.DefaultDatabase().URL)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	store, err := apikeys.NewSQLStore(context.Background(), sqlDB, apikeys.SQLStoreConfig{Flavor: flavor})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return store, cleanup, nil
}

// apiKeyFlavor maps a database URL onto the dialect the store speaks. An
// engine the store cannot speak fails HERE, with its name, rather than at
// the first statement.
func apiKeyFlavor(url string) (apikeys.Flavor, error) {
	lowered := strings.ToLower(strings.TrimSpace(url))
	switch {
	case strings.HasPrefix(lowered, "postgres://"), strings.HasPrefix(lowered, "postgresql://"):
		return apikeys.FlavorPostgres, nil
	case strings.HasPrefix(lowered, "mysql://"), strings.Contains(lowered, "@tcp("):
		return apikeys.FlavorMySQL, nil
	case strings.HasPrefix(lowered, "sqlite://"), strings.HasSuffix(lowered, ".db"),
		strings.HasPrefix(lowered, "file:"), lowered == ":memory:":
		return apikeys.FlavorSQLite, nil
	default:
		return "", fmt.Errorf("apikey: cannot tell which SQL dialect %q speaks (supported: postgres, mysql, sqlite)", url)
	}
}

func runAPIKeyCreate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("apikey create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "Path to nucleus config file")
	name := fs.String("name", "", "What this key is for (shown in listings)")
	owner := fs.String("owner", "", "Account the key belongs to")
	scopes := fs.String("scopes", "", "Space- or comma-separated scopes")
	expires := fs.Duration("expires-in", 0, "Lifetime, e.g. 720h (default: no expiry)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required: a key nobody can identify is a key nobody dares revoke")
	}

	store, cleanup, err := apiKeyStore(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	spec := apikeys.Key{
		Name:    strings.TrimSpace(*name),
		OwnerID: strings.TrimSpace(*owner),
		Scopes:  splitScopes(*scopes),
	}
	if *expires > 0 {
		spec.ExpiresAt = time.Now().UTC().Add(*expires)
	}

	key, secret, err := apikeys.Issue(context.Background(), store, spec)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s\n", secret)
	fmt.Fprintf(stderr, "key %s created. This is the only time the secret is shown.\n", key.ID)
	return nil
}

func runAPIKeyList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("apikey list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "Path to nucleus config file")
	owner := fs.String("owner", "", "Only this owner's keys")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	store, cleanup, err := apiKeyStore(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	keys, err := store.List(context.Background(), strings.TrimSpace(*owner))
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tOWNER\tSCOPES\tSTATE\tLAST USED")
	for _, key := range keys {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			key.ID, valueOr(key.Name, "-"), valueOr(key.OwnerID, "-"),
			valueOr(strings.Join(key.Scopes, ","), "-"), keyState(key), formatStamp(key.LastUsedAt))
	}
	return w.Flush()
}

func runAPIKeyRevoke(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("apikey revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "Path to nucleus config file")
	id := fs.String("id", "", "Key id to revoke")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" {
		return fmt.Errorf("--id is required")
	}

	store, cleanup, err := apiKeyStore(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := store.Revoke(context.Background(), strings.TrimSpace(*id), time.Now().UTC()); err != nil {
		if errors.Is(err, apikeys.ErrNotFound) {
			return fmt.Errorf("no key with id %q", *id)
		}
		return err
	}
	fmt.Fprintf(stdout, "key %s revoked\n", *id)
	return nil
}

func runAPIKeyRotate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("apikey rotate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "Path to nucleus config file")
	id := fs.String("id", "", "Key id to rotate")
	grace := fs.Duration("grace", time.Hour, "How long the old key keeps working")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" {
		return fmt.Errorf("--id is required")
	}

	store, cleanup, err := apiKeyStore(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	key, secret, err := apikeys.Rotate(context.Background(), store, strings.TrimSpace(*id), *grace)
	if err != nil {
		if errors.Is(err, apikeys.ErrNotFound) {
			return fmt.Errorf("no key with id %q", *id)
		}
		return err
	}
	fmt.Fprintf(stdout, "%s\n", secret)
	fmt.Fprintf(stderr, "key %s replaces %s; the old one stops working in %s.\n", key.ID, *id, *grace)
	return nil
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("%s does not accept positional arguments", fs.Name())
	}
	return nil
}

func splitScopes(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}

func keyState(key apikeys.Key) string {
	now := time.Now().UTC()
	switch {
	case !key.RevokedAt.IsZero():
		return "revoked"
	case !key.ExpiresAt.IsZero() && now.After(key.ExpiresAt):
		return "expired"
	case !key.ExpiresAt.IsZero():
		return "expires " + key.ExpiresAt.Format("2006-01-02")
	default:
		return "active"
	}
}

func formatStamp(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
