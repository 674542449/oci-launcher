package oci

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"oci-panel/internal/config"
	"oci-panel/internal/security"
	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/oracle/oci-go-sdk/v65/limits"
	"github.com/oracle/oci-go-sdk/v65/monitoring"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
)

var (
	// Persistent HTTP client with keep-alive pooling.
	// - Proxy from environment so HTTPS_PROXY deployments can reach OCI.
	// - 90s overall timeout: LaunchInstance under capacity contention regularly takes >45s.
	pooledHTTPClient = &http.Client{
		Timeout: 90 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 80 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			DisableCompression:    false,
		},
	}

	// RSA Private Key in-memory cache to avoid repeated PEM decoding
	rsaKeyCache sync.Map // profileID -> *rsa.PrivateKey
)

// DynamicConfigurationProvider implements common.ConfigurationProvider in memory
type DynamicConfigurationProvider struct {
	tenancyID   string
	userID      string
	region      string
	fingerprint string
	privateKey  *rsa.PrivateKey
}

func (p DynamicConfigurationProvider) TenancyOCID() (string, error) {
	return p.tenancyID, nil
}

func (p DynamicConfigurationProvider) UserOCID() (string, error) {
	return p.userID, nil
}

func (p DynamicConfigurationProvider) KeyFingerprint() (string, error) {
	return p.fingerprint, nil
}

func (p DynamicConfigurationProvider) Region() (string, error) {
	return p.region, nil
}

func (p DynamicConfigurationProvider) KeyID() (string, error) {
	return fmt.Sprintf("%s/%s/%s", p.tenancyID, p.userID, p.fingerprint), nil
}

func (p DynamicConfigurationProvider) PrivateRSAKey() (*rsa.PrivateKey, error) {
	return p.privateKey, nil
}

func (p DynamicConfigurationProvider) AuthType() (common.AuthConfig, error) {
	return common.AuthConfig{
		AuthType: common.UserPrincipal,
	}, nil
}

// ParseRSAPrivateKeyPEM parses a PKCS#1 or PKCS#8 RSA private key. Encrypted keys are rejected
// with an explicit message so the user knows to remove the passphrase.
func ParseRSAPrivateKeyPEM(pemStr string) (*rsa.PrivateKey, error) {
	pemBlock, _ := pem.Decode([]byte(pemStr))
	if pemBlock == nil {
		return nil, errors.New("invalid PEM private key format")
	}
	if pemBlock.Type == "ENCRYPTED PRIVATE KEY" || pemBlock.Headers["Proc-Type"] == "4,ENCRYPTED" {
		return nil, errors.New("private key is passphrase-protected; export an unencrypted key (openssl rsa -in key.pem -out key-nopass.pem)")
	}

	if key, err := x509.ParsePKCS1PrivateKey(pemBlock.Bytes); err == nil {
		return key, nil
	}
	pkcs8Key, err := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RSA private key (PKCS#1/PKCS#8): %w", err)
	}
	rsaKey, ok := pkcs8Key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("key is not an RSA private key")
	}
	return rsaKey, nil
}

// keyCacheKey ties the cached RSA key to the fingerprint it was imported with, so a rotated key
// (re-import) can never be paired with the old fingerprint held by a running worker.
func keyCacheKey(profile *storage.OCIProfile) string {
	return fmt.Sprintf("%d:%s", profile.ID, profile.Fingerprint)
}

// GetConfigurationProvider creates or retrieves cached ConfigurationProvider for a profile
func GetConfigurationProvider(profile *storage.OCIProfile) (common.ConfigurationProvider, error) {
	var rsaKey *rsa.PrivateKey
	cacheKey := keyCacheKey(profile)
	if cached, ok := rsaKeyCache.Load(cacheKey); ok {
		rsaKey = cached.(*rsa.PrivateKey)
	} else {
		pemStr, err := security.DecryptAES256GCM(profile.PrivateKeyEnc, config.GlobalConfig.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %w", err)
		}
		rsaKey, err = ParseRSAPrivateKeyPEM(pemStr)
		if err != nil {
			return nil, err
		}
		rsaKeyCache.Store(cacheKey, rsaKey)
	}

	provider := DynamicConfigurationProvider{
		tenancyID:   profile.TenancyOCID,
		userID:      profile.UserOCID,
		region:      profile.Region,
		fingerprint: profile.Fingerprint,
		privateKey:  rsaKey,
	}

	return provider, nil
}

// InvalidateProfileCache clears cached keys when a profile is updated or deleted
func InvalidateProfileCache(profileID uint) {
	prefix := fmt.Sprintf("%d:", profileID)
	rsaKeyCache.Range(func(key, _ interface{}) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			rsaKeyCache.Delete(key)
		}
		return true
	})
}

func firstRegion(regionOverride []string) string {
	if len(regionOverride) > 0 {
		return regionOverride[0]
	}
	return ""
}

func GetComputeClient(profile *storage.OCIProfile, regionOverride ...string) (core.ComputeClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return core.ComputeClient{}, err
	}
	client, err := core.NewComputeClientWithConfigurationProvider(provider)
	if err != nil {
		return core.ComputeClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

func GetVirtualNetworkClient(profile *storage.OCIProfile, regionOverride ...string) (core.VirtualNetworkClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return core.VirtualNetworkClient{}, err
	}
	client, err := core.NewVirtualNetworkClientWithConfigurationProvider(provider)
	if err != nil {
		return core.VirtualNetworkClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

func GetBlockstorageClient(profile *storage.OCIProfile, regionOverride ...string) (core.BlockstorageClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return core.BlockstorageClient{}, err
	}
	client, err := core.NewBlockstorageClientWithConfigurationProvider(provider)
	if err != nil {
		return core.BlockstorageClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

// GetIdentityClient returns an identity client. Availability-domain names are region specific,
// so callers that need ADs for a region other than the profile's must pass that region.
func GetIdentityClient(profile *storage.OCIProfile, regionOverride ...string) (identity.IdentityClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return identity.IdentityClient{}, err
	}
	client, err := identity.NewIdentityClientWithConfigurationProvider(provider)
	if err != nil {
		return identity.IdentityClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

// GetLimitsClient returns a limits client. Service limits are per region: query the home region.
func GetLimitsClient(profile *storage.OCIProfile, regionOverride ...string) (limits.LimitsClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return limits.LimitsClient{}, err
	}
	client, err := limits.NewLimitsClientWithConfigurationProvider(provider)
	if err != nil {
		return limits.LimitsClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

func GetObjectStorageClient(profile *storage.OCIProfile, regionOverride ...string) (objectstorage.ObjectStorageClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return objectstorage.ObjectStorageClient{}, err
	}
	client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(provider)
	if err != nil {
		return objectstorage.ObjectStorageClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

func GetMonitoringClient(profile *storage.OCIProfile, regionOverride ...string) (monitoring.MonitoringClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return monitoring.MonitoringClient{}, err
	}
	client, err := monitoring.NewMonitoringClientWithConfigurationProvider(provider)
	if err != nil {
		return monitoring.MonitoringClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

// ListAvailabilityDomainNames returns the AD names of the tenancy in the given region.
func ListAvailabilityDomainNames(ctx context.Context, profile *storage.OCIProfile, region string) ([]string, error) {
	idClient, err := GetIdentityClient(profile, region)
	if err != nil {
		return nil, err
	}
	resp, err := idClient.ListAvailabilityDomains(ctx, identity.ListAvailabilityDomainsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
	})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ad := range resp.Items {
		if ad.Name != nil {
			names = append(names, *ad.Name)
		}
	}
	return names, nil
}
