package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type runtimeAgent struct {
	configPath string
	config     Config
	state      persistentState
	version    string
	http       *http.Client
	boardless  *boardLessClient
	vps        *vpsPanelClient
	xray       *xrayManager
	mu         sync.Mutex
}

func Run(ctx context.Context, configPath, version string) error {
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("secure config: %w", err)
	}
	state, err := loadState(config.Runtime.StatePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	value := &runtimeAgent{
		configPath: configPath, config: config, state: state, version: version,
		http: &http.Client{Timeout: 15 * time.Second}, xray: newXrayManager(config.Runtime),
	}
	if config.BoardLess != nil && config.Mode != "vps-panel" {
		value.boardless = &boardLessClient{config: config.BoardLess, client: value.http, version: version}
	}
	if config.VPSPanel != nil && config.Mode != "boardless" {
		value.vps = &vpsPanelClient{config: config.VPSPanel, client: value.http}
		registerContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := value.vps.register(registerContext)
		cancel()
		if err != nil {
			return fmt.Errorf("register with vps-panel: %w", err)
		}
		if err := saveConfig(configPath, value.config); err != nil {
			return fmt.Errorf("save vps-panel credentials: %w", err)
		}
	}
	fallbackErr := make(chan error, 1)
	if !config.Runtime.FallbackProxyProtocol {
		go func() { fallbackErr <- serveFallback(ctx, config.Runtime.FallbackAddress) }()
	}
	configChanged := make(chan struct{}, 1)
	if value.vps != nil {
		go runVPSWebSocket(ctx, value.vps, configChanged)
	}
	if err := value.sync(ctx); err != nil {
		log.Printf("initial synchronization failed: %v", err)
	}
	if err := value.traffic(ctx); err != nil {
		log.Printf("initial traffic report failed: %v", err)
	}
	syncTicker := time.NewTicker(30 * time.Second)
	trafficTicker := time.NewTicker(15 * time.Second)
	renewTicker := time.NewTicker(12 * time.Hour)
	defer syncTicker.Stop()
	defer trafficTicker.Stop()
	defer renewTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-fallbackErr:
			return fmt.Errorf("fallback server: %w", err)
		case <-configChanged:
			if err := value.sync(ctx); err != nil {
				log.Printf("configuration synchronization failed: %v", err)
			}
		case <-syncTicker.C:
			if err := value.sync(ctx); err != nil {
				log.Printf("configuration synchronization failed: %v", err)
			}
		case <-renewTicker.C:
			if err := value.sync(ctx); err != nil {
				log.Printf("certificate renewal check failed: %v", err)
			}
		case <-trafficTicker.C:
			if err := value.traffic(ctx); err != nil {
				log.Printf("traffic report failed: %v", err)
			}
		}
	}
}

