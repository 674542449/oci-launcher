package oci

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
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
	// Persistent HTTP client with Keep-Alive connection pooling
	pooledHTTPClient = &http.Client{
		Timeout: 45 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
	}

	// RSA Private Key in-memory cache to avoid repeated PEM decoding
	rsaKeyCache sync.Map // profileID -> *rsa.PrivateKey

	// Client singletons cache
	clientCache sync.Map // cacheKey -> interface{}
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

// GetConfigurationProvider creates or retrieves cached ConfigurationProvider for a profile
func GetConfigurationProvider(profile *storage.OCIProfile) (common.ConfigurationProvider, error) {
	// Check cached RSA key
	var rsaKey *rsa.PrivateKey
	if cached, ok := rsaKeyCache.Load(profile.ID); ok {
		rsaKey = cached.(*rsa.PrivateKey)
	} else {
		// Decrypt private key
		pemStr, err := security.DecryptAES256GCM(profile.PrivateKeyEnc, config.GlobalConfig.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %w", err)
		}

		pemBlock, _ := pem.Decode([]byte(pemStr))
		if pemBlock == nil {
			return nil, errors.New("invalid PEM private key format")
		}

		parsedKey, err := x509.ParsePKCS1PrivateKey(pemBlock.Bytes)
		if err != nil {
			// Try PKCS8
			pkcs8Key, err2 := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
			if err2 != nil {
				return nil, fmt.Errorf("failed to parse RSA private key: %w", err)
			}
			var ok bool
			rsaKey, ok = pkcs8Key.(*rsa.PrivateKey)
			if !ok {
				return nil, errors.New("key is not RSA private key")
			}
		} else {
			rsaKey = parsedKey
		}

		rsaKeyCache.Store(profile.ID, rsaKey)
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

// InvalidateProfileCache clears cached clients and keys when a profile is updated
func InvalidateProfileCache(profileID uint) {
	rsaKeyCache.Delete(profileID)
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

	if len(regionOverride) > 0 && regionOverride[0] != "" {
		client.SetRegion(regionOverride[0])
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

	if len(regionOverride) > 0 && regionOverride[0] != "" {
		client.SetRegion(regionOverride[0])
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

	if len(regionOverride) > 0 && regionOverride[0] != "" {
		client.SetRegion(regionOverride[0])
	}

	return client, nil
}

func GetIdentityClient(profile *storage.OCIProfile) (identity.IdentityClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return identity.IdentityClient{}, err
	}

	client, err := identity.NewIdentityClientWithConfigurationProvider(provider)
	if err != nil {
		return identity.IdentityClient{}, err
	}
	client.HTTPClient = pooledHTTPClient

	return client, nil
}

func GetLimitsClient(profile *storage.OCIProfile) (limits.LimitsClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return limits.LimitsClient{}, err
	}

	client, err := limits.NewLimitsClientWithConfigurationProvider(provider)
	if err != nil {
		return limits.LimitsClient{}, err
	}
	client.HTTPClient = pooledHTTPClient

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

	if len(regionOverride) > 0 && regionOverride[0] != "" {
		client.SetRegion(regionOverride[0])
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

	if len(regionOverride) > 0 && regionOverride[0] != "" {
		client.SetRegion(regionOverride[0])
	}

	return client, nil
}
