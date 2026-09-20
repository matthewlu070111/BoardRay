package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type renderedConfig struct {
	Log struct {
		LogLevel string `json:"loglevel"`
	} `json:"log"`
	API struct {
		Tag      string   `json:"tag"`
		Services []string `json:"services"`
	} `json:"api"`
	DNS struct {
		Servers []string `json:"servers"`
	} `json:"dns"`
	Routing renderedRouting `json:"routing"`
	Policy  struct {
		Levels map[string]renderedLevel `json:"levels"`
		System renderedSystem           `json:"system"`
	} `json:"policy"`
	Stats     struct{}           `json:"stats"`
	Inbounds  []renderedInbound  `json:"inbounds"`
	Outbounds []renderedOutbound `json:"outbounds"`
}

type renderedLevel struct {
	Handshake         int  `json:"handshake"`
	ConnIdle          int  `json:"connIdle"`
	StatsUserUplink   bool `json:"statsUserUplink"`
	StatsUserDownlink bool `json:"statsUserDownlink"`
}

type renderedSystem struct {
	StatsInboundUplink    bool `json:"statsInboundUplink"`
	StatsInboundDownlink  bool `json:"statsInboundDownlink"`
	StatsOutboundUplink   bool `json:"statsOutboundUplink"`
	StatsOutboundDownlink bool `json:"statsOutboundDownlink"`
}

type renderedRouting struct {
	DomainStrategy string                `json:"domainStrategy"`
	Rules          []renderedRoutingRule `json:"rules"`
}

type renderedRoutingRule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	OutboundTag string   `json:"outboundTag"`
}

type renderedInbound struct {
	Tag            string                  `json:"tag"`
	Listen         string                  `json:"listen"`
	Port           int                     `json:"port"`
	Protocol       string                  `json:"protocol"`
	Settings       renderedSettings        `json:"settings"`
	StreamSettings *renderedStreamSettings `json:"streamSettings,omitempty"`
	Sniffing       *renderedSniffing       `json:"sniffing,omitempty"`
}

type renderedSettings struct {
	Address    string             `json:"address,omitempty"`
	Clients    []renderedClient   `json:"clients,omitempty"`
	Decryption string             `json:"decryption,omitempty"`
	Fallbacks  []renderedFallback `json:"fallbacks,omitempty"`
}

type renderedSniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
}

type renderedClient struct {
	ID    string `json:"id"`
	Flow  string `json:"flow"`
	Email string `json:"email"`
}

type renderedFallback struct {
	Dest string `json:"dest"`
	ALPN string `json:"alpn,omitempty"`
	Xver int    `json:"xver"`
}

type renderedStreamSettings struct {
	Network         string                   `json:"network"`
	Security        string                   `json:"security"`
	TLSSettings     *renderedTLSSettings     `json:"tlsSettings,omitempty"`
	RealitySettings *renderedRealitySettings `json:"realitySettings,omitempty"`
}

type renderedTLSSettings struct {
	RejectUnknownSNI bool                     `json:"rejectUnknownSni"`
	MinVersion       string                   `json:"minVersion"`
	ServerName       string                   `json:"serverName"`
	Certificates     []renderedTLSCertificate `json:"certificates"`
}

type renderedTLSCertificate struct {
	CertificateFile string `json:"certificateFile"`
	KeyFile         string `json:"keyFile"`
	OCSPStapling    int    `json:"ocspStapling"`
}

type renderedRealitySettings struct {
	Show        bool     `json:"show"`
	Target      string   `json:"target"`
	Xver        int      `json:"xver"`
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	ShortIDs    []string `json:"shortIds"`
}

type renderedOutbound struct {
	Protocol string `json:"protocol"`
	Tag      string `json:"tag"`
}

