package cliutils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lexcodex/relurpify/tools"
)

func TestNewProxyForLanguageProvidesMetadataAndLogs(t *testing.T) {
	key := "testlang"
	addDescriptor([]string{key}, LSPDescriptor{
		ID:         key,
		Factory:    func(root string) (tools.LSPClient, error) { return newFakeClient(), nil },
		Extensions: []string{"tl"},
		Commands:   []string{"fake-cmd"},
	})

	proxy, instance, cleanup, err := NewProxyForLanguage(key, ".")
	require.NoError(t, err)
	require.NotNil(t, proxy)
	require.NotNil(t, instance)
	require.NotNil(t, cleanup)

	require.Equal(t, key, instance.Language)
	require.Equal(t, "fake-cmd", instance.Command)
	require.Equal(t, 1234, instance.PID)
	require.NotNil(t, instance.Logs)

	cleanup()
}

func TestNewProxyForLanguageUnsupported(t *testing.T) {
	_, _, _, err := NewProxyForLanguage("unknown-language", ".")
	require.Error(t, err)
}

type fakeClient struct {
	logs chan string
}

func newFakeClient() *fakeClient {
	return &fakeClient{logs: make(chan string)}
}

func (f *fakeClient) GetDefinition(ctx context.Context, req tools.DefinitionRequest) (tools.DefinitionResult, error) {
	return tools.DefinitionResult{}, nil
}

func (f *fakeClient) GetReferences(ctx context.Context, req tools.ReferencesRequest) ([]tools.Location, error) {
	return nil, nil
}

func (f *fakeClient) GetHover(ctx context.Context, req tools.HoverRequest) (tools.HoverResult, error) {
	return tools.HoverResult{}, nil
}

func (f *fakeClient) GetDiagnostics(ctx context.Context, file string) ([]tools.Diagnostic, error) {
	return nil, nil
}

func (f *fakeClient) SearchSymbols(ctx context.Context, query string) ([]tools.SymbolInformation, error) {
	return nil, nil
}

func (f *fakeClient) GetDocumentSymbols(ctx context.Context, file string) ([]tools.SymbolInformation, error) {
	return nil, nil
}

func (f *fakeClient) Format(ctx context.Context, req tools.FormatRequest) (string, error) {
	return req.Code, nil
}

func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) Logs() <-chan string { return f.logs }

func (f *fakeClient) ProcessMetadata() tools.ProcessMetadata {
	return tools.ProcessMetadata{
		PID:     1234,
		Command: "fake-cmd",
		Started: time.Now(),
	}
}
