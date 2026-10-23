package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Plugin status lifecycle constants.
const (
	PluginStatusInstalling = "installing"
	PluginStatusReady      = "ready"
	PluginStatusFailed     = "failed"
	PluginStatusRemoving   = "removing"
	// PluginStatusNeedsOAuth means the install is persisted, but an MCP
	// service it switched to OAuth still has no stored token. That is not a
	// successful install until authorization completes.
	PluginStatusNeedsOAuth = "needs_oauth"
)

// Binding kinds recorded in plugin_tenant_bindings. A marketplace binding
// grants a tenant visibility of a shared marketplace, a plugin binding is the
// tenant's install of a plugin package, and the other kinds record resources
// a plugin install owns.
const (
	PluginResourceKindMarketplace  = "marketplace"
	PluginResourceKindPlugin       = "plugin"
	PluginResourceKindMCPService   = "mcp_service"
	PluginResourceKindSkillCatalog = "skill_catalog"
)

// PluginMarketplaceEntity is an imported marketplace archive shared by every
// tenant that imported the same bytes; (name, bundle_sha256) is unique.
type PluginMarketplaceEntity struct {
	ID           string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name         string         `gorm:"type:varchar(255);not null" json:"name"`
	Description  string         `gorm:"type:text" json:"description,omitempty"`
	Version      string         `gorm:"type:varchar(64)" json:"version,omitempty"`
	OwnerName    string         `gorm:"type:varchar(255)" json:"owner_name,omitempty"`
	OwnerEmail   string         `gorm:"type:varchar(255)" json:"owner_email,omitempty"`
	Manifest     string         `gorm:"type:text" json:"-"`
	BundleRef    string         `gorm:"type:varchar(1024)" json:"-"`
	BundleSHA256 string         `gorm:"type:varchar(64);not null" json:"-"`
	CreatedAt    time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"not null" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PluginMarketplaceEntity) TableName() string {
	return "plugin_marketplaces"
}

func pluginJSONValue(v any, empty string) (driver.Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(data) == "null" {
		return empty, nil
	}
	return string(data), nil
}

func pluginJSONScan(value any, dst any) error {
	var data []byte
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("unsupported JSON column type %T", value)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

// PluginSandboxConfigIDs represents the target sandbox config IDs selected during installation.
type PluginSandboxConfigIDs []string

func (ids PluginSandboxConfigIDs) Value() (driver.Value, error) {
	return pluginJSONValue([]string(ids), "[]")
}

func (ids *PluginSandboxConfigIDs) Scan(value any) error {
	*ids = PluginSandboxConfigIDs{}
	return pluginJSONScan(value, (*[]string)(ids))
}

// PluginStringList is a JSON array of strings.
type PluginStringList []string

func (l PluginStringList) Value() (driver.Value, error) { return pluginJSONValue([]string(l), "[]") }

func (l *PluginStringList) Scan(value any) error {
	*l = nil
	return pluginJSONScan(value, (*[]string)(l))
}

// PluginEnvVars is the JSON-encoded env declarations of plugin.json.
type PluginEnvVars []PluginEnvVar

func (e PluginEnvVars) Value() (driver.Value, error) { return pluginJSONValue([]PluginEnvVar(e), "[]") }

func (e *PluginEnvVars) Scan(value any) error {
	*e = nil
	return pluginJSONScan(value, (*[]PluginEnvVar)(e))
}

// PluginMCPServers is the JSON-encoded, already validated .mcp.json servers.
type PluginMCPServers map[string]PluginMCPServerEntry

func (m PluginMCPServers) Value() (driver.Value, error) {
	return pluginJSONValue(map[string]PluginMCPServerEntry(m), "{}")
}

func (m *PluginMCPServers) Scan(value any) error {
	*m = nil
	return pluginJSONScan(value, (*map[string]PluginMCPServerEntry)(m))
}

// PluginPackageSkill is one skill of a plugin, repackaged at import with the
// plugin-prefixed name and stored in the shared bundle store.
type PluginPackageSkill struct {
	Name         string `json:"name"`
	PrefixedName string `json:"prefixed_name"`
	BundleRef    string `json:"bundle_ref"`
}

// PluginPackageSkills is the JSON-encoded skill list of a plugin package.
type PluginPackageSkills []PluginPackageSkill

func (s PluginPackageSkills) Value() (driver.Value, error) {
	return pluginJSONValue([]PluginPackageSkill(s), "[]")
}

func (s *PluginPackageSkills) Scan(value any) error {
	*s = nil
	return pluginJSONScan(value, (*[]PluginPackageSkill)(s))
}

// PluginPackageEntity is one plugin of a shared marketplace, parsed at import.
// It is tenant-independent and lives as long as its marketplace.
type PluginPackageEntity struct {
	ID              string              `gorm:"type:varchar(36);primaryKey" json:"id"`
	MarketplaceID   string              `gorm:"type:varchar(36);not null" json:"marketplace_id"`
	Name            string              `gorm:"type:varchar(255);not null" json:"name"`
	Position        int                 `gorm:"not null;default:0" json:"-"`
	Version         string              `gorm:"type:varchar(64)" json:"version,omitempty"`
	Description     string              `gorm:"type:text" json:"description,omitempty"`
	Category        string              `gorm:"type:varchar(255)" json:"category,omitempty"`
	Keywords        PluginStringList    `gorm:"type:json" json:"keywords,omitempty"`
	RequiresSandbox bool                `gorm:"not null;default:false" json:"requires_sandbox"`
	EnvDeclarations PluginEnvVars       `gorm:"type:json" json:"env_declarations,omitempty"`
	MCPServers      PluginMCPServers    `gorm:"column:mcp_servers;type:json" json:"mcp_servers,omitempty"`
	MCPWarnings     PluginStringList    `gorm:"column:mcp_warnings;type:json" json:"mcp_warnings,omitempty"`
	Skills          PluginPackageSkills `gorm:"type:json" json:"skills,omitempty"`
	CreatedAt       time.Time           `gorm:"not null" json:"created_at"`
}

func (PluginPackageEntity) TableName() string {
	return "plugin_packages"
}

// PluginTenantBindingEntity is the hard-delete ledger of what a tenant holds
// through the plugin system. A kind=plugin row is the tenant's install of a
// plugin package and is the only kind using Version, Status, Error and
// SandboxConfigIDs; mcp_service and skill_catalog rows point at it through
// PluginInstallID.
type PluginTenantBindingEntity struct {
	ID               string                 `gorm:"type:varchar(36);primaryKey" json:"id"`
	TenantID         uint64                 `gorm:"not null;index:idx_plugin_tenant_bindings_tenant" json:"tenant_id"`
	Kind             string                 `gorm:"type:varchar(32);not null" json:"kind"`
	ResourceID       string                 `gorm:"type:varchar(36);not null" json:"resource_id"`
	ResourceName     string                 `gorm:"type:varchar(255);not null" json:"resource_name"`
	PluginInstallID  string                 `gorm:"type:varchar(36);not null;default:''" json:"plugin_install_id,omitempty"`
	Version          string                 `gorm:"type:varchar(64);not null;default:''" json:"version,omitempty"`
	Status           string                 `gorm:"type:varchar(32);not null;default:''" json:"status,omitempty"`
	Error            string                 `gorm:"type:text;not null;default:''" json:"error,omitempty"`
	SandboxConfigIDs PluginSandboxConfigIDs `gorm:"type:json" json:"sandbox_config_ids,omitempty"`
	CreatedAt        time.Time              `gorm:"not null" json:"created_at"`
	UpdatedAt        time.Time              `gorm:"not null" json:"updated_at"`
}

func (PluginTenantBindingEntity) TableName() string {
	return "plugin_tenant_bindings"
}

// --- Manifest Data Structures ---

// MarketplaceOwner represents the owner info in marketplace.json.
type MarketplaceOwner struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// MarketplaceMetadata represents metadata in marketplace.json.
type MarketplaceMetadata struct {
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
}

// MarketplacePluginEntry represents an entry in marketplace.json plugins list.
type MarketplacePluginEntry struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
}

