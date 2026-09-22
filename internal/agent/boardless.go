package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type boardLessClient struct {
	config  *BoardLessConfig
	client  *http.Client
	version string
}

func (b *boardLessClient) fetch(ctx context.Context) (BoardLessSnapshot, error) {
	var value BoardLessSnapshot
	err := jsonRequest(ctx, b.client, http.MethodGet, strings.TrimRight(b.config.PanelURL, "/")+"/api/node/v1/config", b.config.NodeToken, nil, &value, http.StatusOK)
	return value, err
}

func (b *boardLessClient) heartbeat(ctx context.Context) error {
	return jsonRequest(ctx, b.client, http.MethodPost, strings.TrimRight(b.config.PanelURL, "/")+"/api/node/v1/heartbeat", b.config.NodeToken,
		map[string]any{"onlineCount": 0, "version": "boardray/" + b.version}, nil, http.StatusOK)
}

func (b *boardLessClient) upload(ctx context.Context, value PendingUsage) error {
	var response struct {
		Accepted  bool `json:"accepted"`
		Duplicate bool `json:"duplicate"`
	}
	if err := jsonRequest(ctx, b.client, http.MethodPost, strings.TrimRight(b.config.PanelURL, "/")+"/api/node/v1/usage", b.config.NodeToken,
		map[string]any{"reportId": value.ReportID, "entries": value.Entries}, &response, http.StatusOK); err != nil {
		return err
	}
	if !response.Accepted && !response.Duplicate {
		return errors.New("BoardLess rejected usage report")
	}
	return nil
}

func boardLessInbound(snapshot *BoardLessSnapshot, config *BoardLessConfig, now time.Time) (InboundSpec, bool, error) {
	if snapshot == nil || snapshot.Node.Status != "approved" {
		return InboundSpec{}, false, nil
	}
	node := snapshot.Node.Config
	if snapshot.Node.Protocol != "vless" || node.Port < 1 || node.Port > 65535 || node.Transport != "tcp" || node.Flow != "xtls-rprx-vision" || node.SNI == "" {
		return InboundSpec{}, false, errors.New("BoardLess returned an unsupported node configuration")
	}
	value := InboundSpec{Source: "boardless", ID: snapshot.Node.ID, Listen: "0.0.0.0", Port: node.Port, ServerName: node.SNI}
	switch config.Preset {
	case PresetTLS:
		if !node.TLS || node.RealityPublicKey != "" || config.Domain != node.SNI {
			return InboundSpec{}, false, errors.New("BoardLess TLS configuration does not match local ACME domain")
		}
		value.Security, value.ACMEEmail = "tls", config.ACMEEmail
	case PresetReality:
		if node.RealityPublicKey == "" || node.RealityPublicKey != config.RealityPublicKey || node.ShortID != config.RealityShortID {
			return InboundSpec{}, false, errors.New("BoardLess REALITY public values do not match local secrets")
		}
		value.Security, value.RealityTarget = "reality", config.RealityTarget
		value.PrivateKey, value.ShortID = config.RealityPrivateKey, config.RealityShortID
	default:
		return InboundSpec{}, false, errors.New("unsupported BoardLess preset")
	}
	for _, user := range snapshot.Users {
		if user.ID == "" || !validUUID(user.UUID) || (user.ExpiresAt != 0 && user.ExpiresAt <= now.Unix()) || (user.QuotaBytes != 0 && user.QuotaBytes <= user.UsedBytes) {
			continue
		}
		value.Clients = append(value.Clients, ClientSpec{UUID: user.UUID, Email: boardLessStatsID(user.ID)})
	}
	return value, true, nil
}

func boardLessFallbackInbound(config *BoardLessConfig) (InboundSpec, bool) {
	if config == nil || config.Preset != PresetTLS || config.Domain == "" || config.ACMEEmail == "" {
		return InboundSpec{}, false
	}
	return InboundSpec{
		Source: "boardless", ID: "fallback", Listen: "0.0.0.0", Port: 443,
		Security: "tls", ServerName: config.Domain, ACMEEmail: config.ACMEEmail,
	}, true
}

func boardLessStatsID(userID string) string {
	return "br-user-" + base64.RawURLEncoding.EncodeToString([]byte(userID))
}

func boardLessUserID(statsID string) (string, bool) {
	if !strings.HasPrefix(statsID, "br-user-") {
		return "", false
	}
	value, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(statsID, "br-user-"))
	return string(value), err == nil && len(value) > 0
}

func isUnauthorized(err error) bool {
	var value *statusError
	return errors.As(err, &value) && value.Status == http.StatusUnauthorized
}

func reportID(nodeID string, now time.Time) string {
	return fmt.Sprintf("%s-%d", nodeID, now.UnixNano())
}
