package oci

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

// Compartment is one compartment of the tenancy; the root is included with Name "root".
type Compartment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const compartmentsCacheTTL = 5 * time.Minute

// ListCompartments returns the tenancy root followed by every ACTIVE compartment below it (all
// pages). Resources created from the OCI console often live in a sub-compartment, so every
// listing in this package walks this list instead of assuming the root. The result is cached
// briefly; when IAM cannot be read the root alone is returned.
func ListCompartments(ctx context.Context, profile *storage.OCIProfile) []Compartment {
	root := Compartment{ID: profile.TenancyOCID, Name: "root"}
	cacheKey := "compartments:" + profile.TenancyOCID
	if raw, err := cache.GetCachedMetadata(ctx, cacheKey); err == nil && raw != "" {
		var cached []Compartment
		if json.Unmarshal([]byte(raw), &cached) == nil && len(cached) > 0 {
			return cached
		}
	}

	idClient, err := GetIdentityClient(profile)
	if err != nil {
		return []Compartment{root}
	}
	req := identity.ListCompartmentsRequest{
		CompartmentId:          common.String(profile.TenancyOCID),
		CompartmentIdInSubtree: common.Bool(true),
		AccessLevel:            identity.ListCompartmentsAccessLevelAny,
		LifecycleState:         identity.CompartmentLifecycleStateActive,
		Limit:                  common.Int(100),
	}
	result := []Compartment{root}
	for {
		resp, err := idClient.ListCompartments(ctx, req)
		if err != nil {
			log.Printf("[OCI] list compartments for %s failed, using the root only: %v", profile.Name, err)
			return []Compartment{root}
		}
		for _, c := range resp.Items {
			if c.Id == nil {
				continue
			}
			result = append(result, Compartment{ID: *c.Id, Name: StrVal(c.Name)})
		}
		if resp.OpcNextPage == nil {
			break
		}
		req.Page = resp.OpcNextPage
	}

	if data, err := json.Marshal(result); err == nil {
		_ = cache.CacheMetadata(ctx, cacheKey, string(data), compartmentsCacheTTL)
	}
	return result
}

// skipUnreadableCompartment reports whether a listing error on a sub-compartment should be
// ignored (the API key has no rights there) rather than fail the whole listing. Errors on the
// root compartment are never skipped.
func skipUnreadableCompartment(comp Compartment, profile *storage.OCIProfile, err error) bool {
	if comp.ID == profile.TenancyOCID {
		return false
	}
	if se, ok := common.IsServiceError(err); ok {
		switch se.GetHTTPStatusCode() {
		case 401, 403, 404:
			log.Printf("[OCI] compartment %q not readable for %s (HTTP %d), skipped", comp.Name, profile.Name, se.GetHTTPStatusCode())
			return true
		}
	}
	return false
}
