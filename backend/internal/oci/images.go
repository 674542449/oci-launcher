package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

type UbuntuImageInfo struct {
	OCID         string `json:"ocid"`
	DisplayName  string `json:"display_name"`
	Version      string `json:"version"`      // e.g. "24.04" or "22.04"
	Architecture string `json:"architecture"` // aarch64 or x86_64
	TimeCreated  string `json:"time_created"`
}

var versionRegex = regexp.MustCompile(`(\d+\.\d+)`)

// GetTop2UbuntuImages returns the newest build of the two most recent official Ubuntu LTS
// releases that are compatible with the given shape (the Shape filter takes care of
// aarch64 vs x86_64).
func GetTop2UbuntuImages(ctx context.Context, profile *storage.OCIProfile, shape string, region string) ([]UbuntuImageInfo, error) {
	isARM := strings.Contains(shape, "A1.Flex")
	targetArch := "x86_64"
	if isARM {
		targetArch = "aarch64"
	}

	cacheKey := fmt.Sprintf("images:ubuntu:%s:%s:%s", profile.TenancyOCID, region, targetArch)

	if cachedStr, err := cache.GetCachedMetadata(ctx, cacheKey); err == nil && cachedStr != "" {
		var cachedImages []UbuntuImageInfo
		if json.Unmarshal([]byte(cachedStr), &cachedImages) == nil && len(cachedImages) > 0 {
			return cachedImages, nil
		}
	}

	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return nil, err
	}

	// Platform images are listed from the root compartment; only AVAILABLE images can be launched.
	req := core.ListImagesRequest{
		CompartmentId:   common.String(profile.TenancyOCID),
		OperatingSystem: common.String("Canonical Ubuntu"),
		Shape:           common.String(shape),
		LifecycleState:  core.ImageLifecycleStateAvailable,
		SortBy:          core.ListImagesSortByTimecreated,
		SortOrder:       core.ListImagesSortOrderDesc,
		Limit:           common.Int(100),
	}

	var all []core.Image
	for {
		resp, err := computeClient.ListImages(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to query Ubuntu images from OCI: %w", err)
		}
		all = append(all, resp.Items...)
		if resp.OpcNextPage == nil || len(all) > 1000 {
			break
		}
		req.Page = resp.OpcNextPage
	}

	// Newest build per release version; skip Minimal / daily / pre-release builds
	// and anything that is not an Oracle-provided platform image.
	versionMap := make(map[string]UbuntuImageInfo)
	for _, img := range all {
		name := StrVal(img.DisplayName)
		lowerName := strings.ToLower(name)
		if !strings.HasPrefix(lowerName, "canonical-ubuntu-") ||
			strings.Contains(lowerName, "minimal") ||
			strings.Contains(lowerName, "daily") ||
			strings.Contains(lowerName, "beta") ||
			strings.Contains(lowerName, "preview") ||
			strings.Contains(lowerName, "testing") {
			continue
		}
		matches := versionRegex.FindStringSubmatch(name)
		if len(matches) < 2 {
			continue
		}
		version := matches[1]
		if _, exists := versionMap[version]; !exists {
			createdStr := ""
			if img.TimeCreated != nil {
				createdStr = img.TimeCreated.Format(time.RFC3339)
			}
			versionMap[version] = UbuntuImageInfo{
				OCID:         StrVal(img.Id),
				DisplayName:  name,
				Version:      version,
				Architecture: targetArch,
				TimeCreated:  createdStr,
			}
		}
	}

	var versions []string
	for v := range versionMap {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] > versions[j] })

	var results []UbuntuImageInfo
	for i := 0; i < len(versions) && i < 2; i++ {
		results = append(results, versionMap[versions[i]])
	}

	// Fallback: newest images of any name if the naming convention changed
	if len(results) == 0 {
		for i := 0; i < len(all) && i < 2; i++ {
			img := all[i]
			results = append(results, UbuntuImageInfo{
				OCID:         StrVal(img.Id),
				DisplayName:  StrVal(img.DisplayName),
				Version:      "Latest",
				Architecture: targetArch,
			})
		}
	}

	if len(results) > 0 {
		if bytes, err := json.Marshal(results); err == nil {
			_ = cache.CacheMetadata(ctx, cacheKey, string(bytes), 6*time.Hour)
		}
	}

	return results, nil
}
