package muse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

func stubServe(t *testing.T, fn func(context.Context) (*serveConn, error)) {
	t.Helper()
	old := startServe
	startServe = fn
	t.Cleanup(func() { startServe = old })
}

// cannedServe is a host that says lines, whatever it is asked, and records
// what it was sent. With hang set its output stays open after the lines, as a
// host that stopped answering would leave it, until stop.
type cannedServe struct {
	mu      sync.Mutex
	sent    strings.Builder
	stopped bool
}

func (c *cannedServe) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent.Write(p)
}

func (c *cannedServe) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Split(strings.TrimSpace(c.sent.String()), "\n")
}

func (c *cannedServe) wasStopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopped
}

// serveLines makes Fetch ask a canned host instead of the Muse CLI.
func serveLines(t *testing.T, hang bool, stderr string, lines ...string) *cannedServe {
	t.Helper()
	t.Setenv("AIQUOKKA_MUSE_CLI_TIMEOUT", defaultServeTimeout.String())
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "auth.json"))
	c := &cannedServe{}
	stubServe(t, func(context.Context) (*serveConn, error) {
		r, w := io.Pipe()
		go func() {
			for _, line := range lines {
				if _, err := io.WriteString(w, line+"\n"); err != nil {
					return
				}
			}
			if !hang {
				_ = w.Close()
			}
		}()
		return &serveConn{
			in:  c,
			out: r,
			stop: func() {
				c.mu.Lock()
				c.stopped = true
				c.mu.Unlock()
				_ = w.Close()
			},
			diagnostics: func() string { return stderr },
		}, nil
	})
	return c
}

const (
	initializeAnswer = `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"muse","version":"1.4.4"},"grantedCapabilities":[],"experimentalApi":false}}`
	filledAnswer     = `{"jsonrpc":"2.0","id":2,"result":{"usage":{"observedAtMs":1789560000000,"tier":"muse_code_power","window":{"resetsAtMs":1789568145000,"usedPercent":24,"windowDurationMins":300},"weekly":{"resetsAtMs":1789948800000,"usedPercent":112}}}}`
)

func TestFetchFromCLIFilled(t *testing.T) {
	host := serveLines(t, false, "", initializeAnswer, filledAnswer)

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Provider != "Muse" || report.Plan != "muse_code_power" {
		t.Fatalf("provider/plan = %q/%q, want Muse and the tier", report.Provider, report.Plan)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("Windows = %+v, want two", report.Windows)
	}
	five, weekly := report.Windows[0], report.Windows[1]
	if five.ID != "5h" || five.Label != "5h" || five.Duration != 5*time.Hour {
		t.Fatalf("rolling window = %+v, want the 5h window", five)
	}
	if five.UsedPercent == nil || *five.UsedPercent != 24 {
		t.Fatalf("rolling used = %v, want 24", five.UsedPercent)
	}
	if want := time.Unix(1789568145, 0).UTC(); !five.ResetsAt.Equal(want) {
		t.Fatalf("rolling resets = %v, want %v", five.ResetsAt, want)
	}
	if weekly.ID != "weekly" || weekly.Label != "Weekly" || weekly.Duration != 7*24*time.Hour {
		t.Fatalf("weekly window = %+v, want the weekly window", weekly)
	}
	// Over quota is a valid answer, not one to clamp.
	if weekly.UsedPercent == nil || *weekly.UsedPercent != 112 {
		t.Fatalf("weekly used = %v, want 112", weekly.UsedPercent)
	}
	if want := time.Unix(1789948800, 0).UTC(); !weekly.ResetsAt.Equal(want) {
		t.Fatalf("weekly resets = %v, want %v", weekly.ResetsAt, want)
	}

	// The host refuses usage/read before the handshake is closed.
	reqs := host.requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %q, want initialize, initialized, usage/read", reqs)
	}
	for i, want := range []string{"initialize", "initialized", "usage/read"} {
		var req struct {
			Version string `json:"jsonrpc"`
			Method  string `json:"method"`
		}
		if err := json.Unmarshal([]byte(reqs[i]), &req); err != nil {
			t.Fatalf("request %d = %q: %v", i, reqs[i], err)
		}
		if req.Version != "2.0" || req.Method != want {
			t.Errorf("request %d = %q, want JSON-RPC 2.0 %s", i, reqs[i], want)
		}
	}
	if !host.wasStopped() {
		t.Error("the host was left running")
	}
}

