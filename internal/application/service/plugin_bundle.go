package service

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/types"
)

var (
	varRegex = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)
	// pluginNamePattern keeps a plugin name usable as one URL path segment
	// and as the prefix of skill names. MCP services are prefixed with the
	// marketplace name instead.
	pluginNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]*$`)
)

// maxPluginNameLength leaves room for "__<skill>" within skills.MaxNameLength.
const maxPluginNameLength = 48

// validatePluginName rejects names that would break routes or make the
// "<plugin>__<resource>" prefix ambiguous.
func validatePluginName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("cannot be empty")
	case len(name) > maxPluginNameLength:
		return fmt.Errorf("longer than %d bytes", maxPluginNameLength)
	case !pluginNamePattern.MatchString(name):
		return fmt.Errorf("must start with a letter or digit and contain only letters, digits, hyphens and underscores")
	case strings.Contains(name, "__"):
		return fmt.Errorf("must not contain \"__\"")
	}
	return nil
}

// ParsedMarketplace contains the decoded marketplace manifest, normalized raw files, and parsed plugins.
type ParsedMarketplace struct {
	Manifest types.MarketplaceManifest
	RawFiles map[string][]byte
	Plugins  map[string]*ParsedPlugin
	// Order lists plugin names in marketplace.json order.
	Order []string
}

// ParsedPlugin represents one plugin extracted from a marketplace bundle.
type ParsedPlugin struct {
	Name            string
	Source          string
	Description     string
	Version         string
	Category        string
	Keywords        []string
	Manifest        *types.PluginManifest
	HasPluginJSON   bool
	MCPServers      map[string]types.PluginMCPServerEntry
	MCPWarnings     []string
	Skills          []ParsedPluginSkill
	SkillCount      int
	MCPCount        int
	RequiresSandbox bool
	EnvDeclarations []types.PluginEnvVar
}

// ParsedPluginSkill represents a repackaged skill with the prefixed name.
type ParsedPluginSkill struct {
	OriginalName string
	PrefixedName string
	Archive      []byte // Re-packed zip with SKILL.md at root
	Bundle       *SkillBundle
}

// UnpackMarketplaceArchive extracts a zip or tar.gz archive into a normalized in-memory file map.
func UnpackMarketplaceArchive(archive []byte) (map[string][]byte, error) {
	var raw map[string][]byte
	var err error

	if len(archive) >= 2 && archive[0] == 0x1f && archive[1] == 0x8b {
		raw, err = unpackTarGzArchive(archive)
	} else {
		raw, err = unpackZipArchive(archive)
		if err != nil {
			// Fallback: check if uncompressed tar
			var tarErr error
			raw, tarErr = unpackTarGzArchive(archive)
			if tarErr == nil {
				err = nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return normalizeMarketplaceFiles(raw)
}

func unpackTarGzArchive(archive []byte) (map[string][]byte, error) {
	var tr *tar.Reader
	if len(archive) >= 2 && archive[0] == 0x1f && archive[1] == 0x8b {
		gr, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid gzip: %v", ErrSkillBundleInvalid, err)
		}
		defer gr.Close()
		tr = tar.NewReader(gr)
	} else {
		tr = tar.NewReader(bytes.NewReader(archive))
	}

	raw := make(map[string][]byte)
	var totalBytes int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read tar entry: %v", ErrSkillBundleInvalid, err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("%w: entry %q is not a regular file", ErrSkillBundleInvalid, header.Name)
		}

		cleanPath := path.Clean(header.Name)
		if cleanPath == "." || strings.HasPrefix(cleanPath, "..") || path.IsAbs(cleanPath) || strings.Contains(cleanPath, "../") {
			return nil, fmt.Errorf("%w: entry %q escapes the archive root", ErrSkillBundleInvalid, header.Name)
		}

		if header.Size > maxSkillBundleFileBytes {
			return nil, fmt.Errorf("%w: entry %q is too large", ErrSkillBundleInvalid, header.Name)
		}
		if totalBytes+header.Size > maxSkillBundleTotalBytes {
			return nil, fmt.Errorf("%w: archive is too large", ErrSkillBundleInvalid)
		}

		buf, err := io.ReadAll(io.LimitReader(tr, maxSkillBundleFileBytes+1))
		if err != nil {
			return nil, fmt.Errorf("%w: cannot read entry %q: %v", ErrSkillBundleInvalid, header.Name, err)
		}
		if int64(len(buf)) > maxSkillBundleFileBytes {
			return nil, fmt.Errorf("%w: entry %q is too large", ErrSkillBundleInvalid, header.Name)
		}
		totalBytes += int64(len(buf))
		if totalBytes > maxSkillBundleTotalBytes {
			return nil, fmt.Errorf("%w: archive is too large", ErrSkillBundleInvalid)
		}

		raw[cleanPath] = buf
		if len(raw) > maxSkillBundleFiles {
			return nil, fmt.Errorf("%w: archive holds more than %d files", ErrSkillBundleInvalid, maxSkillBundleFiles)
		}
	}
	return raw, nil
}

func unpackZipArchive(archive []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("%w: not a readable zip archive: %v", ErrSkillBundleInvalid, err)
	}
	if len(reader.File) > maxSkillBundleZipEntries {
		return nil, fmt.Errorf("%w: archive has more than %d zip entries", ErrSkillBundleInvalid, maxSkillBundleZipEntries)
	}

	raw := make(map[string][]byte)
	var totalBytes int64
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		cleanPath := path.Clean(entry.Name)
		if cleanPath == "." || strings.HasPrefix(cleanPath, "..") || path.IsAbs(cleanPath) || strings.Contains(cleanPath, "../") {
			return nil, fmt.Errorf("%w: entry %q escapes the archive root", ErrSkillBundleInvalid, entry.Name)
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: entry %q is a symlink", ErrSkillBundleInvalid, entry.Name)
		}
		size := entry.FileInfo().Size()
		if size > maxSkillBundleFileBytes {
			return nil, fmt.Errorf("%w: entry %q is too large", ErrSkillBundleInvalid, entry.Name)
		}
		if totalBytes+size > maxSkillBundleTotalBytes {
			return nil, fmt.Errorf("%w: archive is too large", ErrSkillBundleInvalid)
		}

		rc, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot read %q: %v", ErrSkillBundleInvalid, entry.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxSkillBundleFileBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot read %q: %v", ErrSkillBundleInvalid, entry.Name, err)
		}
		if int64(len(content)) > maxSkillBundleFileBytes {
			return nil, fmt.Errorf("%w: entry %q is too large", ErrSkillBundleInvalid, entry.Name)
		}
		totalBytes += int64(len(content))
		if totalBytes > maxSkillBundleTotalBytes {
			return nil, fmt.Errorf("%w: archive is too large", ErrSkillBundleInvalid)
		}
		raw[cleanPath] = content
		if len(raw) > maxSkillBundleFiles {
			return nil, fmt.Errorf("%w: archive holds more than %d files", ErrSkillBundleInvalid, maxSkillBundleFiles)
		}
	}
	return raw, nil
}

func normalizeMarketplaceFiles(raw map[string][]byte) (map[string][]byte, error) {
	const manifestPath = ".weknora-plugin/marketplace.json"
	if _, ok := raw[manifestPath]; ok {
		return raw, nil
	}
	// The shallowest manifest wins, so the choice does not depend on map order.
	var prefix string
	for name := range raw {
		if !strings.HasSuffix(name, "/"+manifestPath) {
			continue
		}
		candidate := strings.TrimSuffix(name, "/"+manifestPath)
		if prefix == "" || len(candidate) < len(prefix) || (len(candidate) == len(prefix) && candidate < prefix) {
			prefix = candidate
		}
	}
	if prefix == "" {
		return nil, fmt.Errorf("%w: %s is missing", ErrSkillBundleInvalid, manifestPath)
	}
	prefixSlash := prefix + "/"
	out := make(map[string][]byte, len(raw))
	for name, content := range raw {
		if strings.HasPrefix(name, prefixSlash) {
			out[strings.TrimPrefix(name, prefixSlash)] = content
		}
	}
	if _, ok := out[manifestPath]; !ok {
		return nil, fmt.Errorf("%w: %s is missing", ErrSkillBundleInvalid, manifestPath)
	}
	return out, nil
}

// ParsePluginMarketplace parses marketplace.json and all referenced plugins from normalized files.
func ParsePluginMarketplace(files map[string][]byte) (*ParsedMarketplace, error) {
	const manifestPath = ".weknora-plugin/marketplace.json"
	manifestBytes, ok := files[manifestPath]
	if !ok {
		return nil, fmt.Errorf("%w: %s is missing", ErrSkillBundleInvalid, manifestPath)
	}

	var manifest types.MarketplaceManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("%w: cannot parse %s: %v", ErrSkillBundleInvalid, manifestPath, err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return nil, fmt.Errorf("%w: marketplace name cannot be empty", ErrSkillBundleInvalid)
	}

	parsed := &ParsedMarketplace{
		Manifest: manifest,
		RawFiles: files,
		Plugins:  make(map[string]*ParsedPlugin, len(manifest.Plugins)),
	}

	for _, entry := range manifest.Plugins {
		plugin, err := parsePluginFromFiles(files, entry)
		if err != nil {
			return nil, err
		}
		if _, dup := parsed.Plugins[plugin.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate plugin name %q", ErrSkillBundleInvalid, plugin.Name)
		}
		parsed.Plugins[plugin.Name] = plugin
		parsed.Order = append(parsed.Order, plugin.Name)
	}

	return parsed, nil
}

func parsePluginFromFiles(files map[string][]byte, entry types.MarketplacePluginEntry) (*ParsedPlugin, error) {
	cleanSource := path.Clean(strings.TrimPrefix(strings.TrimSpace(entry.Source), "./"))
	if cleanSource == "." {
		cleanSource = ""
	}

	pluginDirPrefix := cleanSource
	if pluginDirPrefix != "" {
		pluginDirPrefix += "/"
	}

	pluginManifestPath := pluginDirPrefix + ".weknora-plugin/plugin.json"
	var pluginManifest *types.PluginManifest
	hasPluginJSON := false
	if pjBytes, ok := files[pluginManifestPath]; ok {
		hasPluginJSON = true
		pluginManifest = &types.PluginManifest{}
		if err := json.Unmarshal(pjBytes, pluginManifest); err != nil {
			return nil, fmt.Errorf("%w: cannot parse %s: %v", ErrSkillBundleInvalid, pluginManifestPath, err)
		}
	}

	pluginName := entry.Name
	if pluginManifest != nil && strings.TrimSpace(pluginManifest.Name) != "" {
		pluginName = pluginManifest.Name
	}
	if err := validatePluginName(pluginName); err != nil {
		return nil, fmt.Errorf("%w: plugin name %q: %v", ErrSkillBundleInvalid, pluginName, err)
	}

	// Validate env declarations in plugin.json
	declaredEnvMap := make(map[string]bool)
	var envDeclarations []types.PluginEnvVar
	if pluginManifest != nil && len(pluginManifest.Env) > 0 {
		for _, e := range pluginManifest.Env {
			name := strings.TrimSpace(e.Name)
			if err := validateUserEnvName(name); err != nil {
				return nil, fmt.Errorf("%w: plugin %q: invalid env declaration %q: %v", ErrSkillBundleInvalid, pluginName, name, err)
			}
			if declaredEnvMap[name] {
				return nil, fmt.Errorf("%w: plugin %q: duplicate env declaration %q", ErrSkillBundleInvalid, pluginName, name)
			}
			declaredEnvMap[name] = true
			envDeclarations = append(envDeclarations, types.PluginEnvVar{
				Name:        name,
				Description: e.Description,
				Required:    e.Required,
			})
		}
	}

	// Parse MCP servers
	mcpPath := pluginDirPrefix + ".mcp.json"
	if pluginManifest != nil && strings.TrimSpace(pluginManifest.MCPServers) != "" {
		cleanMCPServers := path.Clean(strings.TrimPrefix(strings.TrimSpace(pluginManifest.MCPServers), "./"))
		if cleanMCPServers != "" && cleanMCPServers != "." {
			mcpPath = pluginDirPrefix + cleanMCPServers
		}
	}

	mcpServers := make(map[string]types.PluginMCPServerEntry)
	var mcpWarnings []string
	if mcpBytes, ok := files[mcpPath]; ok {
		var mcpConfig types.PluginMCPServersConfig
		if err := json.Unmarshal(mcpBytes, &mcpConfig); err != nil {
			return nil, fmt.Errorf("%w: cannot parse %s: %v", ErrSkillBundleInvalid, mcpPath, err)
		}

		for serverName, server := range mcpConfig.MCPServers {
			// Skip stdio transport
			if strings.EqualFold(server.Type, "stdio") || strings.TrimSpace(server.Command) != "" || len(server.Args) > 0 {
				mcpWarnings = append(mcpWarnings, fmt.Sprintf("跳过 MCP 服务 %q: WeKnora 已禁用 stdio transport", serverName))
				continue
			}

			// Validate URL doesn't contain ${VAR}
			if strings.Contains(server.URL, "${") {
				return nil, fmt.Errorf("%w: plugin %q: MCP 服务 %q 的 URL 中不能使用变量占位符", ErrSkillBundleInvalid, pluginName, serverName)
			}

			// Validate and check headers
			varHeadersFound := 0
			for hKey, hVal := range server.Headers {
				matches := varRegex.FindAllStringSubmatch(hVal, -1)
				if len(matches) == 0 {
					continue
				}
				varHeadersFound++
				if varHeadersFound > 1 {
					return nil, fmt.Errorf("%w: plugin %q: MCP 服务 %q 最多支持一个带 ${VAR} 的 header", ErrSkillBundleInvalid, pluginName, serverName)
				}
				if len(matches) > 1 {
					return nil, fmt.Errorf("%w: plugin %q: MCP 服务 %q 的 header %q 中包含多个变量占位符", ErrSkillBundleInvalid, pluginName, serverName, hKey)
				}

				varName := matches[0][1]
				if !declaredEnvMap[varName] {
					return nil, fmt.Errorf("%w: plugin %q: .mcp.json 引用了 ${%s}, 但 plugin.json 的 env 中未声明", ErrSkillBundleInvalid, pluginName, varName)
				}

				// Check mapping pattern
				// Must be exact "${VAR}" or "Authorization: Bearer ${VAR}"
				exactPlaceholder := fmt.Sprintf("${%s}", varName)
				bearerPlaceholder := fmt.Sprintf("Bearer ${%s}", varName)

				if hVal == exactPlaceholder {
					// API key format
				} else if strings.EqualFold(hKey, "Authorization") && hVal == bearerPlaceholder {
					// Bearer format
				} else {
					return nil, fmt.Errorf("%w: plugin %q: MCP 服务 %q 的 header %q 格式不支持 (仅支持 Bearer ${VAR} 或纯 ${VAR})", ErrSkillBundleInvalid, pluginName, serverName, hKey)
				}
			}

			mcpServers[serverName] = server
		}
	}

	// Parse Skills
	skillsDirRel := "skills"
	if pluginManifest != nil && strings.TrimSpace(pluginManifest.Skills) != "" {
		cleanSkills := path.Clean(strings.TrimPrefix(strings.TrimSpace(pluginManifest.Skills), "./"))
		if cleanSkills != "" && cleanSkills != "." {
			skillsDirRel = cleanSkills
		}
	}
	skillsPrefix := pluginDirPrefix + skillsDirRel + "/"

	// Find all skill subdirectories: skillsPrefix + <subfolder> + /...
	skillFilesByDir := make(map[string]map[string][]byte)
	for filePath, content := range files {
		if strings.HasPrefix(filePath, skillsPrefix) {
			relInSkills := strings.TrimPrefix(filePath, skillsPrefix)
			parts := strings.SplitN(relInSkills, "/", 2)
			if len(parts) == 2 {
				subDir := parts[0]
				fileInSubDir := parts[1]
				if skillFilesByDir[subDir] == nil {
					skillFilesByDir[subDir] = make(map[string][]byte)
				}
				skillFilesByDir[subDir][fileInSubDir] = content
			}
		}
	}

	var parsedSkills []ParsedPluginSkill
	for subDir, skillFiles := range skillFilesByDir {
		skillMD, ok := skillFiles["SKILL.md"]
		if !ok {
			continue
		}

		prefixedName := fmt.Sprintf("%s__%s", pluginName, subDir)
		if len(prefixedName) > skills.MaxNameLength {
			return nil, fmt.Errorf("%w: 技能前缀名称 %q 超过最大长度 %d", ErrSkillBundleInvalid, prefixedName, skills.MaxNameLength)
		}

		rewrittenMD, err := rewriteSkillFrontmatterName(skillMD, prefixedName)
		if err != nil {
			return nil, fmt.Errorf("%w: plugin %q: rewrite SKILL.md for %s failed: %v", ErrSkillBundleInvalid, pluginName, subDir, err)
		}
		skillFiles["SKILL.md"] = rewrittenMD

		zipArchive, err := zipSkillFiles(skillFiles)
		if err != nil {
			return nil, fmt.Errorf("%w: repackage skill %s failed: %v", ErrSkillBundleInvalid, prefixedName, err)
		}

		bundle, err := ParseSkillBundle(zipArchive)
		if err != nil {
			return nil, fmt.Errorf("%w: validate repackaged skill %s failed: %v", ErrSkillBundleInvalid, prefixedName, err)
		}

		parsedSkills = append(parsedSkills, ParsedPluginSkill{
			OriginalName: subDir,
			PrefixedName: prefixedName,
			Archive:      zipArchive,
			Bundle:       bundle,
		})
	}

	// Sort skills deterministically by PrefixedName
	sort.Slice(parsedSkills, func(i, j int) bool {
		return parsedSkills[i].PrefixedName < parsedSkills[j].PrefixedName
	})

	version := ""
	if pluginManifest != nil && pluginManifest.Version != "" {
		version = pluginManifest.Version
	}
	desc := entry.Description
	if pluginManifest != nil && pluginManifest.Description != "" {
		desc = pluginManifest.Description
	}

	return &ParsedPlugin{
		Name:            pluginName,
		Source:          entry.Source,
		Description:     desc,
		Version:         version,
		Category:        entry.Category,
		Keywords:        entry.Keywords,
		Manifest:        pluginManifest,
		HasPluginJSON:   hasPluginJSON,
		MCPServers:      mcpServers,
		MCPWarnings:     mcpWarnings,
		Skills:          parsedSkills,
		SkillCount:      len(parsedSkills),
		MCPCount:        len(mcpServers),
		RequiresSandbox: len(parsedSkills) > 0,
		EnvDeclarations: envDeclarations,
	}, nil
}

func rewriteSkillFrontmatterName(content []byte, newName string) ([]byte, error) {
	lines := strings.Split(string(content), "\n")
	frontmatterStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			frontmatterStart = i
			break
		}
		if trimmed != "" {
			break
		}
	}
	if frontmatterStart < 0 {
		return nil, fmt.Errorf("SKILL.md missing frontmatter")
	}

	frontmatterEnd := -1
	for i := frontmatterStart + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			frontmatterEnd = i
			break
		}
	}
	if frontmatterEnd < 0 {
		return nil, fmt.Errorf("SKILL.md missing closing frontmatter ---")
	}

	nameReplaced := false
	var newLines []string
	newLines = append(newLines, lines[:frontmatterStart+1]...)
	for i := frontmatterStart + 1; i < frontmatterEnd; i++ {
		line := lines[i]
		// Only the top-level key: an indented "name:" belongs to a nested map.
		if strings.HasPrefix(line, "name:") {
			newLines = append(newLines, fmt.Sprintf("name: %s", newName))
			nameReplaced = true
		} else {
			newLines = append(newLines, line)
		}
	}
	if !nameReplaced {
		newLines = append(newLines, fmt.Sprintf("name: %s", newName))
	}
	newLines = append(newLines, lines[frontmatterEnd:]...)
	return []byte(strings.Join(newLines, "\n")), nil
}
