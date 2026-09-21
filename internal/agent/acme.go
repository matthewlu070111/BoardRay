package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ensureCertificate(ctx context.Context, runtime RuntimeConfig, domain, email string, run func(context.Context, string, ...string) ([]byte, error)) (string, string, bool, error) {
	if !validDomain(domain) {
		return "", "", false, fmt.Errorf("invalid ACME domain %q", domain)
	}
	directory := filepath.Join(runtime.CertDir, domain)
	certificate := filepath.Join(directory, "fullchain.pem")
	privateKey := filepath.Join(directory, "private.key")
	valid, err := certificateValid(certificate, privateKey, domain, time.Now(), 30*24*time.Hour)
	if err != nil {
		return "", "", false, err
	}
	if valid {
		return certificate, privateKey, false, nil
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", false, err
	}
	if _, err := os.Stat(runtime.ACMEScript); err != nil {
		return "", "", false, fmt.Errorf("ACME script unavailable: %w", err)
	}
	base := []string{"--home", runtime.ACMEHome, "--config-home", runtime.ACMEHome, "--cert-home", filepath.Join(runtime.ACMEHome, "certs")}
	var issue []string
	if _, err := os.Stat(certificate); err == nil {
		issue = append(append([]string{}, base...), "--renew", "-d", domain, "--ecc", "--force", "--server", "letsencrypt")
	} else {
		issue = append(append([]string{}, base...), "--issue", "--standalone", "-d", domain, "--keylength", "ec-256", "--force", "--server", "letsencrypt")
	}
	if email != "" {
		issue = append(issue, "--accountemail", email)
	}
	if output, err := runACMEWithNginxPaused(ctx, run, "sh", append([]string{runtime.ACMEScript}, issue...)...); err != nil {
		return "", "", false, fmt.Errorf("ACME issuance failed; verify DNS and TCP/80: %w: %s", err, truncate(output))
	}
	staging := filepath.Join(directory, ".staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return "", "", false, err
	}
	stagedCertificate, stagedKey := filepath.Join(staging, "fullchain.pem"), filepath.Join(staging, "private.key")
	install := append(append([]string{}, base...), "--install-cert", "-d", domain, "--ecc", "--fullchain-file", stagedCertificate, "--key-file", stagedKey)
	if output, err := run(ctx, "sh", append([]string{runtime.ACMEScript}, install...)...); err != nil {
		return "", "", false, fmt.Errorf("install ACME certificate: %w: %s", err, truncate(output))
	}
	valid, err = certificateValid(stagedCertificate, stagedKey, domain, time.Now(), time.Hour)
	if err != nil || !valid {
		return "", "", false, errors.New("ACME returned an invalid certificate")
	}
	certData, err := os.ReadFile(stagedCertificate)
	if err != nil {
		return "", "", false, err
	}
	keyData, err := os.ReadFile(stagedKey)
	if err != nil {
		return "", "", false, err
	}
	if err := atomicWrite(certificate, certData, 0o644); err != nil {
		return "", "", false, err
	}
	if err := atomicWrite(privateKey, keyData, 0o600); err != nil {
		return "", "", false, err
	}
	return certificate, privateKey, true, nil
}

func runACMEWithNginxPaused(ctx context.Context, run func(context.Context, string, ...string) ([]byte, error), name string, args ...string) ([]byte, error) {
	if _, err := run(ctx, "systemctl", "is-active", "--quiet", "nginx.service"); err != nil {
		return run(ctx, name, args...)
	}
	if output, err := run(ctx, "systemctl", "stop", "nginx.service"); err != nil {
		return output, fmt.Errorf("stop nginx for ACME HTTP-01: %w", err)
	}
	output, issueErr := run(ctx, name, args...)
	restoreContext, cancelRestore := context.WithTimeout(context.Background(), 30*time.Second)
	restartOutput, restartErr := run(restoreContext, "systemctl", "start", "nginx.service")
	cancelRestore()
	if restartErr != nil {
		output = append(output, restartOutput...)
		return output, errors.Join(issueErr, fmt.Errorf("restore nginx after ACME HTTP-01: %w", restartErr))
	}
	return output, issueErr
}

func certificateValid(certPath, keyPath, domain string, now time.Time, renewBefore time.Duration) (bool, error) {
	certificate, err := os.ReadFile(certPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	privateKey, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	pair, err := tls.X509KeyPair(certificate, privateKey)
	if err != nil || len(pair.Certificate) == 0 {
		return false, nil
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.VerifyHostname(domain) != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) || leaf.NotAfter.Sub(now) <= renewBefore {
		return false, nil
	}
	return true, nil
}

func validDomain(domain string) bool {
	if len(domain) < 4 || len(domain) > 253 || domain != strings.ToLower(domain) || net.ParseIP(domain) != nil || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return false
			}
		}
	}
	return true
}