func TestFetchFromCLINothingObserved(t *testing.T) {
	serveLines(t, false, "", initializeAnswer, `{"jsonrpc":"2.0","id":2,"result":{}}`)

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// muse is present and answered, so the empty report carries the macOS
	// enable steps instead of leaving the user with a bare empty section.
	if len(report.Windows) != 0 {
		t.Fatalf("report = %+v, want no windows", report)
	}
	hints := make([]string, len(report.Extra))
	for i, f := range report.Extra {
		hints[i] = f.Label + ": " + f.Value
	}
	joined := strings.Join(hints, "\n")
	for _, want := range []string{"TBH_CREDENTIAL_BACKEND=file muse login", "muse logout"} {
		if !strings.Contains(joined, want) {
			t.Errorf("hints = %q, want them to mention %q", joined, want)
		}
	}
	if strings.Contains(joined, "muse exec") {
		t.Errorf("hints = %q, want only the stable TBH fix, no observe-a-window workaround", joined)
	}
	var out strings.Builder
	usage.Render(&out, report, time.Now())
	if !strings.Contains(out.String(), "no usage windows reported") {
		t.Fatalf("rendered %q, want \"no usage windows reported\"", out.String())
	}
	if !strings.Contains(out.String(), "TBH_CREDENTIAL_BACKEND=file muse login") {
		t.Fatalf("rendered %q, want the TBH fix in the response", out.String())
	}
	// --json answers a list, empty when nothing is known, never null, with
	// the extras intact.
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"windows":[]`) {
		t.Fatalf("JSON = %s, want an empty windows list", raw)
	}
	if !strings.Contains(string(raw), "TBH_CREDENTIAL_BACKEND=file muse login") {
		t.Fatalf("JSON = %s, want the TBH fix in the extras", raw)
	}
}

func TestFetchFromCLIFilledHasNoEnableHints(t *testing.T) {
	serveLines(t, false, "", initializeAnswer, filledAnswer)

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Extra) != 0 {
		t.Fatalf("report = %+v, want windows without enable hints", report)
	}
}

func TestFetchFromCLISkipsNotifications(t *testing.T) {
	serveLines(t, false, "",
		`{"jsonrpc":"2.0","method":"session/listChanged","params":{}}`,
		initializeAnswer,
		`{"jsonrpc":"2.0","method":"usage/changed","params":{"observedAtMs":1,"tier":"other","window":{"resetsAtMs":1,"usedPercent":99,"windowDurationMins":300},"weekly":{"resetsAtMs":1,"usedPercent":99}}}`,
		`not json at all`,
		`{"jsonrpc":"2.0","id":7,"result":{"usage":null}}`,
		filledAnswer,
	)

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 2 || *report.Windows[0].UsedPercent != 24 {
		t.Fatalf("report = %+v, want the answer to usage/read, not a notification", report)
	}
}

func TestFetchFromCLIErrorAnswer(t *testing.T) {
	serveLines(t, false, "", initializeAnswer,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32603,"message":"usage store\n unavailable","data":{"kind":"internal"}}}`)

	_, err := Fetch(context.Background())
	if err == nil || usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want an error", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "usage/read") || !strings.Contains(msg, "usage store unavailable") || !strings.Contains(msg, "-32603") {
		t.Errorf("err = %q, want the method, the host's message and its code", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("err = %q, want one line", msg)
	}
	if strings.Contains(msg, "muse login") {
		t.Errorf("err = %q, must not send the user to `muse login` for a host fault", msg)
	}
}

