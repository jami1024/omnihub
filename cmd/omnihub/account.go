package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/db"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// runAccountCommand dispatches the `omnihub account ...` subcommands.
// Returns to the caller; never re-enters the gateway logic.
func runAccountCommand(args []string) {
	if len(args) == 0 {
		printAccountUsage(os.Stderr)
		os.Exit(2)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		exitOnErr(accountAdd(rest))
	case "list", "ls":
		exitOnErr(accountList(rest))
	case "enable":
		exitOnErr(accountSetEnabled(rest, true))
	case "disable":
		exitOnErr(accountSetEnabled(rest, false))
	case "delete", "rm":
		exitOnErr(accountDelete(rest))
	case "help", "-h", "--help":
		printAccountUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown account subcommand: %s\n\n", cmd)
		printAccountUsage(os.Stderr)
		os.Exit(2)
	}
}

func printAccountUsage(w io.Writer) {
	fmt.Fprint(w, `omnihub account — manage upstream provider accounts

Usage:
  omnihub account add     [flags]      Insert a new account.
  omnihub account list                  List every account (any state).
  omnihub account enable  <name>        Mark an account enabled.
  omnihub account disable <name>        Mark an account disabled.
  omnihub account delete  <name>        Hard-delete an account.

'add' flags:
  --name=NAME           (required) unique account name
  --provider=NAME       (required) driver name: anthropic | claude-platform
  --api-key=KEY         (required) upstream API key
  --aws-region=REGION   (claude-platform) AWS region
  --workspace-id=ID     (claude-platform) Anthropic workspace ID
  --base-url=URL        override the driver's default endpoint
  --weight=N            weighted random selection weight (default 100)
  --priority=N          lower number = preferred tier (default 0)
  --cost-multiplier=F   scales recorded cost (default 1.0)
  --disabled            create the row in disabled state

The CLI shares OMNIHUB_DATABASE_URL with the gateway. The DSN must
point at a Postgres instance whose migrations have already run
(usually by starting the gateway once).
`)
}

