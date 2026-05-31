package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/admin"
)

// runAdminCommand dispatches the `omnihub admin ...` subcommands. These
// manage the login accounts for the web admin UI — distinct from
// `omnihub key ...` (gateway-traffic virtual keys).
func runAdminCommand(args []string) {
	if len(args) == 0 {
		printAdminUsage(os.Stderr)
		os.Exit(2)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		exitOnErr(adminAdd(rest))
	case "list", "ls":
		exitOnErr(adminList(rest))
	case "enable":
		exitOnErr(adminToggle(rest, true))
	case "disable":
		exitOnErr(adminToggle(rest, false))
	case "passwd":
		exitOnErr(adminPasswd(rest))
	case "delete", "rm":
		exitOnErr(adminDelete(rest))
	case "help", "-h", "--help":
		printAdminUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown admin subcommand: %s\n\n", cmd)
		printAdminUsage(os.Stderr)
		os.Exit(2)
	}
}

func printAdminUsage(w io.Writer) {
	fmt.Fprint(w, `omnihub admin — manage web UI login accounts

Usage:
  omnihub admin add      [flags]      Create a new admin account.
  omnihub admin list                   List every admin (any state).
  omnihub admin enable   <username>    Mark an admin enabled.
  omnihub admin disable  <username>    Mark an admin disabled.
  omnihub admin passwd   <username>    Change an admin's password.
  omnihub admin delete   <username>    Hard-delete an admin.

'add' flags:
  --username=NAME       (required) unique login handle
  --password=PWD        (optional) password; if omitted the CLI reads
                        it from the terminal with echo disabled
  --disabled            create in disabled state

Admin accounts authenticate the web UI at /admin. They are NOT virtual
API keys — gateway traffic still uses 'omnihub key add'.

The CLI shares OMNIHUB_DATABASE_URL with the gateway.
`)
}

func adminAdd(args []string) error {
	fs := flag.NewFlagSet("admin add", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	var (
		username = fs.String("username", "", "unique login handle")
		password = fs.String("password", "", "password (prompted if omitted)")
		disabled = fs.Bool("disabled", false, "create in disabled state")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	*username = strings.TrimSpace(*username)
	if *username == "" {
		fs.Usage()
		return errors.New("missing required flag: --username")
	}

	cleartext := *password
	if cleartext == "" {
		pw, err := readPasswordTwice(fmt.Sprintf("password for %q", *username))
		if err != nil {
			return err
		}
		cleartext = pw
	}
	if cleartext == "" {
		return errors.New("password must not be empty")
	}

	hash, err := admin.HashPassword(cleartext)
	if err != nil {
		return fmt.Errorf("bcrypt: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewAdminUserRepo(pool)
	id, err := repo.Insert(ctx, repository.AdminUserInsertParams{
		Username:     *username,
		PasswordHash: hash,
		Enabled:      !*disabled,
	})
	if err != nil {
		return err
	}
	fmt.Printf("admin user created\n  id:       %d\n  username: %s\n  enabled:  %t\n",
		id, *username, !*disabled)
	return nil
}

func adminList(args []string) error {
	fs := flag.NewFlagSet("admin list", flag.ExitOnError)
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

	repo := repository.NewAdminUserRepo(pool)
	users, err := repo.ListAll(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("no admin users. Create one with: omnihub admin add --username=...")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tUSERNAME\tENABLED\tCREATED")
	for _, u := range users {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n",
			u.ID,
			u.Username,
			yesNo(u.Enabled),
			u.CreatedAt.UTC().Format(time.RFC3339),
		)
	}
	return tw.Flush()
}

func adminToggle(args []string, enabled bool) error {
	if len(args) < 1 || args[0] == "" {
		return errors.New("usage: omnihub admin enable|disable <username>")
	}
	username := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewAdminUserRepo(pool)
	if err := repo.SetEnabled(ctx, username, enabled); err != nil {
		return err
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("admin user %q is now %s. New JWTs cannot be issued; existing JWTs remain valid until they expire.\n",
		username, state)
	return nil
}

func adminPasswd(args []string) error {
	if len(args) < 1 || args[0] == "" {
		return errors.New("usage: omnihub admin passwd <username>")
	}
	username := args[0]

	cleartext, err := readPasswordTwice(fmt.Sprintf("new password for %q", username))
	if err != nil {
		return err
	}
	if cleartext == "" {
		return errors.New("password must not be empty")
	}
	hash, err := admin.HashPassword(cleartext)
	if err != nil {
		return fmt.Errorf("bcrypt: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewAdminUserRepo(pool)
	if err := repo.UpdatePassword(ctx, username, hash); err != nil {
		return err
	}
	fmt.Printf("password updated for %q\n", username)
	return nil
}

func adminDelete(args []string) error {
	if len(args) < 1 || args[0] == "" {
		return errors.New("usage: omnihub admin delete <username>")
	}
	username := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := openDBForCLI(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.NewAdminUserRepo(pool)
	if err := repo.Delete(ctx, username); err != nil {
		return err
	}
	fmt.Printf("admin user %q deleted\n", username)
	return nil
}

// readPasswordTwice prompts on stderr, reads a password with echo off,
// confirms it by re-reading, and returns the verified cleartext. Used
// for both `admin add` (when --password is omitted) and `admin passwd`.
func readPasswordTwice(prompt string) (string, error) {
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", errors.New("password prompt requires a terminal; pass --password instead")
	}
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	first, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "confirm: ")
	second, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("read confirmation: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	if string(first) != string(second) {
		return "", errors.New("passwords did not match")
	}
	return string(first), nil
}
