package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type vpsPanelClient struct {
	config *VPSPanelConfig
	client *http.Client
}

func (v *vpsPanelClient) register(ctx context.Context) error {
	if v.config.AgentToken != "" {
		return nil
	}
	var response struct {
		AgentID    int64  `json:"agent_id"`
		ServerID   int64  `json:"server_id"`
		AgentToken string `json:"agent_token"`
	}
	err := jsonRequest(ctx, v.client, http.MethodPost, strings.TrimRight(v.config.PanelURL, "/")+"/api/agent/register", "",
		map[string]any{"enrollment_token": v.config.EnrollmentToken, "agent_version": v.config.AnnouncedVersion, "existing_config": false},
		&response, http.StatusCreated)
	if err != nil {
		return err
	}
	if response.AgentID <= 0 || response.ServerID <= 0 || response.AgentToken == "" {
		return errors.New("vps-panel returned invalid registration data")
	}
	v.config.AgentID, v.config.ServerID, v.config.AgentToken = response.AgentID, response.ServerID, response.AgentToken
	v.config.EnrollmentToken = ""
	return nil
}

func (v *vpsPanelClient) fetch(ctx context.Context) (VPSDesiredState, error) {
	var value VPSDesiredState
	err := jsonRequest(ctx, v.client, http.MethodGet, strings.TrimRight(v.config.PanelURL, "/")+"/api/agent/config", v.config.AgentToken, nil, &value, http.StatusOK)
	if err == nil && value.Version <= 0 {
		err = errors.New("vps-panel returned invalid desired state version")
	}
	return value, err
}

func (v *vpsPanelClient) reportConfig(ctx context.Context, version int64, applyErr error) error {
	status, message := "success", ""
	if applyErr != nil {
		status, message = "failed", applyErr.Error()
		if len(message) > 500 {
			message = message[:500]
		}
	}
	return jsonRequest(ctx, v.client, http.MethodPost, strings.TrimRight(v.config.PanelURL, "/")+"/api/agent/config/result", v.config.AgentToken,
		map[string]any{"version": version, "status": status, "message": message}, nil, http.StatusNoContent)
}

func (v *vpsPanelClient) reportUpgradeUnsupported(ctx context.Context, version string) error {
	return jsonRequest(ctx, v.client, http.MethodPost, strings.TrimRight(v.config.PanelURL, "/")+"/api/agent/upgrade/result", v.config.AgentToken,
		map[string]any{"version": version, "status": "failed", "message": "automatic upgrade is not supported by BoardRay"}, nil, http.StatusNoContent)
}

func (v *vpsPanelClient) reportTraffic(ctx context.Context, counters map[string]Counter) error {
	clients := make([]map[string]any, 0)
	for id, value := range counters {
		if !strings.HasPrefix(id, "vp-client-") {
			continue
		}
		clientID, err := strconv.ParseInt(strings.TrimPrefix(id, "vp-client-"), 10, 64)
		if err != nil || clientID <= 0 {
			continue
		}
		clients = append(clients, map[string]any{"client_id": clientID, "uplink_bytes": value.Uplink, "downlink_bytes": value.Downlink})
	}
	if len(clients) == 0 {
		return nil
	}
	return jsonRequest(ctx, v.client, http.MethodPost, strings.TrimRight(v.config.PanelURL, "/")+"/api/agent/traffic", v.config.AgentToken,
		map[string]any{"clients": clients}, nil, http.StatusNoContent)
}

func vpsInbounds(state *VPSDesiredState) ([]InboundSpec, error) {
	if state == nil {
		return nil, nil
	}
	var validationErrors []error
	if state.Realm.Enabled || len(state.Realm.Relays) != 0 {
		validationErrors = append(validationErrors, errors.New("Realm desired state is unsupported"))
	}
	if !state.Xray.Enabled && len(state.Xray.Proxies) != 0 {
		validationErrors = append(validationErrors, errors.New("disabled Xray contains proxies"))
	}
	if !state.Xray.Enabled {
		return nil, errors.Join(validationErrors...)
	}
	values := make([]InboundSpec, 0, len(state.Xray.Proxies))
	for _, proxy := range state.Xray.Proxies {
		value, err := vpsInbound(proxy)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("proxy %d: %w", proxy.ID, err))
			continue
		}
		values = append(values, value)
	}
	return values, errors.Join(validationErrors...)
}

func vpsInbound(proxy VPSDesiredProxy) (InboundSpec, error) {
	if proxy.ID <= 0 || proxy.Protocol != "vless" || proxy.Transport != "tcp" || proxy.ServerFlow != "xtls-rprx-vision" || proxy.Port < 1 || proxy.Port > 65535 || proxy.Listen != "0.0.0.0" || proxy.ServerName == "" {
		return InboundSpec{}, errors.New("unsupported VLESS configuration")
	}
	value := InboundSpec{Source: "vps-panel", ID: strconv.FormatInt(proxy.ID, 10), Listen: proxy.Listen, Port: proxy.Port, Security: proxy.Security, ServerName: proxy.ServerName}
	switch proxy.Security {
	case "tls":
		if proxy.TLS == nil || proxy.Reality != nil || proxy.TLS.Mode != "acme" {
			return InboundSpec{}, errors.New("only ACME TLS is supported")
		}
	case "reality":
		if proxy.Reality == nil || proxy.TLS != nil || proxy.Reality.Target == "" || proxy.Reality.PrivateKey == "" || proxy.Reality.ShortID == "" {
			return InboundSpec{}, errors.New("invalid REALITY configuration")
		}
		if !validRealityTarget(proxy.Reality.Target) || !validShortID(proxy.Reality.ShortID) {
			return InboundSpec{}, errors.New("invalid REALITY target or short ID")
		}
		if _, err := realityPublicKey(proxy.Reality.PrivateKey); err != nil {
			return InboundSpec{}, errors.New("invalid REALITY private key")
		}
		value.RealityTarget, value.PrivateKey, value.ShortID = proxy.Reality.Target, proxy.Reality.PrivateKey, proxy.Reality.ShortID
	default:
		return InboundSpec{}, errors.New("security must be tls or reality")
	}
	for _, client := range proxy.Clients {
		if client.ID <= 0 || client.StatsID != "vp-client-"+strconv.FormatInt(client.ID, 10) || !validUUID(client.UUID) {
			return InboundSpec{}, errors.New("invalid client")
		}
		value.Clients = append(value.Clients, ClientSpec{UUID: client.UUID, Email: client.StatsID})
	}
	return value, nil
}
