package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBoardLessClientContract(t *testing.T) {
	var usageReportID string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer node-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		body, status := "", http.StatusOK
		switch r.URL.Path {
		case "/api/node/v1/config":
			body = `{"node":{"id":"node_1","name":"Node","protocol":"vless","status":"approved","config":{"server":"node.example.com","port":443,"transport":"tcp","tls":true,"sni":"node.example.com","flow":"xtls-rprx-vision"},"updatedAt":1},"users":[],"generatedAt":1}`
		case "/api/node/v1/heartbeat":
			body = `{"ok":true}`
		case "/api/node/v1/usage":
			var body struct {
				ReportID string `json:"reportId"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Fatal("invalid usage body")
			}
			usageReportID = body.ReportID
			return response(http.StatusOK, `{"accepted":true,"entries":1}`), nil
		default:
			status = http.StatusNotFound
		}
		return response(status, body), nil
	})}
	client := &boardLessClient{config: &BoardLessConfig{PanelURL: "https://boardless.example", NodeToken: "node-token"}, client: httpClient, version: "test"}
	if snapshot, err := client.fetch(context.Background()); err != nil || snapshot.Node.ID != "node_1" {
		t.Fatalf("fetch = %+v, %v", snapshot, err)
	}
	if err := client.heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	report := PendingUsage{ReportID: "stable-id", Entries: []UsageEntry{{UserID: "usr_1", UpBytes: 1}}}
	if err := client.upload(context.Background(), report); err != nil || usageReportID != "stable-id" {
		t.Fatalf("upload = %q, %v", usageReportID, err)
	}
}

func TestVPSPanelClientContract(t *testing.T) {
	var configStatus string
	var registeredVersion string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/agent/register":
			var body struct {
				AgentVersion string `json:"agent_version"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Fatal("invalid registration body")
			}
			registeredVersion = body.AgentVersion
			return response(http.StatusCreated, `{"agent_id":2,"server_id":3,"agent_token":"agent-token"}`), nil
		case "/api/agent/config":
			if r.Header.Get("Authorization") != "Bearer agent-token" {
				t.Fatal("missing vps-panel token")
			}
			return response(http.StatusOK, `{"version":7,"xray":{"enabled":false,"proxies":[]},"realm":{"enabled":false,"relays":[]}}`), nil
		case "/api/agent/config/result":
			var body struct {
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			configStatus = body.Status
			return response(http.StatusNoContent, ""), nil
		default:
			return response(http.StatusNotFound, ""), nil
		}
	})}
	config := &VPSPanelConfig{PanelURL: "https://vps.example", EnrollmentToken: "enrollment", AnnouncedVersion: VPSPanelVersion}
	client := &vpsPanelClient{config: config, client: httpClient}
	if err := client.register(context.Background()); err != nil || config.AgentToken != "agent-token" || config.EnrollmentToken != "" || registeredVersion != VPSPanelVersion {
		t.Fatalf("register = %+v, %v", config, err)
	}
	state, err := client.fetch(context.Background())
	if err != nil || state.Version != 7 {
		t.Fatalf("fetch = %+v, %v", state, err)
	}
	if err := client.reportConfig(context.Background(), 7, nil); err != nil || configStatus != "success" {
		t.Fatalf("report = %q, %v", configStatus, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestManifestMatchesInstallScript(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	script, err := os.ReadFile(filepath.Join(root, "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "boardless-backend.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(manifest) {
		t.Fatal("backend manifest is not valid JSON")
	}
	match := regexp.MustCompile(`"sha256": "([0-9a-f]{64})"`).FindSubmatch(manifest)
	sum := sha256.Sum256(script)
	if len(match) != 2 || string(match[1]) != hex.EncodeToString(sum[:]) {
		t.Fatalf("backend manifest install hash is stale")
	}
	if !strings.Contains(string(manifest), `"recognitionCode": "BOARDLESS_BACKEND_REPOSITORY_V1"`) {
		t.Fatal("BoardLess recognition code is missing from backend manifest")
	}
	if strings.Contains(string(readme), "<!-- boardless:backend:start -->") {
		t.Fatal("README must link to the standalone backend manifest instead of embedding it")
	}
	uninstall, err := os.ReadFile(filepath.Join(root, "scripts", "uninstall.sh"))
	if err != nil {
		t.Fatal(err)
	}
	uninstallMatch := regexp.MustCompile(`echo '([0-9a-f]{64})  /tmp/boardray-uninstall\.sh'`).FindSubmatch(readme)
	uninstallSum := sha256.Sum256(uninstall)
	if len(uninstallMatch) != 2 || string(uninstallMatch[1]) != hex.EncodeToString(uninstallSum[:]) {
		t.Fatal("README uninstall hash is stale")
	}
	for _, required := range []string{"apt-get install --no-install-recommends -y nginx", "nginx -t", "fallback_proxy_protocol = true", "--force-fallback", `.runtime.fallback_always_on = ((.boardless.preset // "") == "vless-tcp-xtls-vision")`, "systemctl enable --now nginx.service"} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("install script does not enforce Nginx fallback: missing %q", required)
		}
	}
}
