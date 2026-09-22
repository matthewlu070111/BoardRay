package agent

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type RealitySecrets struct {
	PrivateKey string
	PublicKey  string
	ShortID    string
}

func NewRealitySecrets() (RealitySecrets, error) {
	privateBytes := make([]byte, 32)
	if _, err := rand.Read(privateBytes); err != nil {
		return RealitySecrets{}, fmt.Errorf("generate REALITY private key: %w", err)
	}
	privateBytes[0] &= 248
	privateBytes[31] &= 127
	privateBytes[31] |= 64
	privateKey, err := ecdh.X25519().NewPrivateKey(privateBytes)
	if err != nil {
		return RealitySecrets{}, fmt.Errorf("create REALITY private key: %w", err)
	}
	shortID := make([]byte, 8)
	if _, err := rand.Read(shortID); err != nil {
		return RealitySecrets{}, fmt.Errorf("generate REALITY short id: %w", err)
	}
	return RealitySecrets{
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateBytes),
		PublicKey:  base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
		ShortID:    hex.EncodeToString(shortID),
	}, nil
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var value Config
	if err := json.Unmarshal(data, &value); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	defaults(&value)
	if err := validateConfig(value); err != nil {
		return Config{}, err
	}
	return value, nil
}

func defaults(value *Config) {
	if value.Runtime.StatePath == "" {
		value.Runtime.StatePath = "/var/lib/boardray/state.json"
	}
	if value.Runtime.XrayBinary == "" {
		value.Runtime.XrayBinary = "/opt/boardray/xray/xray"
	}
	if value.Runtime.XrayConfig == "" {
		value.Runtime.XrayConfig = "/etc/boardray/xray/config.json"
	}
	if value.Runtime.XrayService == "" {
		value.Runtime.XrayService = "boardray-xray.service"
	}
	if value.Runtime.CertDir == "" {
		value.Runtime.CertDir = "/etc/boardray/xray/certs"
	}
	if value.Runtime.ACMEScript == "" {
		value.Runtime.ACMEScript = "/opt/boardray/acme/acme.sh"
	}
	if value.Runtime.ACMEHome == "" {
		value.Runtime.ACMEHome = "/var/lib/boardray/acme"
	}
	if value.Runtime.FallbackAddress == "" {
		value.Runtime.FallbackAddress = "127.0.0.1:18080"
	}
	if value.Runtime.FallbackSite == "" {
		value.Runtime.FallbackSite = "www.lovelive-anime.jp"
	}
	if value.Runtime.StatsAddress == "" {
		value.Runtime.StatsAddress = "127.0.0.1:10085"
	}
	if value.Runtime.StaleGraceSeconds == 0 {
		value.Runtime.StaleGraceSeconds = 900
	}
	if value.VPSPanel != nil && value.VPSPanel.AnnouncedVersion == "" {
		value.VPSPanel.AnnouncedVersion = VPSPanelVersion
	}
}

func validateConfig(value Config) error {
	if value.Mode != "boardless" && value.Mode != "vps-panel" && value.Mode != "both" {
		return errors.New("mode must be boardless, vps-panel, or both")
	}
	if value.Mode != "vps-panel" {
		if value.BoardLess == nil || value.BoardLess.NodeToken == "" {
			return errors.New("BoardLess node token is required")
		}
		if err := validHTTPURL(value.BoardLess.PanelURL); err != nil {
			return fmt.Errorf("invalid BoardLess panel URL: %w", err)
		}
		if value.BoardLess.Preset != PresetTLS && value.BoardLess.Preset != PresetReality {
			return errors.New("unsupported BoardLess preset")
		}
		if value.BoardLess.Preset == PresetTLS && (value.BoardLess.Domain == "" || value.BoardLess.ACMEEmail == "") {
			return errors.New("TLS preset requires domain and ACME email")
		}
		if value.BoardLess.Preset == PresetReality {
			if value.BoardLess.RealityTarget == "" || value.BoardLess.RealityPrivateKey == "" || value.BoardLess.RealityShortID == "" {
				return errors.New("REALITY preset requires target, private key, and short ID")
			}
			if !validRealityTarget(value.BoardLess.RealityTarget) || !validShortID(value.BoardLess.RealityShortID) {
				return errors.New("REALITY target or short ID is invalid")
			}
			publicKey, err := realityPublicKey(value.BoardLess.RealityPrivateKey)
			if err != nil || publicKey != value.BoardLess.RealityPublicKey {
				return errors.New("REALITY key pair is invalid")
			}
		}
	}
	if value.Mode != "boardless" {
		if value.VPSPanel == nil || value.VPSPanel.PanelURL == "" || (value.VPSPanel.AgentToken == "" && value.VPSPanel.EnrollmentToken == "") {
			return errors.New("vps-panel URL and agent or enrollment token are required")
		}
		if err := validHTTPURL(value.VPSPanel.PanelURL); err != nil {
			return fmt.Errorf("invalid vps-panel URL: %w", err)
		}
	}
	if value.Runtime.StaleGraceSeconds < 0 {
		return errors.New("stale grace must not be negative")
	}
	for name, address := range map[string]string{"stats": value.Runtime.StatsAddress, "fallback": value.Runtime.FallbackAddress} {
		if _, _, err := splitAddress(address); err != nil {
			return fmt.Errorf("invalid %s address: %w", name, err)
		}
	}
	if value.Runtime.FallbackProxyProtocol {
		if _, _, err := splitAddress(value.Runtime.FallbackH2Address); err != nil {
			return fmt.Errorf("invalid HTTP/2 fallback address: %w", err)
		}
	}
	if strings.TrimSpace(value.Runtime.FallbackSite) == "" || strings.ContainsAny(value.Runtime.FallbackSite, "/?#") {
		return errors.New("fallback site must be a hostname")
	}
	if value.Runtime.FallbackAlwaysOn && (value.BoardLess == nil || value.BoardLess.Preset != PresetTLS) {
		return errors.New("always-on fallback requires the BoardLess TLS preset")
	}
	return nil
}

func validRealityTarget(value string) bool {
	host, portValue, err := net.SplitHostPort(value)
	if err != nil || strings.TrimSpace(host) == "" {
		return false
	}
	port, err := strconv.Atoi(portValue)
	return err == nil && port >= 1 && port <= 65535
}

func validShortID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 8
}

func validHTTPURL(value string) error {
	parsed, err := url.Parse(strings.TrimRight(value, "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must be an HTTP(S) origin without query or fragment")
	}
	return nil
}

func realityPublicKey(private string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(private)
	if err != nil || len(data) != 32 {
		return "", errors.New("invalid REALITY private key")
	}
	key, err := ecdh.X25519().NewPrivateKey(data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func saveConfig(path string, value Config) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o600)
}

func loadState(path string) (persistentState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return persistentState{Counters: map[string]Counter{}}, nil
	}
	if err != nil {
		return persistentState{}, err
	}
	var state persistentState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistentState{}, fmt.Errorf("decode state: %w", err)
	}
	if state.Counters == nil {
		state.Counters = map[string]Counter{}
	}
	return state, nil
}

func saveState(path string, state persistentState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".boardray-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
