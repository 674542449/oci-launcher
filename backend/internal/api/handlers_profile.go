package api

import (
	"bufio"
	"crypto/md5"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"oci-panel/internal/config"
	"oci-panel/internal/engine"
	"oci-panel/internal/oci"
	"oci-panel/internal/security"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

type ImportRawProfileRequest struct {
	RawConfig     string `json:"raw_config" binding:"required"` // The raw INI text from Oracle console
	PrivateKeyPEM string `json:"private_key_pem"`               // Manual paste or uploaded content
	KeyFilePath   string `json:"key_file_path"`                 // Or existing path on server
	Tags          string `json:"tags"`                          // e.g. "Main,Tokyo,PAYG"
	Notes         string `json:"notes"`                         // e.g. "Registered 2026-05, Visa ending 1234"
}

type UpdateProfileRequest struct {
	ID                  uint   `json:"id" binding:"required"`
	AccountTypeOverride string `json:"account_type_override"` // auto, free, payg
	Tags                string `json:"tags"`
	Notes               string `json:"notes"`
}

// iniProfile is one [section] of an OCI CLI style config
type iniProfile struct {
	Name        string
	Tenancy     string
	User        string
	Fingerprint string
	Region      string
	KeyFile     string
}

var (
	sectionRegex     = regexp.MustCompile(`^\s*\[(.*?)\]\s*$`)
	ocidRegex        = regexp.MustCompile(`^ocid1\.[a-z0-9]+\.[a-z0-9]+(\.[a-z0-9-]*)?\.[a-z0-9]+$`)
	fingerprintRegex = regexp.MustCompile(`^([0-9a-fA-F]{2}:){15}[0-9a-fA-F]{2}$`)
	regionRegex      = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]$`)
)

// parseOCIConfig parses every [section] of an OCI config block. Values are reset per section so
// a pasted multi-profile ~/.oci/config does not bleed keys across sections.
func parseOCIConfig(raw string) []iniProfile {
	var out []iniProfile
	cur := iniProfile{Name: "DEFAULT"}
	started := false
	flush := func() {
		if started && (cur.Tenancy != "" || cur.User != "" || cur.Fingerprint != "" || cur.Region != "") {
			out = append(out, cur)
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if m := sectionRegex.FindStringSubmatch(line); len(m) > 1 {
			flush()
			cur = iniProfile{Name: strings.TrimSpace(m[1])}
			started = true
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		started = true
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.TrimSpace(parts[1])
		if idx := strings.Index(v, " #"); idx != -1 {
			v = strings.TrimSpace(v[:idx])
		}
		switch k {
		case "tenancy":
			cur.Tenancy = v
		case "user":
			cur.User = v
		case "fingerprint":
			cur.Fingerprint = strings.ToLower(v)
		case "region":
			cur.Region = strings.ToLower(v)
		case "key_file":
			cur.KeyFile = v
		}
	}
	flush()
	return out
}

// allowedKeyDirs are the only host directories a key_file path may point into.
func allowedKeyDirs() []string {
	dirs := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".oci"))
	}
	dirs = append(dirs, "/root/.oci")
	if config.GlobalConfig != nil && config.GlobalConfig.DataDir != "" {
		if abs, err := filepath.Abs(config.GlobalConfig.DataDir); err == nil {
			dirs = append(dirs, abs)
		}
	}
	return dirs
}

// readKeyFile reads a private key from the host, confined to the allowed directories.
func readKeyFile(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory")
		}
		path = filepath.Join(home, path[1:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	allowed := false
	for _, dir := range allowedKeyDirs() {
		if rel, err := filepath.Rel(dir, abs); err == nil && !strings.HasPrefix(rel, "..") {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("私钥路径必须位于 ~/.oci 或数据目录内")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("无法读取私钥文件: %v", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return "", fmt.Errorf("私钥文件无效（不是普通文件或超过 64 KB）")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("无法读取私钥文件: %v", err)
	}
	return string(data), nil
}

// keyFingerprint computes the OCI API key fingerprint (MD5 of the PKIX public key DER).
func keyFingerprint(pemContent string) (string, error) {
	rsaKey, err := oci.ParseRSAPrivateKeyPEM(pemContent)
	if err != nil {
		return "", err
	}
	pubDer, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(pubDer)
	parts := make([]string, 0, len(sum))
	for _, b := range sum {
		parts = append(parts, fmt.Sprintf("%02x", b))
	}
	return strings.Join(parts, ":"), nil
}

// uniqueProfileName returns name, or name-2 / name-3 ... when the name belongs to another tenancy/user.
func uniqueProfileName(name, tenancy, user string) (string, bool) {
	candidate := name
	for i := 2; i < 100; i++ {
		var existing storage.OCIProfile
		err := storage.DB.Where("name = ?", candidate).First(&existing).Error
		if err != nil {
			return candidate, candidate != name // free name
		}
		if existing.TenancyOCID == tenancy && existing.UserOCID == user {
			return candidate, candidate != name // same account: update in place
		}
		candidate = fmt.Sprintf("%s-%d", name, i)
	}
	return candidate, true
}

// upsertProfile validates one parsed section + key and stores it encrypted.
func upsertProfile(p iniProfile, pemContent, tags, notes string) (*storage.OCIProfile, string, error) {
	if p.Tenancy == "" || p.User == "" || p.Fingerprint == "" || p.Region == "" {
		return nil, "", fmt.Errorf("配置块 [%s] 缺少 tenancy / user / fingerprint / region 之一", p.Name)
	}
	if !ocidRegex.MatchString(p.Tenancy) || !strings.HasPrefix(p.Tenancy, "ocid1.tenancy.") {
		return nil, "", fmt.Errorf("tenancy OCID 格式无效: %s", p.Tenancy)
	}
	if !ocidRegex.MatchString(p.User) || !strings.HasPrefix(p.User, "ocid1.user.") {
		return nil, "", fmt.Errorf("user OCID 格式无效: %s", p.User)
	}
	if !fingerprintRegex.MatchString(p.Fingerprint) {
		return nil, "", fmt.Errorf("fingerprint 格式无效: %s", p.Fingerprint)
	}
	if !regionRegex.MatchString(p.Region) {
		return nil, "", fmt.Errorf("region 格式无效: %s（应为类似 ap-tokyo-1 的区域名）", p.Region)
	}

	pemContent = strings.TrimSpace(pemContent)
	if pemContent == "" {
		return nil, "", fmt.Errorf("请提供私钥内容（粘贴 PEM、选择文件或填写宿主机路径）")
	}
	calc, err := keyFingerprint(pemContent)
	if err != nil {
		return nil, "", fmt.Errorf("私钥无法解析: %v", err)
	}
	if !strings.EqualFold(calc, p.Fingerprint) {
		return nil, "", fmt.Errorf("私钥与配置块中的 fingerprint 不匹配（私钥指纹 %s，配置 %s）。请确认粘贴的是同一个 API 密钥", calc, p.Fingerprint)
	}

	keyEnc, err := security.EncryptAES256GCM(pemContent, config.GlobalConfig.MasterKey)
	if err != nil {
		return nil, "", fmt.Errorf("加密私钥失败: %v", err)
	}

	name, renamed := uniqueProfileName(p.Name, p.Tenancy, p.User)
	note := ""
	if renamed {
		note = fmt.Sprintf("名称 [%s] 已被另一个账号使用，已保存为 [%s]", p.Name, name)
	}

	var profile storage.OCIProfile
	if err := storage.DB.Where("name = ?", name).First(&profile).Error; err == nil {
		// Same tenancy + user: key rotation / region update in place
		profile.TenancyOCID = p.Tenancy
		profile.UserOCID = p.User
		profile.Fingerprint = p.Fingerprint
		profile.Region = p.Region
		profile.PrivateKeyEnc = keyEnc
		if tags != "" {
			profile.Tags = tags
		}
		if notes != "" {
			profile.Notes = notes
		}
		profile.Status = "Active"
		profile.StatusMessage = ""
		if err := storage.DB.Save(&profile).Error; err != nil {
			return nil, "", fmt.Errorf("保存账号失败: %v", err)
		}
	} else {
		profile = storage.OCIProfile{
			Name:                name,
			TenancyOCID:         p.Tenancy,
			UserOCID:            p.User,
			Fingerprint:         p.Fingerprint,
			Region:              p.Region,
			PrivateKeyEnc:       keyEnc,
			AccountTypeOverride: "auto",
			Tags:                tags,
			Notes:               notes,
			Status:              "Active",
			IsActive:            true,
		}
		if err := storage.DB.Create(&profile).Error; err != nil {
			return nil, "", fmt.Errorf("保存账号失败: %v", err)
		}
	}

	oci.InvalidateProfileCache(profile.ID)
	return &profile, note, nil
}

// ListProfiles returns all OCI profiles (sensitive keys omitted)
func ListProfiles(c *gin.Context) {
	var profiles []storage.OCIProfile
	storage.DB.Order("id ASC").Find(&profiles)

	c.JSON(http.StatusOK, gin.H{
		"profiles": profiles,
	})
}

// ImportRawProfile parses the Oracle console config block and stores the encrypted profile
func ImportRawProfile(c *gin.Context) {
	var req ImportRawProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	sections := parseOCIConfig(req.RawConfig)
	if len(sections) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "未能从粘贴文本中解析出 tenancy, user, fingerprint, region。请粘贴 Oracle 控制台「添加 API 密钥」生成的配置块。",
		})
		return
	}
	if len(sections) > 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("粘贴内容包含 %d 个配置段，请一次只导入一个账号，或使用「同步宿主机配置」", len(sections)),
		})
		return
	}
	section := sections[0]

	pemContent := strings.TrimSpace(req.PrivateKeyPEM)
	if pemContent == "" {
		targetPath := strings.TrimSpace(req.KeyFilePath)
		if targetPath == "" {
			targetPath = section.KeyFile
		}
		if targetPath != "" {
			content, err := readKeyFile(targetPath)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			pemContent = content
		}
	}

	profile, note, err := upsertProfile(section, pemContent, req.Tags, req.Notes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	storage.LogAudit("IMPORT_PROFILE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), fmt.Sprintf("Imported profile %s (%s)", profile.Name, profile.Region), "SUCCESS")

	// Initial connectivity check
	health, _ := oci.CheckSingleAccountHealth(c.Request.Context(), profile)

	msg := "OCI 账号导入成功"
	if note != "" {
		msg += "。" + note
	}
	if health != nil && !health.IsHealthy {
		msg += "。连通性检查未通过：" + health.Message
	}

	c.JSON(http.StatusOK, gin.H{
		"message": msg,
		"profile": profile,
		"health":  health,
	})
}

// UpdateProfile updates tags, notes, and override settings
func UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	updates := map[string]interface{}{
		"tags":  req.Tags,
		"notes": req.Notes,
	}
	if req.AccountTypeOverride != "" {
		switch strings.ToLower(req.AccountTypeOverride) {
		case "auto", "free", "payg":
			updates["account_type_override"] = strings.ToLower(req.AccountTypeOverride)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "account_type_override 只能是 auto / free / payg"})
			return
		}
	}

	storage.DB.Model(&profile).Updates(updates)

	c.JSON(http.StatusOK, gin.H{
		"message": "Profile 更新成功",
		"profile": profile,
	})
}

// DeleteProfile stops the profile's tasks and deletes the profile
func DeleteProfile(c *gin.Context) {
	profile, ok := profileFromParam(c)
	if !ok {
		return
	}

	// Workers must not keep launching with credentials that no longer exist
	var running []storage.LaunchTask
	storage.DB.Where("profile_id = ? AND status = ?", profile.ID, "running").Find(&running)
	for _, t := range running {
		engine.StopTask(t.ID)
	}
	storage.DB.Model(&storage.LaunchTask{}).
		Where("profile_id = ? AND status = ?", profile.ID, "stopped").
		Update("last_message", "关联账号已删除，任务停止")

	oci.InvalidateProfileCache(profile.ID)
	storage.DB.Delete(&profile)

	storage.LogAudit("DELETE_PROFILE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), fmt.Sprintf("Deleted profile ID %d (%d running tasks stopped)", profile.ID, len(running)), "SUCCESS")

	c.JSON(http.StatusOK, gin.H{"message": "Profile 已删除"})
}

// CheckSingleHealth executes a single-account health inspection
// STRICT REQUIREMENT: Only single account, no batch!
func CheckSingleHealth(c *gin.Context) {
	profile, ok := profileFromParam(c)
	if !ok {
		return
	}

	result, err := oci.CheckSingleAccountHealth(c.Request.Context(), &profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"result": result,
	})
}

// SyncLocalProfiles imports every profile of the host's ~/.oci/config (mounted read-only into the container)
func SyncLocalProfiles(c *gin.Context) {
	var configPath string
	for _, dir := range allowedKeyDirs() {
		candidate := filepath.Join(dir, "config")
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			configPath = candidate
			break
		}
	}
	if configPath == "" {
		c.JSON(http.StatusOK, gin.H{
			"message": "宿主机 ~/.oci/config 不存在，没有可同步的账号",
			"count":   0,
		})
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取 " + configPath + ": " + err.Error()})
		return
	}

	sections := parseOCIConfig(string(data))
	imported := 0
	var problems []string
	for _, section := range sections {
		if section.KeyFile == "" {
			problems = append(problems, fmt.Sprintf("[%s] 缺少 key_file", section.Name))
			continue
		}
		keyPath := section.KeyFile
		if !filepath.IsAbs(keyPath) && !strings.HasPrefix(keyPath, "~") {
			keyPath = filepath.Join(filepath.Dir(configPath), keyPath)
		}
		pemContent, err := readKeyFile(keyPath)
		if err != nil {
			problems = append(problems, fmt.Sprintf("[%s] %v", section.Name, err))
			continue
		}
		if _, _, err := upsertProfile(section, pemContent, "", ""); err != nil {
			problems = append(problems, fmt.Sprintf("[%s] %v", section.Name, err))
			continue
		}
		imported++
	}

	storage.LogAudit("SYNC_LOCAL_PROFILES", "admin", c.ClientIP(), c.GetHeader("User-Agent"), fmt.Sprintf("Synced %d profiles from %s", imported, configPath), "SUCCESS")

	msg := fmt.Sprintf("已从 %s 同步 %d 个账号", configPath, imported)
	if len(problems) > 0 {
		msg += "；跳过：" + strings.Join(problems, "；")
	}
	c.JSON(http.StatusOK, gin.H{
		"message":  msg,
		"count":    imported,
		"problems": problems,
	})
}
