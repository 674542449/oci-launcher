package api

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

// Saved SSH public keys: pasted once in Settings, picked from a list when creating instances.
// A default key is chosen per account (never globally), so nothing nudges the same key into
// every tenancy.

var sshKeyTypeRe = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ssh-dss|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com)$`)

type sshKeyRequest struct {
	Name      string `json:"name" binding:"required,max=64"`
	PublicKey string `json:"public_key" binding:"required"`
}

type sshKeyView struct {
	storage.SSHKey
	DefaultFor []string `json:"default_for"` // names of the accounts that use this key by default
}

// parseSSHPublicKey validates an OpenSSH public key line and returns its type, SHA256
// fingerprint (the form ssh-keygen -l prints) and the trailing comment.
func parseSSHPublicKey(raw string) (keyType, fingerprint, comment string, err error) {
	line := strings.TrimSpace(raw)
	if strings.Contains(line, "PRIVATE KEY") {
		return "", "", "", errors.New("所提供内容为私钥，请粘贴 .pub 公钥")
	}
	if strings.ContainsAny(line, "\r\n") {
		return "", "", "", errors.New("每次仅可保存一行公钥；多个密钥请分别添加")
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !sshKeyTypeRe.MatchString(fields[0]) {
		return "", "", "", errors.New("不是有效的 OpenSSH 公钥（应以 ssh-ed25519、ssh-rsa 或 ecdsa-sha2-… 开头）")
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(blob) < 8 {
		return "", "", "", errors.New("公钥的 Base64 部分无法解析")
	}
	sum := sha256.Sum256(blob)
	fingerprint = "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
	if len(fields) > 2 {
		comment = strings.Join(fields[2:], " ")
	}
	return fields[0], fingerprint, comment, nil
}

func sshKeyViews(keys []storage.SSHKey) []sshKeyView {
	var profiles []storage.OCIProfile
	storage.DB.Where("default_ssh_key_id > 0").Order("id ASC").Find(&profiles)
	byKey := map[uint][]string{}
	for _, p := range profiles {
		byKey[p.DefaultSSHKeyID] = append(byKey[p.DefaultSSHKeyID], p.Name)
	}
	views := make([]sshKeyView, 0, len(keys))
	for _, k := range keys {
		names := byKey[k.ID]
		if names == nil {
			names = []string{}
		}
		views = append(views, sshKeyView{SSHKey: k, DefaultFor: names})
	}
	return views
}

// ListSSHKeys returns the saved public keys with the accounts each one is the default for.
func ListSSHKeys(c *gin.Context) {
	var keys []storage.SSHKey
	storage.DB.Order("created_at ASC").Find(&keys)
	c.JSON(http.StatusOK, gin.H{"keys": sshKeyViews(keys)})
}

// CreateSSHKey stores one public key.
func CreateSSHKey(c *gin.Context) {
	var req sshKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写名称与公钥（名称不超过 64 个字符）"})
		return
	}
	keyType, fingerprint, _, err := parseSSHPublicKey(req.PublicKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var dup int64
	storage.DB.Model(&storage.SSHKey{}).Where("fingerprint = ?", fingerprint).Count(&dup)
	if dup > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该公钥已存在（指纹相同）"})
		return
	}

	key := storage.SSHKey{
		Name:        strings.TrimSpace(req.Name),
		KeyType:     keyType,
		PublicKey:   strings.TrimSpace(req.PublicKey),
		Fingerprint: fingerprint,
		CreatedAt:   time.Now(),
	}
	if err := storage.DB.Create(&key).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存公钥失败: " + err.Error()})
		return
	}
	storage.LogAudit("ADD_SSH_KEY", "admin", c.ClientIP(), c.GetHeader("User-Agent"), key.Name+" "+fingerprint, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": "公钥已保存", "key": sshKeyView{SSHKey: key, DefaultFor: []string{}}})
}

// DeleteSSHKey removes a saved key and clears it as any account's default.
func DeleteSSHKey(c *gin.Context) {
	id, ok := parseID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公钥 ID"})
		return
	}
	var key storage.SSHKey
	if err := storage.DB.First(&key, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "公钥不存在"})
		return
	}
	storage.DB.Delete(&key)
	storage.DB.Model(&storage.OCIProfile{}).Where("default_ssh_key_id = ?", key.ID).Update("default_ssh_key_id", 0)
	storage.LogAudit("DELETE_SSH_KEY", "admin", c.ClientIP(), c.GetHeader("User-Agent"), key.Name+" "+key.Fingerprint, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": "公钥已删除"})
}

// SetDefaultSSHKey makes a key the default for one account (?profile_id=). It reports the
// other accounts already using the key, so reuse across tenancies is a conscious choice.
func SetDefaultSSHKey(c *gin.Context) {
	id, ok := parseID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公钥 ID"})
		return
	}
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}
	var key storage.SSHKey
	if err := storage.DB.First(&key, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "公钥不存在"})
		return
	}

	var others []storage.OCIProfile
	storage.DB.Where("default_ssh_key_id = ? AND id <> ?", key.ID, profile.ID).Find(&others)
	names := make([]string, 0, len(others))
	for _, o := range others {
		names = append(names, o.Name)
	}

	storage.DB.Model(&profile).Update("default_ssh_key_id", key.ID)
	msg := "已设为「" + profile.Name + "」的默认公钥"
	if len(names) > 0 {
		msg += "。注意：该公钥同时为 " + strings.Join(names, "、") + " 的默认公钥，多个账号共用同一公钥存在被关联的风险"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "shared_with": names})
}