func renderXray(specs []InboundSpec, statsAddress, fallbackAddress, fallbackH2Address string, fallbackProxyProtocol bool) ([]byte, error) {
	config := renderedConfig{}
	config.Log.LogLevel = "warning"
	config.API.Tag, config.API.Services = "api", []string{"HandlerService", "LoggerService", "StatsService"}
	config.DNS.Servers = []string{"https://1.1.1.1/dns-query"}
	config.Routing = renderedRouting{DomainStrategy: "IPIfNonMatch", Rules: []renderedRoutingRule{
		{Type: "field", InboundTag: []string{"api"}, OutboundTag: "api"},
		{Type: "field", IP: []string{"geoip:cn", "geoip:private"}, OutboundTag: "block"},
		{Type: "field", Protocol: []string{"bittorrent"}, OutboundTag: "block"},
		{Type: "field", Domain: []string{"geosite:category-ads-all"}, OutboundTag: "block"},
	}}
	config.Policy.Levels = map[string]renderedLevel{"0": {Handshake: 2, ConnIdle: 220, StatsUserUplink: true, StatsUserDownlink: true}}
	config.Policy.System = renderedSystem{StatsInboundUplink: true, StatsInboundDownlink: true, StatsOutboundUplink: true, StatsOutboundDownlink: true}
	statsHost, statsPort, err := splitAddress(statsAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid stats address: %w", err)
	}
	config.Inbounds = []renderedInbound{{
		Tag: "api", Listen: statsHost, Port: statsPort, Protocol: "dokodemo-door",
		Settings: renderedSettings{Address: statsHost},
	}}
	config.Outbounds = []renderedOutbound{{Protocol: "freedom", Tag: "direct"}, {Protocol: "blackhole", Tag: "block"}}
	ports := map[int]bool{}
	for _, spec := range specs {
		if ports[spec.Port] {
			return nil, fmt.Errorf("duplicate Xray port %d", spec.Port)
		}
		ports[spec.Port] = true
		inbound := renderedInbound{
			Tag: "boardray-" + spec.Source + "-" + spec.ID, Listen: spec.Listen, Port: spec.Port, Protocol: "vless",
			Settings:       renderedSettings{Decryption: "none", Clients: make([]renderedClient, 0, len(spec.Clients))},
			StreamSettings: &renderedStreamSettings{Security: spec.Security},
			Sniffing:       &renderedSniffing{Enabled: true, DestOverride: []string{"http", "tls", "quic"}},
		}
		for _, client := range spec.Clients {
			inbound.Settings.Clients = append(inbound.Settings.Clients, renderedClient{ID: client.UUID, Flow: "xtls-rprx-vision", Email: client.Email})
		}
		switch spec.Security {
		case "tls":
			if spec.Certificate == "" || spec.PrivateKey == "" {
				return nil, fmt.Errorf("TLS certificate missing for %s", spec.ServerName)
			}
			inbound.StreamSettings.Network = "tcp"
			inbound.StreamSettings.TLSSettings = &renderedTLSSettings{
				RejectUnknownSNI: true, MinVersion: "1.3", ServerName: spec.ServerName,
				Certificates: []renderedTLSCertificate{{CertificateFile: spec.Certificate, KeyFile: spec.PrivateKey, OCSPStapling: 3600}},
			}
			inbound.Settings.Fallbacks = []renderedFallback{{Dest: fallbackAddress}}
			if fallbackProxyProtocol {
				inbound.Settings.Fallbacks[0].Xver = 1
				inbound.Settings.Fallbacks = append(inbound.Settings.Fallbacks, renderedFallback{ALPN: "h2", Dest: fallbackH2Address, Xver: 1})
			}
		case "reality":
			if spec.RealityTarget == "" || spec.PrivateKey == "" || spec.ShortID == "" {
				return nil, fmt.Errorf("REALITY secrets missing for %s", spec.ServerName)
			}
			inbound.StreamSettings.Network = "raw"
			inbound.StreamSettings.RealitySettings = &renderedRealitySettings{
				Show: false, Target: spec.RealityTarget, Xver: 0, ServerNames: []string{spec.ServerName},
				PrivateKey: spec.PrivateKey, ShortIDs: []string{spec.ShortID},
			}
		default:
			return nil, fmt.Errorf("unsupported security %q", spec.Security)
		}
		config.Inbounds = append(config.Inbounds, inbound)
	}
	return json.MarshalIndent(config, "", "  ")
}

func splitAddress(address string) (string, int, error) {
	host, portValue, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 || strings.TrimSpace(host) == "" {
		return "", 0, errors.New("must contain a host and port")
	}
	return host, port, nil
}

type xrayManager struct {
	config RuntimeConfig
	run    func(context.Context, string, ...string) ([]byte, error)
	probe  func(context.Context, string) error
}

func newXrayManager(config RuntimeConfig) *xrayManager {
	return &xrayManager{config: config, run: runCommand, probe: probeAddress}
}

func (m *xrayManager) prepareCertificates(ctx context.Context, specs []InboundSpec) ([]InboundSpec, bool, error) {
	values := append([]InboundSpec(nil), specs...)
	renewed := false
	for index := range values {
		if values[index].Security != "tls" {
			continue
		}
		certificate, key, changed, err := ensureCertificate(ctx, m.config, values[index].ServerName, values[index].ACMEEmail, m.run)
		if err != nil {
			return nil, false, err
		}
		renewed = renewed || changed
		values[index].Certificate, values[index].PrivateKey = certificate, key
	}
	return values, renewed, nil
}

