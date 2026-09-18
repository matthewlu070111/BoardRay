package agent

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testUUID = "7b0f8cf8-08c6-4d9e-9f42-89f2a605d84e"

func TestRealitySecrets(t *testing.T) {
	value, err := NewRealitySecrets()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := realityPublicKey(value.PrivateKey); err != nil || got != value.PublicKey {
		t.Fatalf("public key = %q, %v", got, err)
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(value.PrivateKey); err != nil || len(decoded) != 32 || len(value.ShortID) != 16 {
		t.Fatalf("invalid secrets: %+v", value)
	}
}

func TestGeneratedConfigAcceptedByXray(t *testing.T) {
	binary := os.Getenv("BOARDRAY_TEST_XRAY")
	if binary == "" {
		t.Skip("BOARDRAY_TEST_XRAY is not set")
	}
	directory := t.TempDir()
	certPath, keyPath := writeTestCertificate(t, directory, "tls.example.com")
	secrets, err := NewRealitySecrets()
	if err != nil {
		t.Fatal(err)
	}
	data, err := renderXray([]InboundSpec{
		{Source: "boardless", ID: "tls", Listen: "127.0.0.1", Port: 18443, Security: "tls", ServerName: "tls.example.com", Certificate: certPath, PrivateKey: keyPath, Clients: []ClientSpec{{UUID: testUUID, Email: "br-user-a"}}},
		{Source: "vps-panel", ID: "2", Listen: "127.0.0.1", Port: 18444, Security: "reality", ServerName: "www.microsoft.com", RealityTarget: "www.microsoft.com:443", PrivateKey: secrets.PrivateKey, ShortID: secrets.ShortID, Clients: []ClientSpec{{UUID: testUUID, Email: "vp-client-2"}}},
	}, "127.0.0.1:19085", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "run", "-test", "-config", configPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Xray rejected generated config: %v\n%s", err, output)
	}
}

func writeTestCertificate(t *testing.T, directory, domain string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestBoardLessInboundFiltersUsers(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	var snapshot BoardLessSnapshot
	snapshot.Node.ID, snapshot.Node.Protocol, snapshot.Node.Status = "node_1", "vless", "approved"
	snapshot.Node.Config = NodeConfig{Port: 443, Transport: "tcp", TLS: true, SNI: "node.example.com", Flow: "xtls-rprx-vision"}
	snapshot.Users = []BoardLessUser{
		{ID: "active", UUID: testUUID, ExpiresAt: now.Unix() + 10, QuotaBytes: 100, UsedBytes: 1},
		{ID: "expired", UUID: testUUID, ExpiresAt: now.Unix() - 1, QuotaBytes: 100},
		{ID: "spent", UUID: testUUID, ExpiresAt: now.Unix() + 10, QuotaBytes: 100, UsedBytes: 100},
	}
	value, enabled, err := boardLessInbound(&snapshot, &BoardLessConfig{Preset: PresetTLS, Domain: "node.example.com", ACMEEmail: "ops@example.com"}, now)
	if err != nil || !enabled || len(value.Clients) != 1 || value.Clients[0].Email != boardLessStatsID("active") {
		t.Fatalf("inbound = %+v, %v, %v", value, enabled, err)
	}
}

func TestMergeBoardLessWinsPortConflict(t *testing.T) {
	boardless := &InboundSpec{Source: "boardless", ID: "one", Port: 443}
	vps := []InboundSpec{{Source: "vps-panel", ID: "conflict", Port: 443}, {Source: "vps-panel", ID: "ok", Port: 8443}}
	values, err := mergeInbounds(boardless, vps, nil)
	if len(values) != 2 || values[0].Source != "boardless" || values[1].ID != "ok" || err == nil {
		t.Fatalf("merge = %+v, %v", values, err)
	}
}

func TestRenderTLSAndReality(t *testing.T) {
	values := []InboundSpec{
		{Source: "boardless", ID: "tls", Listen: "0.0.0.0", Port: 443, Security: "tls", ServerName: "tls.example.com", Certificate: "/cert", PrivateKey: "/key", Clients: []ClientSpec{{UUID: testUUID, Email: "br-user-a"}}},
		{Source: "vps-panel", ID: "2", Listen: "0.0.0.0", Port: 8443, Security: "reality", ServerName: "www.example.com", RealityTarget: "www.example.com:443", PrivateKey: "private", ShortID: "0123456789abcdef", Clients: []ClientSpec{{UUID: testUUID, Email: "vp-client-2"}}},
	}
	data, err := renderXray(values, "127.0.0.1:10085", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	var rendered renderedConfig
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatal(err)
	}
	if len(rendered.Inbounds) != 2 || rendered.Inbounds[0].StreamSettings.Network != "tcp" || len(rendered.Inbounds[0].Settings.Fallbacks) != 1 || rendered.Inbounds[1].StreamSettings.Network != "raw" {
		t.Fatalf("rendered config = %+v", rendered)
	}
}

func TestStatsAndPersistentDeltas(t *testing.T) {
	statsID := boardLessStatsID("usr_1")
	data := []byte(`{"stat":[{"name":"user>>>` + statsID + `>>>traffic>>>uplink","value":"10"},{"name":"user>>>` + statsID + `>>>traffic>>>downlink","value":20}]}`)
	current, err := parseStats(data)
	if err != nil {
		t.Fatal(err)
	}
	state := persistentState{Counters: map[string]Counter{statsID: {Uplink: 7, Downlink: 30}}}
	var snapshot BoardLessSnapshot
	snapshot.Node.ID = "node_1"
	updateBoardLessUsage(&state, &snapshot, current, time.Unix(1, 0))
	if len(state.Pending) != 1 || state.Pending[0].Entries[0].UpBytes != 3 || state.Pending[0].Entries[0].DownBytes != 20 {
		t.Fatalf("pending = %+v", state.Pending)
	}
}

func TestXrayValidationFailureKeepsCurrentConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &xrayManager{config: RuntimeConfig{XrayBinary: "xray", XrayConfig: configPath, XrayPrevious: filepath.Join(directory, "previous.json"), XrayService: "xray", StatsAddress: "127.0.0.1:10085", FallbackAddress: "127.0.0.1:18080"}}
	manager.run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "xray" {
			return []byte("bad candidate"), errors.New("exit 1")
		}
		return nil, nil
	}
	manager.probe = func(context.Context, string) error { return nil }
	_, err := manager.apply(context.Background(), []InboundSpec{{Source: "vps-panel", ID: "1", Listen: "0.0.0.0", Port: 443, Security: "reality", ServerName: "www.example.com", RealityTarget: "www.example.com:443", PrivateKey: "private", ShortID: "0123456789abcdef"}})
	if err == nil || !strings.Contains(err.Error(), "bad candidate") {
		t.Fatalf("apply error = %v", err)
	}
	data, _ := os.ReadFile(configPath)
	if string(data) != "old\n" {
		t.Fatalf("current config changed: %q", data)
	}
}
