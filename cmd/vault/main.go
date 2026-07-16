// vault is the albear CLI: administration and terminal workflows over the
// Noise-encrypted daemon socket.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/m7medVision/albear/internal/adapters/protocol"
	"github.com/m7medVision/albear/internal/client"
	"github.com/m7medVision/albear/internal/infrastructure/system"
	"github.com/m7medVision/albear/internal/install"
	"github.com/m7medVision/albear/internal/update"
	"github.com/m7medVision/albear/internal/version"
)

// Exit codes (PRD 14.4).
const (
	exitOK          = 0
	exitUsage       = 2
	exitLocked      = 3
	exitAuth        = 4
	exitNotFound    = 5
	exitDenied      = 6
	exitIntegrity   = 7
	exitUnavailable = 8
	exitConflict    = 9
	exitInternal    = 10
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	if cmd == "version" || cmd == "--version" {
		return cmdVersion(rest)
	}

	// Refresh the update cache (at most once per day) while the command runs;
	// the passive notice after a successful command reads the result.
	upd := update.New(version.Version).Background()
	code := dispatch(cmd, rest)
	if code == exitOK {
		upd.Notice(os.Stderr)
	}
	return code
}

func dispatch(cmd string, rest []string) int {
	switch cmd {
	case "init":
		return cmdInit(rest)
	case "status":
		return cmdStatus(rest)
	case "unlock":
		return cmdUnlock(rest)
	case "lock":
		return simpleOp("vault.lock")
	case "panic-lock":
		return simpleOp("vault.panic")
	case "add":
		return cmdAdd(rest)
	case "list":
		return cmdList(rest)
	case "search":
		return cmdSearch(rest)
	case "show":
		return cmdShow(rest)
	case "edit":
		return cmdEdit(rest)
	case "remove":
		return cmdRemove(rest)
	case "generate":
		return cmdGenerate(rest)
	case "password":
		return cmdPassword(rest)
	case "clients":
		return cmdClients(rest)
	case "backup":
		return cmdBackup(rest)
	case "events":
		return cmdEvents(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "install":
		return cmdInstall(rest)
	case "destroy":
		return cmdDestroy(rest)
	case "help", "--help", "-h":
		usage()
		return exitOK
	}
	fmt.Fprintf(os.Stderr, "vault: unknown command %q\n", cmd)
	usage()
	return exitUsage
}

func usage() {
	fmt.Fprint(os.Stderr, `albear (البير) — local encrypted secrets manager

Usage:
  vault init | status | unlock | lock | panic-lock
  vault add login|note|api-key [flags]
  vault list | search <query> | show <record> [--reveal] [--json]
  vault edit <record> [flags] | remove <record>
  vault generate password [--length N]
  vault password change
  vault clients list|approve|revoke
  vault backup create|verify|restore <path>
  vault events [--limit N]
  vault install <browser> [--native-host PATH] [--extension-dir PATH] [--print-only]
  vault doctor
  vault destroy
  vault version
`)
}

// ---- version -------------------------------------------------------------

func cmdVersion(args []string) int {
	fmt.Println("vault", version.Version)
	chk := update.New(version.Version)
	if !chk.Enabled() {
		return exitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), update.HTTPTimeout)
	defer cancel()
	rel, err := chk.CheckNow(ctx)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "vault: update check failed:", err)
	case version.IsNewer(rel.Tag, version.Version):
		fmt.Printf("update available: %s -> %s — %s\n", version.Version, rel.Tag, rel.URL)
	default:
		fmt.Println("up to date")
	}
	return exitOK
}

// ---- plumbing ----------------------------------------------------------

func connect() (*client.Client, int) {
	paths, err := system.ResolvePaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vault:", err)
		return nil, exitInternal
	}
	c, err := client.DialCLI(paths.Socket())
	if err != nil {
		fmt.Fprintln(os.Stderr, "vault: daemon unavailable — is vaultd running?")
		return nil, exitUnavailable
	}
	return c, exitOK
}

