package oci

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDNSLabelAndDefaultName(t *testing.T) {
	if got := DNSLabel("vcn", "20260906-1659"); got != "vcn202609061659" {
		t.Fatalf("DNSLabel = %q", got)
	}
	if got := DNSLabel("subnet", "20260906-1659"); len(got) != 15 || !strings.HasPrefix(got, "subnet") {
		t.Fatalf("subnet label = %q", got)
	}
	name := DefaultName("instance")
	if !strings.HasPrefix(name, "instance-") || len(name) != len("instance-20260906-1659") {
		t.Fatalf("DefaultName = %q", name)
	}
}

func TestCloudConfigHasNoToolFingerprint(t *testing.T) {
	for _, tc := range []struct {
		mode, key, pass string
		want, reject    []string
	}{
		{
			mode: "root_key", key: "ssh-ed25519 AAAA test@local",
			want:   []string{"#cloud-config", "disable_root: false", "runcmd:", "iptables"},
			reject: []string{"ssh_pwauth", "chpasswd", "oci-panel", "oci_init", "PermitRootLogin"},
		},
		{
			mode: "root_password", pass: `p4ss"word\x`,
			want:   []string{"ssh_pwauth: true", "chpasswd:", `password: "p4ss\"word\\x"`, "PermitRootLogin yes", "PasswordAuthentication yes", "systemctl restart ssh"},
			reject: []string{"oci-panel", "oci_init"},
		},
	} {
		raw, err := base64.StdEncoding.DecodeString(BuildCloudInitUserData(tc.mode, tc.key, tc.pass, true))
		if err != nil {
			t.Fatal(err)
		}
		doc := string(raw)
		if !strings.HasPrefix(doc, "#cloud-config\n") {
			t.Fatalf("%s: not a cloud-config document:\n%s", tc.mode, doc)
		}
		for _, w := range tc.want {
			if !strings.Contains(doc, w) {
				t.Fatalf("%s: missing %q in:\n%s", tc.mode, w, doc)
			}
		}
		for _, r := range tc.reject {
			if strings.Contains(doc, r) {
				t.Fatalf("%s: unexpected %q in:\n%s", tc.mode, r, doc)
			}
		}
		if !strings.Contains(doc, "net.ipv4.tcp_congestion_control=bbr") {
			t.Fatalf("%s: BBR line missing:\n%s", tc.mode, doc)
		}
	}
}
