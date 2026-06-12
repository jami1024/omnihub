// Command omnihub-cli is the OmniHub client-side companion tool
// (design doc §10 / phase 7): local configuration, the X-OmniHub-*
// client protocol headers, and gateway diagnostics.
//
//	omnihub-cli config set base_url https://gw.example.com
//	omnihub-cli config set api_key sk-omnihub-...
//	omnihub-cli config show
//	omnihub-cli doctor
//	omnihub-cli models
//
// Identity rules (deliberate): install_id is random on first run,
// stored in the local config, never derived from hardware or user
// names, and the user can reset it by deleting the config file. The
// session id is random per invocation and carries no user information.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const cliVersion = "0.1.0"

// config is ~/.config/omnihub/config.json.
type config struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	InstallID string `json:"install_id"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "config":
		err = cmdConfig(os.Args[2:])
	case "doctor":
		err = cmdDoctor()
	case "models":
		err = cmdModels()
	case "version":
		fmt.Printf("omnihub-cli %s (%s/%s)\n", cliVersion, runtime.GOOS, runtime.GOARCH)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `omnihub-cli — OmniHub gateway companion

usage:
  omnihub-cli config set <base_url|api_key> <value>
  omnihub-cli config show
  omnihub-cli doctor      check connectivity, auth and capabilities
  omnihub-cli models      list models served by the gateway
  omnihub-cli version`)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(dir, "omnihub", "config.json"), nil
}

// loadConfig reads the config, creating it (with a fresh install id)
// on first run.
func loadConfig() (*config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	cfg := &config{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if jerr := json.Unmarshal(raw, cfg); jerr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, jerr)
		}
	case os.IsNotExist(err):
		// first run
	default:
		return nil, err
	}
	if cfg.InstallID == "" {
		cfg.InstallID = "inst_" + randomHex(13)
		if err := saveConfig(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func saveConfig(cfg *config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func cmdConfig(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("config needs a subcommand")
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: omnihub-cli config set <base_url|api_key> <value>")
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		switch args[1] {
		case "base_url":
			cfg.BaseURL = strings.TrimRight(strings.TrimSpace(args[2]), "/")
		case "api_key":
			cfg.APIKey = strings.TrimSpace(args[2])
		default:
			return fmt.Errorf("unknown config key %q (base_url | api_key)", args[1])
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Println("saved.")
		return nil
	case "show":
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		path, _ := configPath()
		fmt.Printf("config:     %s\nbase_url:   %s\napi_key:    %s\ninstall_id: %s\n",
			path, valueOrUnset(cfg.BaseURL), maskKey(cfg.APIKey), cfg.InstallID)
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

// cmdDoctor runs the connectivity diagnostics: /healthz (no auth),
// then the auth-guarded capability negotiation endpoint.
func cmdDoctor() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.BaseURL == "" {
		return fmt.Errorf("base_url not configured — run: omnihub-cli config set base_url <url>")
	}
	client := newClient(cfg)

	fmt.Printf("gateway:  %s\n", cfg.BaseURL)

	// 1) Reachability, no auth.
	status, _, err := client.get("/healthz", false)
	if err != nil {
		fmt.Printf("healthz:  FAIL (%v)\n", err)
		return fmt.Errorf("gateway unreachable")
	}
	fmt.Printf("healthz:  ok (HTTP %d)\n", status)

	// 2) Auth + capability negotiation.
	if cfg.APIKey == "" {
		fmt.Println("auth:     SKIP (api_key not configured)")
		return nil
	}
	status, body, err := client.get("/v1/omnihub/capabilities", true)
	if err != nil {
		fmt.Printf("auth:     FAIL (%v)\n", err)
		return fmt.Errorf("capabilities request failed")
	}
	if status != http.StatusOK {
		fmt.Printf("auth:     FAIL (HTTP %d): %s\n", status, truncate(string(body), 200))
		return fmt.Errorf("virtual key rejected")
	}
	var caps struct {
		ServerVersion string   `json:"server_version"`
		Protocols     []string `json:"protocols"`
		Features      []string `json:"features"`
	}
	if err := json.Unmarshal(body, &caps); err != nil {
		return fmt.Errorf("decode capabilities: %w", err)
	}
	fmt.Printf("auth:     ok\nserver:   %s\nprotocols: %s\nfeatures:  %s\n",
		caps.ServerVersion, strings.Join(caps.Protocols, ", "), strings.Join(caps.Features, ", "))
	return nil
}

func cmdModels() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return fmt.Errorf("configure base_url and api_key first (omnihub-cli config set ...)")
	}
	status, body, err := newClient(cfg).get("/v1/models", true)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", status, truncate(string(body), 200))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("decode models: %w", err)
	}
	for _, m := range out.Data {
		fmt.Println(m.ID)
	}
	return nil
}

// cliClient sends requests with the X-OmniHub-* client protocol
// headers. The session id is per-invocation; request ids are per call.
type cliClient struct {
	cfg       *config
	http      *http.Client
	sessionID string
}

func newClient(cfg *config) *cliClient {
	return &cliClient{
		cfg:       cfg,
		http:      &http.Client{Timeout: 15 * time.Second},
		sessionID: "sess_" + randomHex(13),
	}
}

func (c *cliClient) get(path string, withAuth bool) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.cfg.BaseURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("OmniHubCLI/%s (%s; %s)", cliVersion, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-OmniHub-Client", "cli")
	req.Header.Set("X-OmniHub-Client-Version", cliVersion)
	req.Header.Set("X-OmniHub-Client-Platform", runtime.GOOS+"/"+runtime.GOARCH)
	req.Header.Set("X-OmniHub-Client-Mode", "interactive")
	req.Header.Set("X-OmniHub-Session-ID", c.sessionID)
	req.Header.Set("X-OmniHub-Request-ID", "req_"+randomHex(13))
	req.Header.Set("X-OmniHub-Install-ID", c.cfg.InstallID)
	req.Header.Set("X-OmniHub-Capabilities", "streaming,tools,vision,thinking")
	if withAuth {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, err
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is unrecoverable for id generation.
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func maskKey(k string) string {
	if k == "" {
		return "(unset)"
	}
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "…" + k[len(k)-4:]
}

func valueOrUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