func (a *runtimeAgent) sync(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now().UTC()
	var operationErrors []error
	if a.boardless != nil {
		requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		snapshot, err := a.boardless.fetch(requestContext)
		cancel()
		if err == nil {
			a.state.BoardLessSnapshot, a.state.BoardLessFetched = &snapshot, now
		} else if isUnauthorized(err) {
			a.state.BoardLessSnapshot = nil
			a.state.BoardLessFetched = time.Time{}
			operationErrors = append(operationErrors, errors.New("BoardLess authentication rejected; access revoked"))
		} else {
			operationErrors = append(operationErrors, fmt.Errorf("fetch BoardLess snapshot: %w", err))
			grace := time.Duration(a.config.Runtime.StaleGraceSeconds) * time.Second
			if a.state.BoardLessFetched.IsZero() || now.Sub(a.state.BoardLessFetched) > grace {
				a.state.BoardLessSnapshot = nil
			}
		}
		heartbeatContext, cancelHeartbeat := context.WithTimeout(ctx, 10*time.Second)
		if err := a.boardless.heartbeat(heartbeatContext); err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("BoardLess heartbeat: %w", err))
		}
		cancelHeartbeat()
	}
	if a.vps != nil {
		requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		desired, err := a.vps.fetch(requestContext)
		cancel()
		if err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("fetch vps-panel desired state: %w", err))
		} else {
			a.state.VPSDesired = &desired
		}
	}
	var boardlessSpec *InboundSpec
	if a.boardless != nil {
		value, enabled, err := boardLessInbound(a.state.BoardLessSnapshot, a.config.BoardLess, now)
		if err != nil {
			operationErrors = append(operationErrors, err)
		} else if enabled {
			boardlessSpec = &value
		}
		if boardlessSpec == nil && a.config.Runtime.FallbackAlwaysOn {
			if value, enabled := boardLessFallbackInbound(a.config.BoardLess); enabled {
				boardlessSpec = &value
			}
		}
	}
	vpsSpecs, vpsValidationErr := vpsInbounds(a.state.VPSDesired)
	specs, compatibilityErr := mergeInbounds(boardlessSpec, vpsSpecs, vpsValidationErr)
	// Capture BoardLess counters before a configuration change can restart Xray.
	if a.boardless != nil && a.state.BoardLessSnapshot != nil {
		statsContext, cancelStats := context.WithTimeout(ctx, 10*time.Second)
		counters, err := collectStats(statsContext, a.config.Runtime, a.xray.run)
		cancelStats()
		if err == nil {
			boardlessCounters := countersWithPrefix(counters, "br-user-")
			updateBoardLessUsage(&a.state, a.state.BoardLessSnapshot, counterDeltas(&a.state, boardlessCounters), now)
		}
	}
	applyContext, cancelApply := context.WithTimeout(ctx, 8*time.Minute)
	hash, applyErr := a.xray.apply(applyContext, specs)
	cancelApply()
	if applyErr == nil {
		if hash != a.state.AppliedHash {
			// Xray restarts when its effective config changes, so its cumulative counters restart too.
			a.state.Counters = map[string]Counter{}
		}
		a.state.AppliedHash = hash
	}
	vpsResultErr := errors.Join(compatibilityErr, applyErr)
	if a.vps != nil && a.state.VPSDesired != nil && a.state.VPSDesired.Version != a.state.VPSReportedVersion {
		reportContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := a.vps.reportConfig(reportContext, a.state.VPSDesired.Version, vpsResultErr)
		cancel()
		if err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("report vps-panel config result: %w", err))
		} else if vpsResultErr == nil {
			a.state.VPSReportedVersion = a.state.VPSDesired.Version
		}
	}
	if applyErr != nil {
		operationErrors = append(operationErrors, applyErr)
	}
	if compatibilityErr != nil {
		operationErrors = append(operationErrors, compatibilityErr)
	}
	if err := saveState(a.config.Runtime.StatePath, a.state); err != nil {
		operationErrors = append(operationErrors, fmt.Errorf("save state: %w", err))
	}
	return errors.Join(operationErrors...)
}

func (a *runtimeAgent) traffic(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	counters, err := collectStats(requestContext, a.config.Runtime, a.xray.run)
	cancel()
	if err != nil {
		return err
	}
	deltas := counterDeltas(&a.state, counters)
	if a.boardless != nil {
		updateBoardLessUsage(&a.state, a.state.BoardLessSnapshot, deltas, time.Now().UTC())
	}
	if err := saveState(a.config.Runtime.StatePath, a.state); err != nil {
		return err
	}
	if a.boardless != nil {
		for len(a.state.Pending) > 0 {
			uploadContext, cancelUpload := context.WithTimeout(ctx, 15*time.Second)
			err := a.boardless.upload(uploadContext, a.state.Pending[0])
			cancelUpload()
			if err != nil {
				return err
			}
			a.state.Pending = a.state.Pending[1:]
			if err := saveState(a.config.Runtime.StatePath, a.state); err != nil {
				return err
			}
		}
	}
	if a.vps != nil {
		uploadContext, cancelUpload := context.WithTimeout(ctx, 15*time.Second)
		err := a.vps.reportTraffic(uploadContext, deltas)
		cancelUpload()
		if err != nil {
			rewindCounterDeltas(&a.state, counters, deltas, "vp-client-")
			return errors.Join(err, saveState(a.config.Runtime.StatePath, a.state))
		}
	}
	return nil
}
