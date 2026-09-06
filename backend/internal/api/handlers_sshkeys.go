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

var sshKeyTypeRe = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ssh-dss|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com)$`)

type sshKeyRequest struct {
	Name      string `json:"name" binding:"required,max=64"`
	PublicKey string `json:"public_key" binding:"required"`
}

// parseSSHPublicKey validates an OpenSSH public key line and returns its type, SHA256
// fingerprint (the form ssh-keygen -l prints) and the trailing comment.
func parseSSHPublicKey(raw string) (keyType, fingerprint, comment string, err error) {
	line := strings.TrimSpace(raw)
	if strings.Contains(line, "PRIVATE KEY") {
		return "", "", "", errors.New("这是私钥，请粘贴 .pub 公钥")
	}
	if strings.ContainsAny(line, "\r\n") {
		return "", "", "", errors.New("只能保存一行公钥；多把密钥请分别添加")
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

// ListSSHKeys returns the saved public keys, default first.
func ListSSHKeys(c *gin.Context) {
	var keys []storage.SSHKey
	storage.DB.Order("is_default DESC, created_at ASC").Find(&keys)
	c.JSON(http.StatusOK, gin.H{"keys": keys})
}

// CreateSSHKey stores one public key; the first key saved becomes the default.
func CreateSSHKey(c *gin.Context) {
	var req sshKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写名称和公钥（名称不超过 64 个字符）"})
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
		c.JSON(http.StatusConflict, gin.H{"error": "这把公钥已经保存过（指纹相同）"})
		return
	}
	var total int64
	storage.DB.Model(&storage.SSHKey{}).Count(&total)

	key := storage.SSHKey{
		Name:        strings.TrimSpace(req.Name),
		KeyType:     keyType,
		PublicKey:   strings.TrimSpace(req.PublicKey),
		Fingerprint: fingerprint,
		IsDefault:   total == 0,
		CreatedAt:   time.Now(),
	}
	if err := storage.DB.Create(&key).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存公钥失败: " + err.Error()})
		return
	}
	storage.LogAudit("ADD_SSH_KEY", "admin", c.ClientIP(), c.GetHeader("User-Agent"), key.Name+" "+fingerprint, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": "公钥已保存", "key": key})
}

// DeleteSSHKey removes a saved key; if it was the default, the oldest remaining key takes over.
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
	if key.IsDefault {
		var next storage.SSHKey
		if err := storage.DB.Order("created_at ASC").First(&next).Error; err == nil {
			storage.DB.Model(&next).Update("is_default", true)
		}
	}
	storage.LogAudit("DELETE_SSH_KEY", "admin", c.ClientIP(), c.GetHeader("User-Agent"), key.Name+" "+key.Fingerprint, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": "公钥已删除"})
}

// SetDefaultSSHKey marks one key as the default pre-selected when creating instances.
func SetDefaultSSHKey(c *gin.Context) {
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
	storage.DB.Model(&storage.SSHKey{}).Where("is_default = ?", true).Update("is_default", false)
	storage.DB.Model(&key).Update("is_default", true)
	c.JSON(http.StatusOK, gin.H{"message": "已设为默认公钥"})
}