func exitCodeFor(err error) int {
	var ae *client.APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case protocol.CodeVaultLocked:
			return exitLocked
		case protocol.CodeAuthFailed, protocol.CodeRateLimited:
			return exitAuth
		case protocol.CodeNotFound, protocol.CodeUninitialized:
			return exitNotFound
		case protocol.CodeDenied:
			return exitDenied
		case protocol.CodeIntegrity:
			return exitIntegrity
		case protocol.CodeConflict:
			return exitConflict
		case protocol.CodeInvalid:
			return exitUsage
		}
		return exitInternal
	}
	if errors.Is(err, client.ErrDaemonUnavailable) {
		return exitUnavailable
	}
	return exitInternal
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "vault:", err)
	return exitCodeFor(err)
}

func call(op string, payload, out any) (int, error) {
	c, code := connect()
	if c == nil {
		return code, errors.New("unavailable")
	}
	defer c.Close()
	if err := c.Call(op, payload, out); err != nil {
		return exitCodeFor(err), err
	}
	return exitOK, nil
}

func simpleOp(op string) int {
	if code, err := call(op, nil, nil); err != nil {
		return fail(err)
	} else if code != exitOK {
		return code
	}
	fmt.Println("ok")
	return exitOK
}

// parseAnywhere parses fs against args with flags allowed after positional
// arguments (Go's flag package stops at the first positional otherwise).
func parseAnywhere(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// readPassword prompts on the terminal; never from arguments (PRD 12.3).
func readPassword(prompt string, allowStdin bool) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(pw), err
	}
	if !allowStdin {
		return "", errors.New("refusing to read password from a non-terminal; use --password-stdin")
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func promptLine(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

// ---- vault lifecycle ---------------------------------------------------

func cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	stdin := fs.Bool("password-stdin", false, "read the master password from stdin")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	pw, err := readPassword("New master password: ", *stdin)
	if err != nil {
		return fail(err)
	}
	if !*stdin {
		confirm, err := readPassword("Repeat master password: ", false)
		if err != nil || pw != confirm {
			fmt.Fprintln(os.Stderr, "vault: passwords do not match")
			return exitUsage
		}
	}
	if code, err := call("vault.init", map[string]string{"password": pw}, nil); err != nil {
		return fail(err)
	} else if code != exitOK {
		return code
	}
	fmt.Println("Vault created. There is NO recovery if you forget the master password.")
	return exitOK
}

func cmdStatus(args []string) int {
	var st struct {
		Initialized bool   `json:"initialized"`
		Unlocked    bool   `json:"unlocked"`
		Epoch       uint64 `json:"epoch"`
		RecordCount int64  `json:"recordCount"`
	}
	if _, err := call("vault.status", nil, &st); err != nil {
		return fail(err)
	}
	switch {
	case !st.Initialized:
		fmt.Println("state: uninitialized (run `vault init`)")
	case st.Unlocked:
		fmt.Printf("state: unlocked  records: %d\n", st.RecordCount)
	default:
		fmt.Println("state: locked")
	}
	return exitOK
}

func cmdUnlock(args []string) int {
	fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
	stdin := fs.Bool("password-stdin", false, "read the master password from stdin")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	pw, err := readPassword("Master password: ", *stdin)
	if err != nil {
		return fail(err)
	}
	if code, err := call("vault.unlock", map[string]string{"password": pw}, nil); err != nil {
		return fail(err)
	} else if code != exitOK {
		return code
	}
	fmt.Println("unlocked")
	return exitOK
}

// ---- records -----------------------------------------------------------

type recordFlags struct {
	fs        *flag.FlagSet
	name      *string
	username  *string
	service   *string
	env       *string
	urls      *string
	tags      *string
	notes     *string
	apiKey    *string
	apiSecret *string
	password  *string
	genPw     *bool
}

func newRecordFlags(name string) *recordFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return &recordFlags{
		fs:        fs,
		name:      fs.String("name", "", "record name"),
		username:  fs.String("username", "", "username"),
		service:   fs.String("service", "", "service (api-key)"),
		env:       fs.String("environment", "", "environment (api-key)"),
		urls:      fs.String("url", "", "comma-separated URLs"),
		tags:      fs.String("tags", "", "comma-separated tags"),
		notes:     fs.String("notes", "", "notes body"),
		apiKey:    fs.String("api-key", "", "API key value"),
		apiSecret: fs.String("api-secret", "", "API secret value"),
		password:  fs.String("password", "", "password value (prefer --generate or prompt)"),
		genPw:     fs.Bool("generate", false, "generate the password"),
	}
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (rf *recordFlags) payload(recordType string) (map[string]any, error) {
	p := map[string]any{"type": recordType, "name": *rf.name}
	if *rf.username != "" {
		p["username"] = *rf.username
	}
	if *rf.service != "" {
		p["service"] = *rf.service
	}
	if *rf.env != "" {
		p["environment"] = *rf.env
	}
	if urls := splitList(*rf.urls); urls != nil {
		p["urls"] = urls
	}
	if tags := splitList(*rf.tags); tags != nil {
		p["tags"] = tags
	}
	if *rf.notes != "" {
		p["notes"] = *rf.notes
	}
	if *rf.apiKey != "" {
		p["apiKey"] = *rf.apiKey
	}
	if *rf.apiSecret != "" {
		p["apiSecret"] = *rf.apiSecret
	}
	if *rf.password != "" {
		p["password"] = *rf.password
	}
	return p, nil
}

func cmdAdd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "vault: add login|note|api-key")
		return exitUsage
	}
	kind := args[0]
	rf := newRecordFlags("add " + kind)
	if rf.fs.Parse(args[1:]) != nil {
		return exitUsage
	}

	typeName := map[string]string{"login": "login", "note": "note", "api-key": "api"}[kind]
	if typeName == "" {
		fmt.Fprintln(os.Stderr, "vault: add login|note|api-key")
		return exitUsage
	}

	if *rf.name == "" {
		*rf.name = promptLine("Name: ")
	}
	if typeName == "login" && *rf.password == "" && !*rf.genPw {
		pw, err := readPassword("Password (empty to generate): ", false)
		if err != nil {
			return fail(err)
		}
		if pw == "" {
			*rf.genPw = true
		} else {
			*rf.password = pw
		}
	}

	c, code := connect()
	if c == nil {
		return code
	}
	defer c.Close()

	if *rf.genPw {
		var gen struct {
			Password string `json:"password"`
		}
		if err := c.Call("password.generate", map[string]bool{"default": true}, &gen); err != nil {
			return fail(err)
		}
		*rf.password = gen.Password
	}

	payload, err := rf.payload(typeName)
	if err != nil {
		return fail(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.Call("records.create", payload, &created); err != nil {
		return fail(err)
	}
	fmt.Printf("created %s\n", created.ID)
	return exitOK
}

type recordView struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Revision uint64   `json:"revision"`
	Name     string   `json:"name"`
	Username string   `json:"username"`
	Service  string   `json:"service"`
	URLs     []string `json:"urls"`
	Tags     []string `json:"tags"`
}

