package oci

// StrVal safely dereferences a *string, returning empty string if nil
func StrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
