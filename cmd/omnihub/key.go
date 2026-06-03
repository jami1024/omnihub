package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/apikey"
)

// runKeyCommand dispatches the `omnihub key ...` subcommands.
func runKeyCommand(args []string) {
	if len(args) == 0 {
		printKeyUsage(os.Stderr)
		os.Exit(2)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		exitOnErr(keyAdd(rest))
	case "list", "ls":
		exitOnErr(keyList(rest))
	case "enable":
		exitOnErr(keyToggle(rest, true))
	case "disable":
		exitOnErr(keyToggle(rest, false))
	case "delete", "rm":
		exitOnErr(keyDelete(rest))
	case "help", "-h", "--help":
		printKeyUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown key subcommand: %s\n\n", cmd)
		printKeyUsage(os.Stderr)
		os.Exit(2)
	}
}

func printKeyUsage(w io.Writer) {
	fmt.Fprint(w, `omnihub key — manage virtual API keys

Usage:
  omnihub key add     [flags]      Create a new key. Cleartext is printed once.
  omnihub key list                  List every key (any state).
  omnihub key enable  <name>        Mark a key enabled.
  omnihub key disable <name>        Mark a key disabled.
  omnihub key delete  <name>        Hard-delete a key.

'add' flags:
  --name=NAME              (required) unique key handle (used in CLI ops)
  --label=LABEL            display label for request logs; defaults to --name
  --key=VALUE              use this cleartext key instead of generating one
                           (useful when migrating from OMNIHUB_API_KEYS env)
  --daily-usd=N            daily spend cap in USD (enforced in a follow-up commit)
  --rpm=N                  requests-per-minute ceiling (enforced in a follow-up commit)
  --allowed-models=LIST    comma-separated model allow-list; empty = all
  --disabled               create in disabled state

The CLI shares OMNIHUB_DATABASE_URL with the gateway.
`)
}

func keyAdd(args []string) error {
	fs := flag.NewFlagSet("key add", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	var (
		name     = fs.String("name", "", "unique key handle")
		label    = fs.String("label", "", "display label (default: --name)")
		raw      = fs.String("key", "", "use this cleartext key (default: generate)")
		dailyUSD = fs.Float64("daily-usd", -1, "daily spend cap (USD)")
		rpm      = fs.Int("rpm", -1, "requests-per-minute ceiling")
		allowed  = fs.String("allowed-models", "", "comma-separated model allow-list")
		disabled = fs.Bool("disabled", false, "create in disabled state")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		fs.Usage()
		return errors.New("missing required flag: --name")
	}

	cleartext := *raw
	if cleartext == "" {
		generated, err := apikey.Generate()
		if err != nil {
			return err
		}
		cleartext = generated
	}

	params := repository.ApiKeyInsertParams{
		Name:    *name,
		Hash:    apikey.HashOf(cleartext),
		Label:   *label,
		Enabled: !*disabled,
	}
	if *dailyUSD >= 0 {
		v := *dailyUSD
		params.DailyUSDLimit = &v
	}
	if *rpm > 0 {
		v := *rpm
		params.RPMLimit = &v
	}
	if *allowed != "" {
		for _, m := range strings.Split(*allowed, ",") {
			if m = strings.TrimSpace(m); m != "" {
				params.AllowedModels = append(params.AllowedModels, m)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewApiKeyRepo(pool)
	id, err := repo.Insert(ctx, params)
	if err != nil {
		return err
	}

	fmt.Printf("api key created\n  id:    %d\n  name:  %s\n  enabled: %t\n", id, *name, !*disabled)
	if *raw == "" {
		fmt.Println()
		fmt.Println("---- KEY (shown once, store it somewhere safe) ----")
		fmt.Println(cleartext)
		fmt.Println("---------------------------------------------------")
	}
	return nil
}

func keyList(args []string) error {
	fs := flag.NewFlagSet("key list", flag.ExitOnError)
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

	repo := repository.NewApiKeyRepo(pool)
	keys, err := repo.ListAll(ctx)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		fmt.Println("no api keys. Create one with: omnihub key add --name=...")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tLABEL\tENABLED\tDAILY_USD\tRPM\tMODELS")
	for _, k := range keys {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			k.ID,
			k.Name,
			defaultStr(k.Label, "(name)"),
			yesNo(k.Enabled),
			usdStr(k.DailyUSDLimit),
			intStr(k.RPMLimit),
			modelsStr(k.AllowedModels),
		)
	}
	return tw.Flush()
}

func keyToggle(args []string, enabled bool) error {
	if len(args) < 1 || args[0] == "" {
		return errors.New("usage: omnihub key enable|disable <name>")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewApiKeyRepo(pool)
	if err := repo.SetEnabled(ctx, name, enabled); err != nil {
		return err
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("api key %q is now %s. NOTIFY refreshes the gateway within < 1 s.\n", name, state)
	return nil
}

func keyDelete(args []string) error {
	if len(args) < 1 || args[0] == "" {
		return errors.New("usage: omnihub key delete <name>")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewApiKeyRepo(pool)
	if err := repo.Delete(ctx, name); err != nil {
		return err
	}
	fmt.Printf("api key %q deleted\n", name)
	return nil
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func usdStr(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *p)
}

func intStr(p *int) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *p)
}

func modelsStr(ms []string) string {
	if len(ms) == 0 {
		return "(all)"
	}
	return strings.Join(ms, ",")
}
