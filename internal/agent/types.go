package agent

import (
	"encoding/json"
	"time"
)

const (
	PresetTLS       = "vless-tcp-xtls-vision"
	PresetReality   = "vless-tcp-xtls-vision-reality"
	VPSPanelVersion = "v0.25.0"
)

type Config struct {
	Mode      string           `json:"mode"`
	BoardLess *BoardLessConfig `json:"boardless,omitempty"`
	VPSPanel  *VPSPanelConfig  `json:"vps_panel,omitempty"`
	Runtime   RuntimeConfig    `json:"runtime"`
}

type BoardLessConfig struct {
	PanelURL          string `json:"panel_url"`
	NodeToken         string `json:"node_token"`
	Preset            string `json:"preset"`
	Domain            string `json:"domain,omitempty"`
	ACMEEmail         string `json:"acme_email,omitempty"`
	RealityTarget     string `json:"reality_target,omitempty"`
	RealityPrivateKey string `json:"reality_private_key,omitempty"`
	RealityPublicKey  string `json:"reality_public_key,omitempty"`
	RealityShortID    string `json:"reality_short_id,omitempty"`
}

type VPSPanelConfig struct {
	PanelURL         string `json:"panel_url"`
	EnrollmentToken  string `json:"enrollment_token,omitempty"`
	AgentToken       string `json:"agent_token,omitempty"`
	AgentID          int64  `json:"agent_id,omitempty"`
	ServerID         int64  `json:"server_id,omitempty"`
	AnnouncedVersion string `json:"announced_version"`
}

type RuntimeConfig struct {
	StatePath             string `json:"state_path"`
	XrayBinary            string `json:"xray_binary"`
	XrayConfig            string `json:"xray_config"`
	XrayService           string `json:"xray_service"`
	CertDir               string `json:"cert_dir"`
	ACMEScript            string `json:"acme_script"`
	ACMEHome              string `json:"acme_home"`
	FallbackAddress       string `json:"fallback_address"`
	FallbackH2Address     string `json:"fallback_h2_address"`
	FallbackProxyProtocol bool   `json:"fallback_proxy_protocol"`
	FallbackSite          string `json:"fallback_site"`
	FallbackAlwaysOn      bool   `json:"fallback_always_on"`
	StatsAddress          string `json:"stats_address"`
	StaleGraceSeconds     int64  `json:"stale_grace_seconds"`
}

type BoardLessSnapshot struct {
	Node struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		Protocol  string     `json:"protocol"`
		Status    string     `json:"status"`
		Config    NodeConfig `json:"config"`
		UpdatedAt int64      `json:"updatedAt"`
	} `json:"node"`
	Users       []BoardLessUser `json:"users"`
	GeneratedAt int64           `json:"generatedAt"`
}

type NodeConfig struct {
	Server           string `json:"server"`
	Port             int    `json:"port"`
	Transport        string `json:"transport"`
	TLS              bool   `json:"tls"`
	SNI              string `json:"sni"`
	Flow             string `json:"flow"`
	RealityPublicKey string `json:"realityPublicKey"`
	ShortID          string `json:"shortId"`
}

type BoardLessUser struct {
	ID         string `json:"id"`
	UUID       string `json:"uuid"`
	Secret     string `json:"secret"`
	ExpiresAt  int64  `json:"expiresAt"`
	QuotaBytes int64  `json:"quotaBytes"`
	UsedBytes  int64  `json:"usedBytes"`
}

type VPSDesiredState struct {
	Version int64 `json:"version"`
	Xray    struct {
		Enabled bool              `json:"enabled"`
		Proxies []VPSDesiredProxy `json:"proxies"`
	} `json:"xray"`
	Realm struct {
		Enabled bool              `json:"enabled"`
		Relays  []json.RawMessage `json:"relays"`
	} `json:"realm"`
}

type VPSDesiredProxy struct {
	ID         int64              `json:"id"`
	Listen     string             `json:"listen"`
	Port       int                `json:"port"`
	Protocol   string             `json:"protocol"`
	Transport  string             `json:"transport"`
	Security   string             `json:"security"`
	ServerFlow string             `json:"server_flow"`
	ServerName string             `json:"server_name"`
	TLS        *VPSDesiredTLS     `json:"tls,omitempty"`
	Reality    *VPSDesiredReality `json:"reality,omitempty"`
	Clients    []VPSDesiredClient `json:"clients"`
}

type VPSDesiredTLS struct {
	Mode        string `json:"mode"`
	Certificate string `json:"certificate,omitempty"`
	PrivateKey  string `json:"private_key,omitempty"`
}

type VPSDesiredReality struct {
	Target     string `json:"target"`
	PrivateKey string `json:"private_key"`
	ShortID    string `json:"short_id"`
}

type VPSDesiredClient struct {
	ID      int64  `json:"id"`
	StatsID string `json:"stats_id"`
	UUID    string `json:"uuid"`
}

type InboundSpec struct {
	Source        string
	ID            string
	Listen        string
	Port          int
	Security      string
	ServerName    string
	Certificate   string
	PrivateKey    string
	ACMEEmail     string
	RealityTarget string
	ShortID       string
	Clients       []ClientSpec
}

type ClientSpec struct {
	UUID  string
	Email string
}

type Counter struct {
	Uplink   int64 `json:"uplink"`
	Downlink int64 `json:"downlink"`
}

type PendingUsage struct {
	ReportID string       `json:"report_id"`
	Entries  []UsageEntry `json:"entries"`
}

type UsageEntry struct {
	UserID    string `json:"userId"`
	UpBytes   int64  `json:"upBytes"`
	DownBytes int64  `json:"downBytes"`
}

type persistentState struct {
	BoardLessSnapshot  *BoardLessSnapshot `json:"boardless_snapshot,omitempty"`
	BoardLessFetched   time.Time          `json:"boardless_fetched,omitempty"`
	VPSDesired         *VPSDesiredState   `json:"vps_desired,omitempty"`
	Counters           map[string]Counter `json:"counters,omitempty"`
	Pending            []PendingUsage     `json:"pending,omitempty"`
	AppliedHash        string             `json:"applied_hash,omitempty"`
	VPSReportedVersion int64              `json:"vps_reported_version,omitempty"`
}