func (m *xrayManager) apply(ctx context.Context, specs []InboundSpec) (string, error) {
	prepared, certificateChanged, err := m.prepareCertificates(ctx, specs)
	if err != nil {
		return "", err
	}
	data, err := renderXray(prepared, m.config.StatsAddress, m.config.FallbackAddress, m.config.FallbackH2Address, m.config.FallbackProxyProtocol)
	if err != nil {
		return "", err
	}
	hashBytes := sha256.Sum256(data)
	hash := hex.EncodeToString(hashBytes[:])
	current, currentErr := os.ReadFile(m.config.XrayConfig)
	if currentErr == nil && bytes.Equal(bytes.TrimSpace(current), bytes.TrimSpace(data)) {
		if certificateChanged {
			if output, restartErr := m.run(ctx, "systemctl", "restart", m.config.XrayService); restartErr != nil {
				return "", fmt.Errorf("restart Xray after certificate renewal: %w: %s", restartErr, truncate(output))
			}
		} else if _, err := m.run(ctx, "systemctl", "is-active", "--quiet", m.config.XrayService); err != nil {
			if output, startErr := m.run(ctx, "systemctl", "start", m.config.XrayService); startErr != nil {
				return "", fmt.Errorf("start Xray: %w: %s", startErr, truncate(output))
			}
		}
		if err := m.probeSpecs(ctx, prepared); err != nil {
			return "", err
		}
		return hash, nil
	}
	if currentErr != nil && !errors.Is(currentErr, os.ErrNotExist) {
		return "", currentErr
	}
	if err := os.MkdirAll(filepath.Dir(m.config.XrayConfig), 0o700); err != nil {
		return "", err
	}
	candidate, err := os.CreateTemp(filepath.Dir(m.config.XrayConfig), ".candidate-*.json")
	if err != nil {
		return "", err
	}
	candidatePath := candidate.Name()
	defer os.Remove(candidatePath)
	if err := candidate.Chmod(0o600); err != nil {
		candidate.Close()
		return "", err
	}
	if _, err := candidate.Write(append(data, '\n')); err != nil {
		candidate.Close()
		return "", err
	}
	if err := candidate.Close(); err != nil {
		return "", err
	}
	if output, err := m.run(ctx, m.config.XrayBinary, "run", "-test", "-config", candidatePath); err != nil {
		return "", fmt.Errorf("Xray rejected candidate: %w: %s", err, truncate(output))
	}
	hadCurrent := currentErr == nil
	if hadCurrent {
		if err := atomicWrite(m.config.XrayPrevious, current, 0o600); err != nil {
			return "", err
		}
	}
	if err := atomicWrite(m.config.XrayConfig, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	if output, err := m.run(ctx, "systemctl", "enable", m.config.XrayService); err != nil {
		return "", m.rollback(ctx, hadCurrent, fmt.Errorf("enable Xray: %w: %s", err, truncate(output)))
	}
	if output, err := m.run(ctx, "systemctl", "restart", m.config.XrayService); err != nil {
		return "", m.rollback(ctx, hadCurrent, fmt.Errorf("restart Xray: %w: %s", err, truncate(output)))
	}
	if err := m.probeSpecs(ctx, prepared); err != nil {
		return "", m.rollback(ctx, hadCurrent, err)
	}
	return hash, nil
}

func (m *xrayManager) probeSpecs(ctx context.Context, specs []InboundSpec) error {
	for _, spec := range specs {
		address := net.JoinHostPort("127.0.0.1", fmt.Sprint(spec.Port))
		var probeErr error
		for attempt := 0; attempt < 10; attempt++ {
			probeContext, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			probeErr = m.probe(probeContext, address)
			cancel()
			if probeErr == nil {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		if probeErr != nil {
			return fmt.Errorf("Xray listener %s unavailable", address)
		}
	}
	return nil
}

func (m *xrayManager) rollback(ctx context.Context, hadCurrent bool, applyErr error) error {
	if hadCurrent {
		previous, err := os.ReadFile(m.config.XrayPrevious)
		if err == nil {
			_ = atomicWrite(m.config.XrayConfig, previous, 0o600)
			_, _ = m.run(ctx, "systemctl", "restart", m.config.XrayService)
		}
	} else {
		_ = os.Remove(m.config.XrayConfig)
		_, _ = m.run(ctx, "systemctl", "stop", m.config.XrayService)
	}
	return applyErr
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	return command.CombinedOutput()
}

func probeAddress(ctx context.Context, address string) error {
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err == nil {
		connection.Close()
	}
	return err
}

func truncate(value []byte) string {
	value = bytes.TrimSpace(value)
	if len(value) > 1024 {
		value = value[:1024]
	}
	return string(value)
}

func mergeInbounds(boardless *InboundSpec, vps []InboundSpec, vpsErr error) ([]InboundSpec, error) {
	values := make([]InboundSpec, 0, len(vps)+1)
	ports := map[int]bool{}
	if boardless != nil {
		values = append(values, *boardless)
		ports[boardless.Port] = true
	}
	var errs []error
	if vpsErr != nil {
		errs = append(errs, vpsErr)
	}
	for _, value := range vps {
		if ports[value.Port] {
			errs = append(errs, fmt.Errorf("vps-panel proxy %s conflicts with BoardLess port %d", value.ID, value.Port))
			continue
		}
		ports[value.Port] = true
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Port == values[j].Port {
			return values[i].ID < values[j].ID
		}
		return values[i].Port < values[j].Port
	})
	return values, errors.Join(errs...)
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}