func printRecords(records []recordView) {
	for _, r := range records {
		extra := r.Username
		if extra == "" {
			extra = r.Service
		}
		urls := ""
		if len(r.URLs) > 0 {
			urls = r.URLs[0]
		}
		fmt.Printf("%-8s %-6s %-24s %-20s %s\n", r.ID[:8], r.Type, r.Name, extra, urls)
	}
}

func cmdList(args []string) int {
	var out struct {
		Records []recordView `json:"records"`
	}
	if _, err := call("records.list", nil, &out); err != nil {
		return fail(err)
	}
	printRecords(out.Records)
	return exitOK
}

func cmdSearch(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "vault: search <query>")
		return exitUsage
	}
	var out struct {
		Records []recordView `json:"records"`
	}
	if _, err := call("records.search", map[string]string{"query": strings.Join(args, " ")}, &out); err != nil {
		return fail(err)
	}
	printRecords(out.Records)
	return exitOK
}

func cmdShow(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	reveal := fs.Bool("reveal", false, "reveal secret values")
	asJSON := fs.Bool("json", false, "JSON output")
	positional, err := parseAnywhere(fs, args)
	if err != nil || len(positional) < 1 {
		fmt.Fprintln(os.Stderr, "vault: show <record> [--reveal] [--json]")
		return exitUsage
	}
	ref := positional[0]

	c, code := connect()
	if c == nil {
		return code
	}
	defer c.Close()

	var meta map[string]any
	if err := c.Call("records.show", map[string]string{"ref": ref}, &meta); err != nil {
		return fail(err)
	}

	// Redaction by default: secrets appear only with --reveal (PRD 14.2),
	// in both human and JSON output.
	if *reveal {
		var secret map[string]any
		if err := c.Call("records.reveal", map[string]string{"ref": ref}, &secret); err != nil {
			return fail(err)
		}
		meta["secret"] = secret
	} else {
		meta["secret"] = map[string]any{"password": "[redacted]"}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(meta)
		return exitOK
	}
	for _, k := range []string{"id", "type", "revision", "name", "username", "service", "environment"} {
		if v, ok := meta[k]; ok && v != "" && v != nil {
			fmt.Printf("%-12s %v\n", k+":", v)
		}
	}
	if urls, ok := meta["urls"].([]any); ok {
		for _, u := range urls {
			fmt.Printf("%-12s %v\n", "url:", u)
		}
	}
	if secret, ok := meta["secret"].(map[string]any); ok {
		for k, v := range secret {
			if v != "" && v != nil {
				fmt.Printf("%-12s %v\n", k+":", v)
			}
		}
	}
	return exitOK
}

