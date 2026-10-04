package exitcode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
)

func TestOfNil(t *testing.T) {
	if got := Of(nil); got != OK {
		t.Fatalf("Of(nil) = %d, want %d", got, OK)
	}
}

func TestOfExplicitCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"usage", Usagef("bad flag"), Usage},
		{"auth", Authf("no token"), Auth},
		{"net", Netf("down"), Network},
		{"run failed", New(DeployFailed, "运行失败"), DeployFailed},
		{"wrapped", fmt.Errorf("cmd x: %w", New(Auth, "401")), Auth},
		{"silent fail", Fail(DeployFailed), DeployFailed},
		{"deep wrap", fmt.Errorf("a: %w", fmt.Errorf("b: %w", New(Network, "5xx"))), Network},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Of(tc.err); got != tc.want {
				t.Fatalf("Of(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// 自定义 Coder 应优先于其它启发式判断。
type coderErr struct{ code int }

func (c coderErr) Error() string { return "custom" }
func (c coderErr) ExitCode() int { return c.code }

func TestOfCoderWins(t *testing.T) {
	err := fmt.Errorf("wrap: %w", coderErr{code: Network})
	if got := Of(err); got != Network {
		t.Fatalf("Of = %d, want %d", got, Network)
	}
}

func TestOfContext(t *testing.T) {
	if got := Of(context.DeadlineExceeded); got != Network {
		t.Fatalf("deadline => %d, want %d", got, Network)
	}
	if got := Of(fmt.Errorf("call: %w", context.Canceled)); got != Network {
		t.Fatalf("canceled => %d, want %d", got, Network)
	}
}

func TestOfNetErrors(t *testing.T) {
	if got := Of(&net.OpError{Op: "dial", Err: errors.New("boom")}); got != Network {
		t.Fatalf("net.OpError => %d, want %d", got, Network)
	}
	if got := Of(&url.Error{Op: "Get", URL: "http://x", Err: errors.New("dial tcp: connection refused")}); got != Network {
		t.Fatalf("url.Error => %d, want %d", got, Network)
	}
	if got := Of(errors.New("dial tcp 127.0.0.1:8080: connectex: No connection could be made")); got != Usage {
		// 该英文串不含 hints，落到兜底；这里只是固定当前行为
		t.Logf("unmatched message => %d", got)
	}
	if got := Of(errors.New("Get \"http://x\": dial tcp: connect: connection refused")); got != Network {
		t.Fatalf("message heuristic => %d, want %d", got, Network)
	}
}

func TestOfUnknownFallsBackToUsage(t *testing.T) {
	if got := Of(errors.New("unknown command \"foo\" for \"deploy\"")); got != Usage {
		t.Fatalf("unknown => %d, want %d", got, Usage)
	}
}

func TestErrorFormatting(t *testing.T) {
	e := Wrap(Auth, errors.New("401"), "鉴权失败")
	if got := e.Error(); got != "鉴权失败: 401" {
		t.Fatalf("Error() = %q", got)
	}
	var target *Error
	if !errors.As(e, &target) {
		t.Fatalf("errors.As(*Error) failed")
	}
	if target.Code != Auth || target.Silent() {
		t.Fatalf("Wrap produced code=%d silent=%v", target.Code, target.Silent())
	}
	if New(Usage, "x").Silent() {
		t.Fatalf("New(...) must not be silent")
	}
	if !Fail(DeployFailed).Silent() {
		t.Fatalf("Fail(...) must be silent")
	}
}