// MarketplaceManifest represents the complete marketplace.json.
type MarketplaceManifest struct {
	Name     string                   `json:"name"`
	Owner    MarketplaceOwner         `json:"owner,omitempty"`
	Metadata MarketplaceMetadata      `json:"metadata,omitempty"`
	Plugins  []MarketplacePluginEntry `json:"plugins"`
}

// PluginAuthor represents author information in plugin.json.
type PluginAuthor struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// PluginEnvVar represents an environment variable declared in plugin.json.
type PluginEnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PluginManifest represents plugin.json.
type PluginManifest struct {
	Name        string         `json:"name"`
	Version     string         `json:"version,omitempty"`
	Description string         `json:"description,omitempty"`
	Author      PluginAuthor   `json:"author,omitempty"`
	License     string         `json:"license,omitempty"`
	Keywords    []string       `json:"keywords,omitempty"`
	Skills      string         `json:"skills,omitempty"`     // defaults to ./skills/
	MCPServers  string         `json:"mcpServers,omitempty"` // defaults to ./.mcp.json
	Env         []PluginEnvVar `json:"env,omitempty"`
}

// PluginMCPServerEntry represents one server definition inside .mcp.json.
type PluginMCPServerEntry struct {
	Type    string            `json:"type"` // http | sse
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
}

// PluginMCPServersConfig represents the structure of .mcp.json.
type PluginMCPServersConfig struct {
	MCPServers map[string]PluginMCPServerEntry `json:"mcpServers"`
}

// PluginInstall is a tenant's install of a plugin package: a kind=plugin
// binding joined with its package. It is a read model, not a table.
type PluginInstall struct {
	ID               string                 `json:"id"`
	TenantID         uint64                 `json:"tenant_id"`
	MarketplaceID    string                 `json:"marketplace_id"`
	PackageID        string                 `json:"package_id"`
	PluginName       string                 `json:"plugin_name"`
	Version          string                 `json:"version,omitempty"`
	Description      string                 `json:"description,omitempty"`
	SandboxConfigIDs PluginSandboxConfigIDs `json:"sandbox_config_ids"`
	Status           string                 `json:"status"`
	Error            string                 `json:"error,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// PluginInfoForResource represents plugin attribution returned in MCP/Skill queries.
type PluginInfoForResource struct {
	PluginInstallID string `json:"plugin_install_id"`
	PluginName      string `json:"plugin_name"`
	MarketplaceID   string `json:"marketplace_id"`
	MarketplaceName string `json:"marketplace_name,omitempty"`
}
