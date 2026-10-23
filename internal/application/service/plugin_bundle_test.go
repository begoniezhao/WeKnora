package service

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePluginMarketplaceWithAcmePluginsTarGz(t *testing.T) {
	data, err := os.ReadFile("testdata/acme-plugins.tar.gz")
	require.NoError(t, err)

	files, err := UnpackMarketplaceArchive(data)
	require.NoError(t, err)
	require.NotEmpty(t, files)

	mp, err := ParsePluginMarketplace(files)
	require.NoError(t, err)
	require.NotNil(t, mp)
	assert.Equal(t, "acme-plugins", mp.Manifest.Name)
	assert.Len(t, mp.Plugins, 3)

	// Verify neon
	neon, ok := mp.Plugins["neon"]
	require.True(t, ok)
	assert.Equal(t, "neon", neon.Name)
	assert.True(t, neon.RequiresSandbox)
	assert.Equal(t, 2, neon.SkillCount)
	assert.Equal(t, 2, neon.MCPCount)
	assert.True(t, neon.HasPluginJSON)
	assert.Len(t, neon.Skills, 2)
	assert.Equal(t, "neon__neon-branches", neon.Skills[0].PrefixedName)
	assert.Equal(t, "neon__neon-postgres", neon.Skills[1].PrefixedName)

	// Verify calcom
	calcom, ok := mp.Plugins["calcom"]
	require.True(t, ok)
	assert.Equal(t, "calcom", calcom.Name)
	assert.False(t, calcom.RequiresSandbox)
	assert.Equal(t, 0, calcom.SkillCount)
	assert.Equal(t, 1, calcom.MCPCount)
	assert.True(t, calcom.HasPluginJSON)

	// Verify sql-style (fallback since no plugin.json)
	sqlStyle, ok := mp.Plugins["sql-style"]
	require.True(t, ok)
	assert.Equal(t, "sql-style", sqlStyle.Name)
	assert.True(t, sqlStyle.RequiresSandbox)
	assert.Equal(t, 1, sqlStyle.SkillCount)
	assert.Equal(t, 0, sqlStyle.MCPCount)
	assert.False(t, sqlStyle.HasPluginJSON)
	assert.Equal(t, "sql-style__sql-style", sqlStyle.Skills[0].PrefixedName)
}

func TestPluginEnvMissingDeclarationError(t *testing.T) {
	files := map[string][]byte{
		".weknora-plugin/marketplace.json": []byte(`{
			"name": "test-mp",
			"plugins": [{"name": "bad-plugin", "source": "./bad"}]
		}`),
		"bad/.weknora-plugin/plugin.json": []byte(`{
			"name": "bad-plugin",
			"env": [{"name": "DECLARED_VAR", "required": true}]
		}`),
		"bad/.mcp.json": []byte(`{
			"mcpServers": {
				"bad-srv": {
					"type": "http",
					"url": "https://example.com/mcp",
					"headers": {"Authorization": "Bearer ${UNDECLARED_VAR}"}
				}
			}
		}`),
	}

	_, err := ParsePluginMarketplace(files)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "引用了 ${UNDECLARED_VAR}, 但 plugin.json 的 env 中未声明")
}

func TestPluginStdioSkippedWithWarning(t *testing.T) {
	files := map[string][]byte{
		".weknora-plugin/marketplace.json": []byte(`{
			"name": "test-mp",
			"plugins": [{"name": "stdio-plugin", "source": "./stdio"}]
		}`),
		"stdio/.mcp.json": []byte(`{
			"mcpServers": {
				"stdio-srv": {
					"type": "stdio",
					"command": "npx",
					"args": ["-y", "mcp-server"]
				},
				"http-srv": {
					"type": "http",
					"url": "https://example.com/mcp"
				}
			}
		}`),
	}

	mp, err := ParsePluginMarketplace(files)
	require.NoError(t, err)
	p := mp.Plugins["stdio-plugin"]
	require.NotNil(t, p)
	assert.Equal(t, 1, p.MCPCount)
	assert.Contains(t, p.MCPServers, "http-srv")
	assert.NotContains(t, p.MCPServers, "stdio-srv")
	assert.Len(t, p.MCPWarnings, 1)
	assert.Contains(t, p.MCPWarnings[0], "已禁用 stdio transport")
}

func TestValidatePluginName(t *testing.T) {
	for _, name := range []string{"neon", "sql-style", "my_plugin", "插件1"} {
		assert.NoError(t, validatePluginName(name), name)
	}
	for _, name := range []string{"", "a/b", "a b", "-lead", "a__b", strings.Repeat("x", maxPluginNameLength+1)} {
		assert.Error(t, validatePluginName(name), name)
	}
}

func TestRewriteSkillFrontmatterNameKeepsNestedKeys(t *testing.T) {
	in := "---\nname: orig\nmetadata:\n  name: nested\ndescription: d\n---\nbody\n"
	out, err := rewriteSkillFrontmatterName([]byte(in), "acme__orig")
	require.NoError(t, err)
	assert.Equal(t, "---\nname: acme__orig\nmetadata:\n  name: nested\ndescription: d\n---\nbody\n", string(out))
}

func TestNormalizeMarketplaceFilesPicksShallowestManifest(t *testing.T) {
	raw := map[string][]byte{
		"root/.weknora-plugin/marketplace.json":          []byte(`{}`),
		"root/vendor/x/.weknora-plugin/marketplace.json": []byte(`{}`),
		"root/plugins/a/.weknora-plugin/plugin.json":     []byte(`{}`),
	}
	for i := 0; i < 20; i++ {
		out, err := normalizeMarketplaceFiles(raw)
		require.NoError(t, err)
		assert.Contains(t, out, ".weknora-plugin/marketplace.json")
		assert.Contains(t, out, "plugins/a/.weknora-plugin/plugin.json")
	}
}