func TestFetchFromCLIErrorOnInitialize(t *testing.T) {
	host := serveLines(t, false, "",
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"clientInfo.name is invalid","data":{"kind":"invalidParams"}}}`)

	_, err := Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("err = %v, want the refused initialize", err)
	}
	if reqs := host.requests(); len(reqs) != 1 {
		t.Fatalf("requests = %q, want nothing sent after the refusal", reqs)
	}
}

func TestFetchFromCLILoginError(t *testing.T) {
	for _, message := range []string{"Not logged in", "login required", "Authentication expired", "no credentials found"} {
		serveLines(t, false, "", initializeAnswer,
			`{"jsonrpc":"2.0","id":2,"error":{"code":-32603,"message":"`+message+`","data":{"kind":"internal"}}}`)

		_, err := Fetch(context.Background())
		if !usage.IsNotConfigured(err) {
			t.Fatalf("%s: err = %v, want NotConfigured", message, err)
		}
		if !strings.Contains(err.Error(), "run `muse login`") {
			t.Errorf("%s: err = %q, want the login hint", message, err)
		}
	}
	// The handshake's own refusal is no login fault.
	if loginFailure.MatchString("Not initialized") {
		t.Error("\"Not initialized\" reads as a login error")
	}
}

func TestFetchFromCLITimeout(t *testing.T) {
	host := serveLines(t, true, "", initializeAnswer)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := Fetch(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || usage.IsNotConfigured(err) {
			t.Fatalf("err = %v, want a timeout error", err)
		}
		msg := err.Error()
		if !strings.Contains(msg, "did not answer") {
			t.Errorf("err = %q, want it to say the host did not answer", msg)
		}
		// The caller's 50ms binds before our own wait, so the error names
		// that deadline instead of our timeout, with no env hint.
		if !strings.Contains(msg, "before the caller's deadline") {
			t.Errorf("err = %q, want it to name the caller's deadline", msg)
		}
		if !strings.Contains(msg, "waited") {
			t.Errorf("err = %q, want the elapsed wait", msg)
		}
		if strings.Contains(msg, "AIQUOKKA_MUSE_CLI_TIMEOUT") {
			t.Errorf("err = %q, must not hint at lengthening our own wait", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Fetch still waits for a host that does not answer")
	}
	if !host.wasStopped() {
		t.Error("the silent host was left running")
	}
}

// A host that never answers fails with our own wait, the elapsed time and
// the last thing it said on stderr.
func TestFetchFromCLITimeoutNamesElapsedAndStderr(t *testing.T) {
	host := serveLines(t, true, "starting up\nstill loading the model index\n", initializeAnswer)
	t.Setenv("AIQUOKKA_MUSE_CLI_TIMEOUT", "100ms")

	_, err := Fetch(context.Background())
	if err == nil || usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want a timeout error", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "did not answer within 100ms") {
		t.Errorf("err = %q, want our own wait from the env", msg)
	}
	if !strings.Contains(msg, "waited") {
		t.Errorf("err = %q, want the elapsed wait", msg)
	}
	if !strings.Contains(msg, "still loading the model index") {
		t.Errorf("err = %q, want the host's last stderr line", msg)
	}
	if strings.Contains(msg, "starting up") {
		t.Errorf("err = %q, want only the last stderr line", msg)
	}
	if !strings.Contains(msg, "AIQUOKKA_MUSE_CLI_TIMEOUT") {
		t.Errorf("err = %q, want the longer-wait hint", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("err = %q, want one line", msg)
	}
	if !host.wasStopped() {
		t.Error("the silent host was left running")
	}
}

func TestServeTimeoutFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", defaultServeTimeout},
		{"valid", "30s", 30 * time.Second},
		{"padded", "  45s ", 45 * time.Second},
		{"minutes", "2m", 2 * time.Minute},
		{"invalid", "soon", defaultServeTimeout},
		{"zero", "0", defaultServeTimeout},
		{"negative", "-5s", defaultServeTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AIQUOKKA_MUSE_CLI_TIMEOUT", tc.value)
			if got := serveTimeout(); got != tc.want {
				t.Errorf("serveTimeout() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestServeTimeoutErrorKeepsShortElapsed(t *testing.T) {
	msg := serveTimeoutError(&serveConn{}, time.Millisecond, 1200*time.Microsecond, true).Error()
	if !strings.Contains(msg, "(waited 1.2ms)") {
		t.Fatalf("err = %q, want the short elapsed time", msg)
	}
}

func TestFetchFromCLICancelled(t *testing.T) {
	serveLines(t, true, "", initializeAnswer)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := Fetch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestFetchFromCLIEndsEarly(t *testing.T) {
	serveLines(t, false, "error: installed binary is missing;\n rerun the installer\n", initializeAnswer)

	_, err := Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch succeeded, want an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ended before it answered") || !strings.Contains(msg, "installed binary is missing; rerun the installer") {
		t.Errorf("err = %q, want the early end with the host's stderr", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("err = %q, want one line", msg)
	}
}

func TestFetchFromCLIMuseMissing(t *testing.T) {
	// The real start, with a PATH that has no muse on it.
	t.Setenv("PATH", t.TempDir())
	writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth"}}}`)

	_, err := Fetch(context.Background())
	if !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "not on PATH") || strings.Contains(msg, "\n") {
		t.Errorf("err = %q, want one line saying muse is not on PATH", msg)
	}
}

func TestFetchFromCLIStartFails(t *testing.T) {
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "auth.json"))
	stubServe(t, func(context.Context) (*serveConn, error) {
		return nil, &exec.Error{Name: "muse", Err: errors.New("permission denied")}
	})
	_, err := Fetch(context.Background())
	if err == nil || usage.IsNotConfigured(err) || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want the start failure", err)
	}
}