func cmdEdit(args []string) int {
	rf := newRecordFlags("edit")
	positional, err := parseAnywhere(rf.fs, args)
	if err != nil || len(positional) < 1 {
		fmt.Fprintln(os.Stderr, "vault: edit <record> [--name ...] [--username ...] ...")
		return exitUsage
	}
	ref := positional[0]

	c, code := connect()
	if c == nil {
		return code
	}
	defer c.Close()

	var meta struct {
		recordView
		Environment string `json:"environment"`
	}
	if err := c.Call("records.show", map[string]string{"ref": ref}, &meta); err != nil {
		return fail(err)
	}
	var secret map[string]any
	if err := c.Call("records.reveal", map[string]string{"ref": ref}, &secret); err != nil {
		return fail(err)
	}

	// Merge: explicit flags override, everything else is preserved.
	pick := func(flagVal, current string) string {
		if flagVal != "" {
			return flagVal
		}
		return current
	}
	getStr := func(k string) string {
		if v, ok := secret[k].(string); ok {
			return v
		}
		return ""
	}
	payload := map[string]any{
		"id":               meta.ID,
		"expectedRevision": meta.Revision,
		"type":             meta.Type,
		"name":             pick(*rf.name, meta.Name),
		"username":         pick(*rf.username, meta.Username),
		"service":          pick(*rf.service, meta.Service),
		"environment":      pick(*rf.env, meta.Environment),
		"password":         pick(*rf.password, getStr("password")),
		"notes":            pick(*rf.notes, getStr("notes")),
		"apiKey":           pick(*rf.apiKey, getStr("apiKey")),
		"apiSecret":        pick(*rf.apiSecret, getStr("apiSecret")),
	}
	if urls := splitList(*rf.urls); urls != nil {
		payload["urls"] = urls
	} else {
		payload["urls"] = meta.URLs
	}
	if tags := splitList(*rf.tags); tags != nil {
		payload["tags"] = tags
	} else {
		payload["tags"] = meta.Tags
	}

	if err := c.Call("records.update", payload, nil); err != nil {
		return fail(err)
	}
	fmt.Println("updated")
	return exitOK
}

func cmdRemove(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "vault: remove <record>")
		return exitUsage
	}
	c, code := connect()
	if c == nil {
		return code
	}
	defer c.Close()

	var meta recordView
	if err := c.Call("records.show", map[string]string{"ref": args[0]}, &meta); err != nil {
		return fail(err)
	}
	if err := c.Call("records.delete", map[string]string{"id": meta.ID}, nil); err != nil {
		return fail(err)
	}
	fmt.Println("removed", meta.Name)
	return exitOK
}

func cmdGenerate(args []string) int {
	if len(args) < 1 || args[0] != "password" {
		fmt.Fprintln(os.Stderr, "vault: generate password [--length N]")
		return exitUsage
	}
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	length := fs.Int("length", 24, "password length")
	if fs.Parse(args[1:]) != nil {
		return exitUsage
	}
	var out struct {
		Password string `json:"password"`
	}
	if _, err := call("password.generate", map[string]any{
		"length": *length, "upper": true, "lower": true, "digits": true, "symbols": true,
	}, &out); err != nil {
		return fail(err)
	}
	fmt.Println(out.Password)
	return exitOK
}