// accountAdd inserts a new row via repository.AccountRepo.Insert.
func accountAdd(args []string) error {
	fs := flag.NewFlagSet("account add", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	var (
		name        = fs.String("name", "", "account name (unique)")
		drv         = fs.String("provider", "", "driver name")
		apiKey      = fs.String("api-key", "", "upstream API key")
		awsRegion   = fs.String("aws-region", "", "AWS region (claude-platform)")
		workspaceID = fs.String("workspace-id", "", "workspace id (claude-platform)")
		baseURL     = fs.String("base-url", "", "endpoint override")
		weight      = fs.Int("weight", 100, "weighted random weight")
		priority    = fs.Int("priority", 0, "priority tier (lower = preferred)")
		multiplier  = fs.Float64("cost-multiplier", 1.0, "cost multiplier (1.0 = base)")
		disabled    = fs.Bool("disabled", false, "create in disabled state")
		cbFailures  = fs.Int("circuit-failure-threshold", -1, "per-account override; -1 = use global default")
		cbDuration  = fs.String("circuit-open-duration", "", "per-account override Go duration; empty = use global default")
		cbHalfOpen  = fs.Int("circuit-half-open-success", -1, "per-account override; -1 = use global default")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *name == "" || *drv == "" || *apiKey == "" {
		fs.Usage()
		return errors.New("missing required flags: --name, --provider, --api-key")
	}

	creds := map[string]string{"api_key": *apiKey}
	if *awsRegion != "" {
		creds["aws_region"] = *awsRegion
	}
	if *workspaceID != "" {
		creds["workspace_id"] = *workspaceID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	params := repository.InsertParams{
		Name:           *name,
		Provider:       *drv,
		Enabled:        !*disabled,
		Weight:         *weight,
		Priority:       *priority,
		CostMultiplier: *multiplier,
		BaseURL:        *baseURL,
		Credentials:    creds,
	}
	if *cbFailures >= 0 {
		v := *cbFailures
		params.CircuitFailureThreshold = &v
	}
	if *cbDuration != "" {
		d, perr := time.ParseDuration(*cbDuration)
		if perr != nil {
			return fmt.Errorf("--circuit-open-duration: %w", perr)
		}
		params.CircuitOpenDuration = &d
	}
	if *cbHalfOpen > 0 {
		v := *cbHalfOpen
		params.CircuitHalfOpenSuccess = &v
	}

	repo := repository.NewAccountRepo(pool)
	id, err := repo.Insert(ctx, params)
	if err != nil {
		return err
	}
	fmt.Printf("account created\n  id:       %d\n  name:     %s\n  provider: %s\n  enabled:  %t\n",
		id, *name, *drv, !*disabled)
	return nil
}

// accountList prints every row as a tab-aligned table. Credentials
// are summarised (key names only) so secrets never land on screen.
func accountList(args []string) error {
	fs := flag.NewFlagSet("account list", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewAccountRepo(pool)
	accounts, enabledFlags, err := repo.ListAll(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Println("no accounts. Add one with: omnihub account add --name=... --provider=... --api-key=...")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER\tENABLED\tPRIORITY\tWEIGHT\tMULTIPLIER\tCREDENTIALS")
	for i, a := range accounts {
		credSummary := summariseCredentials(a)
		fmt.Fprintf(tw,
			"%d\t%s\t%s\t%s\t%d\t%d\t%.2f\t%s\n",
			a.ID,
			a.Name,
			a.Provider,
			yesNo(enabledFlags[i]),
			a.Priority,
			a.Weight,
			a.CostMultiplier,
			credSummary,
		)
	}
	return tw.Flush()
}

// summariseCredentials lists the credential KEYS present on the
// account (e.g. "api_key,aws_region,workspace_id"). Values are
// deliberately not printed.
func summariseCredentials(a *provider.Account) string {
	if len(a.Credentials) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(a.Credentials))
	for k := range a.Credentials {
		keys = append(keys, k)
	}
	return joinComma(keys)
}

func joinComma(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// accountSetEnabled toggles the enabled flag for a named account.
func accountSetEnabled(args []string, enabled bool) error {
	if len(args) < 1 || args[0] == "" {
		return errors.New("usage: omnihub account enable|disable <name>")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewAccountRepo(pool)
	if err := repo.SetEnabled(ctx, name, enabled); err != nil {
		return err
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("account %q is now %s. The change becomes visible to the gateway within ~30 s (account pool refresh interval).\n", name, state)
	return nil
}

// accountDelete hard-deletes a named account. The CLI does not prompt
// for confirmation; the operation is reversed by re-running `account
// add`.
func accountDelete(args []string) error {
	if len(args) < 1 || args[0] == "" {
		return errors.New("usage: omnihub account delete <name>")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewAccountRepo(pool)
	if err := repo.Delete(ctx, name); err != nil {
		return err
	}
	fmt.Printf("account %q deleted\n", name)
	return nil
}

// openDBForCLI is the CLI-side counterpart of the gateway's
// initDatabase: smaller pool, no migrations, fail-fast on a missing
// DSN. CLI commands do not run migrations because the gateway should
// have already done so on first boot.
func openDBForCLI(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("OMNIHUB_DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("OMNIHUB_DATABASE_URL is not set")
	}
	return db.Open(ctx, db.Config{
		DSN:             dsn,
		MaxConns:        4,
		MinConns:        1,
		MaxConnLifetime: 5 * time.Minute,
		MaxConnIdleTime: 1 * time.Minute,
	})
}

// exitOnErr writes err to stderr and exits 1 when err is non-nil.
// The CLI commands intentionally use this single helper so error
// messages all flow through the same formatter.
func exitOnErr(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err.Error())
	os.Exit(1)
}
