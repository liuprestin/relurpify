package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigHelpers(t *testing.T) {
	data := map[string]interface{}{
		"permissions": map[string]interface{}{
			"file_write": "ask",
		},
	}
	value, ok := getConfigValue(data, "permissions.file_write")
	require.True(t, ok)
	require.Equal(t, "ask", value)

	require.NoError(t, setConfigValue(data, "permissions.file_write", "allow"))
	value, ok = getConfigValue(data, "permissions.file_write")
	require.True(t, ok)
	require.Equal(t, "allow", value)

	require.NoError(t, setConfigValue(data, "context.max_files_in_context", 10))
	value, ok = getConfigValue(data, "context.max_files_in_context")
	require.True(t, ok)
	require.Equal(t, 10, value)
}
