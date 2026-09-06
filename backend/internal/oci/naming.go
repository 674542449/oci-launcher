package oci

import (
	"strings"
	"time"

	"oci-panel/internal/config"
)

// Names written to OCI follow the console's own defaults (instance-YYYYMMDD-HHMM,
// vcn-YYYYMMDD-HHMM, volume-YYYYMMDD-HHMM …) so resources created here are
// indistinguishable from ones created by hand. The stamp uses NAME_TIMEZONE (Asia/Tokyo).

func nameLocation() *time.Location {
	zone := "Asia/Tokyo"
	if config.GlobalConfig != nil && config.GlobalConfig.NameTimezone != "" {
		zone = config.GlobalConfig.NameTimezone
	}
	if loc, err := time.LoadLocation(zone); err == nil {
		return loc
	}
	// No tzdata in the container: Japan has no daylight saving, a fixed offset is exact.
	return time.FixedZone("JST", 9*3600)
}

// NameStamp returns YYYYMMDD-HHMM in the naming time zone.
func NameStamp() string {
	return time.Now().In(nameLocation()).Format("20060102-1504")
}

// DefaultName returns "<prefix>-YYYYMMDD-HHMM", e.g. instance-20260906-1659.
func DefaultName(prefix string) string {
	return prefix + "-" + NameStamp()
}

// DNSLabel derives the label the console proposes for a name: letters and digits only,
// at most 15 characters (vcn-20260906-1659 -> vcn202609061659).
func DNSLabel(prefix, stamp string) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, r := range stamp {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	label := strings.ToLower(b.String())
	if len(label) > 15 {
		label = label[:15]
	}
	return label
}
