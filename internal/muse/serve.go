package muse

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

// serveTimeout bounds the whole exchange with `muse serve`: start, handshake
// and the one read. By hand it takes well under a second.
const serveTimeout = 5 * time.Second

// serveConn is a running `muse serve`: JSON-RPC 2.0 on its stdin and stdout,
// one object per line. stop ends the process and unblocks a pending read; it
// is safe to call more than once. diagnostics returns what the process wrote
// to stderr, for the case where it ends without answering; it may stop the
// process to collect it.
type serveConn struct {
	in          io.Writer
	out         io.Reader
	stop        func()
	diagnostics func() string
}

// startServe starts `muse serve`. It is a variable so tests can answer with
// canned lines, the way they stub keyEndpoint for the mint call.
var startServe = startMuseServe

// The exchange's requests. The host refuses usage/read ("Not initialized")
// until the initialized notification has closed the handshake, and
// clientInfo.name must match ^[a-z0-9_]+$.
const (
	initializeID = 1
	usageReadID  = 2

	initializeRequest  = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"aiquokka","version":"0"}}}`
	initializedNotice  = `{"jsonrpc":"2.0","method":"initialized"}`
	usageReadRequest   = `{"jsonrpc":"2.0","id":2,"method":"usage/read","params":{}}`
	serveNotOnPathHint = "the `muse` CLI is not on PATH"
)

// serveMessage is one line from the host: an answer (id with result or error)
// or a notification (method, no id), which the exchange skips.
type serveMessage struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *serveError     `json:"error"`
}

type serveError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Kind string `json:"kind"`
	} `json:"data"`
}

// cliUsage is the usage/read result (UsageReadResult in the CLI's
// msp.schema.json). The host omits usage when it has observed none.
type cliUsage struct {
	Usage *struct {
		Tier   string     `json:"tier"`
		Window *cliWindow `json:"window"`
		Weekly *cliWindow `json:"weekly"`
	} `json:"usage"`
}

// cliWindow is one window as the CLI reports it: the mint call's numbers
// under camelCase names, the reset stamp in epoch milliseconds. The fields
// stay loose for the same reason windowSnapshot's do.
type cliWindow struct {
	UsedPercent        any `json:"usedPercent"`
	WindowDurationMins any `json:"windowDurationMins"`
	ResetsAtMs         any `json:"resetsAtMs"`
}

// snapshot converts to the mint call's shape so both sources build their
// windows the same way; resetsAt tells milliseconds from seconds.
func (w *cliWindow) snapshot() *windowSnapshot {
	if w == nil {
		return nil
	}
	return &windowSnapshot{UsedPercent: w.UsedPercent, WindowDurationMins: w.WindowDurationMins, ResetsAt: w.ResetsAtMs}
}

// fetchFromCLI asks the Muse CLI for the quota it last observed. The CLI may
// read its own Keychain item without a dialog, which aiquokka may not.
func fetchFromCLI(ctx context.Context) (*usage.Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, serveTimeout)
	defer cancel()

	conn, err := startServe(ctx)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, usage.NotConfigured("Muse quota is read through the Muse CLI, and %s — install it and run `muse login`", serveNotOnPathHint)
		}
		return nil, fmt.Errorf("starting `muse serve`: %s", oneLine(err.Error()))
	}
	defer conn.stop()
	// A host that stops answering is ended at the deadline, which fails the
	// read below.
	defer context.AfterFunc(ctx, conn.stop)()

	result, err := serveExchange(conn)
	if err != nil {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("`muse serve` did not answer within %s", serveTimeout)
			}
			return nil, ctx.Err()
		}
		return nil, err
	}
	var read cliUsage
	if err := json.Unmarshal(result, &read); err != nil {
		return nil, fmt.Errorf("decoding Muse quota from `muse serve`: %w", err)
	}
	return reportFromCLI(&read), nil
}

// serveExchange runs the handshake and the one read, returning the
// usage/read result.
func serveExchange(conn *serveConn) (json.RawMessage, error) {
	// Notifications are small; the buffer only has to hold the initialize
	// answer and whatever the host announces between the two.
	lines := bufio.NewScanner(conn.out)
	lines.Buffer(make([]byte, 0, 64<<10), 1<<20)

	if _, err := io.WriteString(conn.in, initializeRequest+"\n"); err != nil {
		return nil, serveEnded(conn, err)
	}
	if _, err := serveAnswer(conn, lines, initializeID, "initialize"); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(conn.in, initializedNotice+"\n"+usageReadRequest+"\n"); err != nil {
		return nil, serveEnded(conn, err)
	}
	return serveAnswer(conn, lines, usageReadID, "usage/read")
}

// serveAnswer reads lines until the answer to request id, skipping the
// notifications (and anything else that is not that answer) in between.
func serveAnswer(conn *serveConn, lines *bufio.Scanner, id int, method string) (json.RawMessage, error) {
	for lines.Scan() {
		var msg serveMessage
		if json.Unmarshal(lines.Bytes(), &msg) != nil || msg.ID == nil || *msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return nil, serveFailure(method, msg.Error)
		}
		return msg.Result, nil
	}
	return nil, serveEnded(conn, lines.Err())
}

// loginFailure matches a host error about the account rather than the
// request. The CLI's error kinds (ErrorKind in its schema) have none for a
// missing login, so the message is all there is to go by.
var loginFailure = regexp.MustCompile(`(?i)\b(log ?in|logged (in|out)|sign(ed)? ?in|unauthori[sz]ed|unauthenticated|authenticat\w*|credentials?)\b`)

// serveFailure turns an error answer into the provider's one-line error.
func serveFailure(method string, e *serveError) error {
	msg := oneLine(e.Message)
	if msg == "" {
		msg = "no message"
	}
	if loginFailure.MatchString(msg) || loginFailure.MatchString(e.Data.Kind) {
		return usage.NotConfigured("the Muse CLI reports no usable login (%s) — run `muse login`", msg)
	}
	return fmt.Errorf("`muse serve` refused %s: %s (code %d)", method, msg, e.Code)
}

// serveEnded reports a host that closed its output before answering, with
// the first of what it said on stderr.
func serveEnded(conn *serveConn, cause error) error {
	detail := ""
	if conn.diagnostics != nil {
		detail = oneLine(conn.diagnostics())
	}
	if detail == "" && cause != nil {
		detail = oneLine(cause.Error())
	}
	if detail == "" {
		return errors.New("`muse serve` ended before it answered")
	}
	if len(detail) > 200 {
		detail = detail[:200] + "…"
	}
	return fmt.Errorf("`muse serve` ended before it answered: %s", detail)
}

// oneLine keeps a diagnostic on the same line as the message around it.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// reportFromCLI converts the usage/read result to the shared model. A missing
// usage means this host has observed none, so there are no windows to show
// and none is invented. The tier is the provider's id, not the display name
// the mint call gives.
func reportFromCLI(read *cliUsage) *usage.Report {
	report := &usage.Report{Provider: "Muse"}
	if read.Usage == nil {
		return report
	}
	report.Plan = read.Usage.Tier
	if w := rollingWindow(read.Usage.Window.snapshot()); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	if w := weeklyWindow(read.Usage.Weekly.snapshot()); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	return report
}