func cmdPassword(args []string) int {
	if len(args) < 1 || args[0] != "change" {
		fmt.Fprintln(os.Stderr, "vault: password change")
		return exitUsage
	}
	current, err := readPassword("Current master password: ", false)
	if err != nil {
		return fail(err)
	}
	next, err := readPassword("New master password: ", false)
	if err != nil {
		return fail(err)
	}
	confirm, err := readPassword("Repeat new master password: ", false)
	if err != nil || next != confirm {
		fmt.Fprintln(os.Stderr, "vault: passwords do not match")
		return exitUsage
	}
	if code, err := call("vault.changePassword", map[string]string{"current": current, "next": next}, nil); err != nil {
		return fail(err)
	} else if code != exitOK {
		return code
	}
	fmt.Println("Master password changed. The vault is now locked.")
	return exitOK
}

// ---- clients -----------------------------------------------------------

func cmdClients(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "vault: clients list|approve|revoke")
		return exitUsage
	}
	switch args[0] {
	case "list":
		var out struct {
			Clients []struct {
				ID     string `json:"id"`
				Kind   int    `json:"kind"`
				Status int    `json:"status"`
				Label  string `json:"label"`
			} `json:"clients"`
		}
		if _, err := call("clients.list", nil, &out); err != nil {
			return fail(err)
		}
		kinds := map[int]string{1: "cli", 2: "chrome", 3: "bridge", 4: "admin"}
		statuses := map[int]string{1: "pending", 2: "approved", 3: "revoked"}
		for _, cl := range out.Clients {
			fmt.Printf("%s  %-7s %-9s %s\n", cl.ID, kinds[cl.Kind], statuses[cl.Status], cl.Label)
		}
		return exitOK

	case "approve":
		var pending struct {
			Pending []struct {
				PairingID string `json:"pairingId"`
				Label     string `json:"label"`
				Phrase    string `json:"phrase"`
			} `json:"pending"`
		}
		if _, err := call("clients.pending", nil, &pending); err != nil {
			return fail(err)
		}
		if len(pending.Pending) == 0 {
			fmt.Println("no pending pairing requests")
			return exitOK
		}
		p := pending.Pending[0]
		fmt.Printf("Pairing request: %s\nPhrase: %s\n", p.Label, p.Phrase)
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintln(os.Stderr, "vault: approval requires an interactive terminal")
			return exitDenied
		}
		if answer := promptLine("Does the phrase match what the client shows? [y/N] "); answer != "y" && answer != "Y" {
			fmt.Println("aborted")
			return exitOK
		}
		if _, err := call("clients.approve", map[string]string{"pairingId": p.PairingID}, nil); err != nil {
			return fail(err)
		}
		fmt.Println("approved")
		return exitOK

	case "revoke":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "vault: clients revoke <client-id>")
			return exitUsage
		}
		if _, err := call("clients.revoke", map[string]string{"id": args[1]}, nil); err != nil {
			return fail(err)
		}
		fmt.Println("revoked")
		return exitOK
	}
	fmt.Fprintln(os.Stderr, "vault: clients list|approve|revoke")
	return exitUsage
}

// ---- backup ------------------------------------------------------------

func cmdBackup(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "vault: backup create|verify|restore <path>")
		return exitUsage
	}
	op, path := args[0], args[1]
	switch op {
	case "create":
		if _, err := call("backup.create", map[string]string{"path": path}, nil); err != nil {
			return fail(err)
		}
		fmt.Println("backup written to", path)
		return exitOK
	case "verify":
		var out struct {
			VaultID     string `json:"vaultId"`
			CreatedAtMs uint64 `json:"createdAtMs"`
		}
		if _, err := call("backup.verify", map[string]string{"path": path}, &out); err != nil {
			return fail(err)
		}
		fmt.Printf("backup OK  vault=%s created=%d\n", out.VaultID, out.CreatedAtMs)
		return exitOK
	case "restore":
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintln(os.Stderr, "vault: restore requires an interactive terminal")
			return exitDenied
		}
		fmt.Println("Restore REPLACES the current vault with the backup contents.")
		if answer := promptLine("Type 'restore' to continue: "); answer != "restore" {
			fmt.Println("aborted")
			return exitOK
		}
		if _, err := call("backup.restore", map[string]string{"path": path}, nil); err != nil {
			return fail(err)
		}
		fmt.Println("restored; the vault is locked")
		return exitOK
	}
	fmt.Fprintln(os.Stderr, "vault: backup create|verify|restore <path>")
	return exitUsage
}

