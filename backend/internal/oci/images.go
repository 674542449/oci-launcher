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
	Version      string `json:"version"`      // e.g. "26.04" or "24.04"
	Architecture string `json:"architecture"` // aarch64 or x86_64
	TimeCreated  string `json:"time_created"`
}

var versionRegex = regexp.MustCompile(`(\d+\.\d+)`)

// GetTop2UbuntuImages dynamically discovers and returns the top 2 latest official Ubuntu LTS releases
func GetTop2UbuntuImages(ctx context.Context, profile *storage.OCIProfile, shape string, region string) ([]UbuntuImageInfo, error) {
	isARM := strings.Contains(shape, "A1.Flex")
	targetArch := "x86_64"
	if isARM {
		targetArch = "aarch64"
	}

	cacheKey := fmt.Sprintf("images:ubuntu:%s:%s:%s", profile.TenancyOCID, region, targetArch)

	// Check cache
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

	req := core.ListImagesRequest{
		CompartmentId:   common.String(profile.TenancyOCID),
		OperatingSystem: common.String("Canonical Ubuntu"),
		Shape:           common.String(shape),
		SortBy:          core.ListImagesSortByTimecreated,
		SortOrder:       core.ListImagesSortOrderDesc,
	}

	resp, err := computeClient.ListImages(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query Ubuntu images from OCI: %w", err)
	}

	// Filter and group by major version
	versionMap := make(map[string]UbuntuImageInfo) // version -> latest image

	for _, img := range resp.Items {
		name := StrVal(img.DisplayName)
		lowerName := strings.ToLower(name)

		// 1. Must be official Canonical Ubuntu standard release
		// Filter out Minimal, Daily, Beta, Preview, Testing
		if strings.Contains(lowerName, "minimal") ||
			strings.Contains(lowerName, "daily") ||
			strings.Contains(lowerName, "beta") ||
			strings.Contains(lowerName, "preview") ||
			strings.Contains(lowerName, "testing") {
			continue
		}

		// 2. Extract version (e.g. "26.04" or "24.04")
		matches := versionRegex.FindStringSubmatch(name)
		if len(matches) < 2 {
			continue
		}
		version := matches[1]

		// 3. Keep the latest build for each version
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

	// 4. Sort distinct versions descending (e.g. 26.04 > 24.04 > 22.04)
	var versions []string
	for v := range versionMap {
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i] > versions[j]
	})

	// 5. Pick the top 2 latest official releases
	var results []UbuntuImageInfo
	for i := 0; i < len(versions) && i < 2; i++ {
		results = append(results, versionMap[versions[i]])
	}

	// Fallback if none matched
	if len(results) == 0 && len(resp.Items) > 0 {
		for i := 0; i < len(resp.Items) && i < 2; i++ {
			img := resp.Items[i]
			results = append(results, UbuntuImageInfo{
				OCID:         StrVal(img.Id),
				DisplayName:  StrVal(img.DisplayName),
				Version:      "Latest",
				Architecture: targetArch,
			})
		}
	}

	// Cache in Redis for 6 hours
	if bytes, err := json.Marshal(results); err == nil {
		_ = cache.CacheMetadata(ctx, cacheKey, string(bytes), 6*time.Hour)
	}

	return results, nil
}
