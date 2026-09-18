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
	"strings"
	"time"
)

type renderedConfig struct {
	Log struct {
		LogLevel string `json:"loglevel"`
	} `json:"log"`
	API struct {
		Tag      string   `json:"tag"`
		Listen   string   `json:"listen"`
		Services []string `json:"services"`
	} `json:"api"`
	Policy struct {
		Levels map[string]renderedLevel `json:"levels"`
	} `json:"policy"`
	Stats     struct{}           `json:"stats"`
	Inbounds  []renderedInbound  `json:"inbounds"`
	Outbounds []renderedOutbound `json:"outbounds"`
}

type renderedLevel struct {
	StatsUserUplink   bool `json:"statsUserUplink"`
	StatsUserDownlink bool `json:"statsUserDownlink"`
}

type renderedInbound struct {
	Tag            string                 `json:"tag"`
	Listen         string                 `json:"listen"`
	Port           int                    `json:"port"`
	Protocol       string                 `json:"protocol"`
	Settings       renderedSettings       `json:"settings"`
	StreamSettings renderedStreamSettings `json:"streamSettings"`
}

type renderedSettings struct {
	Clients    []renderedClient   `json:"clients"`
	Decryption string             `json:"decryption"`
	Fallbacks  []renderedFallback `json:"fallbacks,omitempty"`
}

type renderedClient struct {
	ID    string `json:"id"`
	Flow  string `json:"flow"`
	Email string `json:"email"`
}

type renderedFallback struct {
	Dest string `json:"dest"`
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

func renderXray(specs []InboundSpec, statsAddress, fallbackAddress string) ([]byte, error) {
	config := renderedConfig{}
	config.Log.LogLevel = "warning"
	config.API.Tag, config.API.Listen, config.API.Services = "api", statsAddress, []string{"StatsService"}
	config.Policy.Levels = map[string]renderedLevel{"0": {StatsUserUplink: true, StatsUserDownlink: true}}
	config.Inbounds = make([]renderedInbound, 0, len(specs))
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
			StreamSettings: renderedStreamSettings{Security: spec.Security},
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
				RejectUnknownSNI: true, MinVersion: "1.2", ServerName: spec.ServerName,
				Certificates: []renderedTLSCertificate{{CertificateFile: spec.Certificate, KeyFile: spec.PrivateKey}},
			}
			inbound.Settings.Fallbacks = []renderedFallback{{Dest: fallbackAddress, Xver: 0}}
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
	data, err := renderXray(prepared, m.config.StatsAddress, m.config.FallbackAddress)
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