func cmdEvents(args []string) int {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	limit := fs.Int("limit", 25, "number of events")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	var out struct {
		Events []struct {
			OccurredMs int64  `json:"occurredMs"`
			Severity   int    `json:"severity"`
			Code       int    `json:"code"`
			Details    string `json:"details"`
		} `json:"events"`
	}
	if _, err := call("events.recent", map[string]int{"limit": *limit}, &out); err != nil {
		return fail(err)
	}
	for _, e := range out.Events {
		fmt.Printf("%d  sev=%d code=%d %s\n", e.OccurredMs, e.Severity, e.Code, e.Details)
	}
	return exitOK
}

func cmdDoctor(args []string) int {
	paths, err := system.ResolvePaths()
	if err != nil {
		return fail(err)
	}
	fmt.Println("database: ", paths.Database())
	fmt.Println("config:   ", paths.ConfigDir)
	fmt.Println("socket:   ", paths.Socket())
	for _, p := range []string{paths.Database(), paths.StaticKey()} {
		if err := system.CheckPrivate(p); err != nil {
			fmt.Println("warn:     ", err)
		}
	}
	c, code := connect()
	if c == nil {
		fmt.Println("daemon:    unavailable")
		return code
	}
	defer c.Close()
	var st struct {
		Initialized bool `json:"initialized"`
		Unlocked    bool `json:"unlocked"`
	}
	if err := c.Call("vault.status", nil, &st); err != nil {
		return fail(err)
	}
	fmt.Printf("daemon:    up  initialized=%v unlocked=%v\n", st.Initialized, st.Unlocked)
	return exitOK
}

// ---- install -----------------------------------------------------------

func cmdInstall(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "vault: install <browser> [--native-host PATH] [--extension-dir PATH] [--print-only]")
		return exitUsage
	}
	name := args[0]
	strategy, err := install.Get(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vault:", err)
		return exitUsage
	}
	fs := flag.NewFlagSet("install "+name, flag.ContinueOnError)
	nativeHost := fs.String("native-host", "", "path to vault-native")
	extensionDir := fs.String("extension-dir", "", "path to built extension directory")
	printOnly := fs.Bool("print-only", false, "validate and print install paths without writing")
	if fs.Parse(args[1:]) != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "vault: install", name, "does not accept positional arguments")
		return exitUsage
	}
	result, err := install.Install(strategy, install.Options{
		NativeHostPath: *nativeHost,
		ExtensionDir:   *extensionDir,
		PrintOnly:      *printOnly,
	})
	if err != nil {
		return fail(err)
	}
	label := strings.ToUpper(name[:1]) + name[1:]
	if result.WroteManifest {
		fmt.Println(label, "native host installed:", result.ManifestPath)
	} else {
		fmt.Println(label, "native host manifest:", result.ManifestPath)
	}
	fmt.Println("vault-native:", result.NativeHostPath)
	fmt.Println("extension ID:", result.ExtensionID)
	fmt.Println("extension dir:", result.ExtensionDir)
	fmt.Println()
	fmt.Println("Open the browser's extensions page, enable Developer mode, choose Load unpacked, then select:")
	fmt.Println(result.ExtensionDir)
	return exitOK
}

// ---- destroy -----------------------------------------------------------

func cmdDestroy(args []string) int {
	// Interactive-only by default (PRD 19.1 Level 4, acceptance 18).
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "vault: destroy requires an interactive terminal")
		return exitDenied
	}
	fmt.Println("This PERMANENTLY deletes the vault. There is no recovery without a backup.")
	fmt.Println("Note: SSD wear leveling may keep older copies of the encrypted file.")

	pw, err := readPassword("Master password: ", false)
	if err != nil {
		return fail(err)
	}
	if answer := promptLine("Type 'destroy my vault' to continue: "); answer != "destroy my vault" {
		fmt.Println("aborted")
		return exitOK
	}
	if _, err := call("vault.destroy", map[string]string{"password": pw}, nil); err != nil {
		return fail(err)
	}
	fmt.Println("vault destroyed")
	return exitOK
}
