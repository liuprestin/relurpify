package toolchain

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lexcodex/relurpify/cmd/internal/setup"
	"github.com/lexcodex/relurpify/tools"
)

func TestManagerWarmLanguagesEmitsEvents(t *testing.T) {
	sink := make(chan Event, 4)
	servers := []setup.LSPServer{{ID: "go", Language: "Go", Available: true}}
	m, err := NewManager(".", servers, sink)
	require.NoError(t, err)
	m.proxyFactory = func(language, root string) (*tools.Proxy, *tools.ProxyInstance, func(), error) {
		return tools.NewProxy(time.Millisecond), nil, func() {}, nil
	}

	require.NoError(t, m.WarmLanguages([]string{"go"}))
	expectEvent(t, sink, EventWarmStart)
	expectEvent(t, sink, EventWarmSuccess)
}

func TestManagerWarmLanguagesFailureEmitsErrorEvent(t *testing.T) {
	sink := make(chan Event, 4)
	servers := []setup.LSPServer{{ID: "go", Language: "Go", Available: true}}
	m, err := NewManager(".", servers, sink)
	require.NoError(t, err)
	m.proxyFactory = func(language, root string) (*tools.Proxy, *tools.ProxyInstance, func(), error) {
		return nil, nil, nil, errors.New("boom")
	}

	require.Error(t, m.WarmLanguages([]string{"go"}))
	expectEvent(t, sink, EventWarmStart)
	failed := expectEvent(t, sink, EventWarmFailed)
	require.EqualError(t, failed.Err, "boom")
}

func TestManagerLogStreamingAndShutdown(t *testing.T) {
	sink := make(chan Event, 8)
	servers := []setup.LSPServer{{ID: "go", Language: "Go", Available: true}}
	m, err := NewManager(".", servers, sink)
	require.NoError(t, err)
	logs := make(chan string, 1)
	m.proxyFactory = func(language, root string) (*tools.Proxy, *tools.ProxyInstance, func(), error) {
		inst := &tools.ProxyInstance{
			Language: "go",
			Command:  "fake",
			PID:      42,
			Started:  time.Now(),
			Logs:     logs,
		}
		return tools.NewProxy(time.Millisecond), inst, func() {}, nil
	}

	require.NoError(t, m.WarmLanguages([]string{"go"}))
	expectEvent(t, sink, EventWarmStart)
	expectEvent(t, sink, EventWarmSuccess)

	logs <- "booted"
	logEvent := expectEvent(t, sink, EventLogLine)
	require.Equal(t, "booted", logEvent.Message)

	m.Close()
	expectEvent(t, sink, EventShutdown)
}

func expectEvent(t *testing.T, sink <-chan Event, typ EventType) Event {
	t.Helper()
	select {
	case evt := <-sink:
		require.Equal(t, typ, evt.Type)
		return evt
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for %s", typ)
		return Event{}
	}
}
