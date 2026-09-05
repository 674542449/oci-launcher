package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"time"

	"oci-panel/internal/storage"
)

type TelegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"` // HTML
}

// esc escapes user-controlled text for Telegram's HTML parse mode (<, >, & must be entities).
func esc(s string) string {
	return html.EscapeString(s)
}

// SendTelegramMessage sends an HTML message through the Bot API
func SendTelegramMessage(botToken, chatID, messageText string) error {
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram bot token or chat id not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := TelegramMessage{
		ChatID:    chatID,
		Text:      messageText,
		ParseMode: "HTML",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var apiErr struct {
			Description string `json:"description"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Description != "" {
			return fmt.Errorf("telegram API error %d: %s", resp.StatusCode, apiErr.Description)
		}
		return fmt.Errorf("telegram API returned status: %d", resp.StatusCode)
	}

	return nil
}

// GetGlobalTelegramConfig retrieves the bot token and chat id from settings
func GetGlobalTelegramConfig() (botToken string, chatID string) {
	var tokenSetting, chatSetting storage.SystemSetting
	if err := storage.DB.First(&tokenSetting, "key = ?", "tg_bot_token").Error; err == nil {
		botToken = tokenSetting.Value
	}
	if err := storage.DB.First(&chatSetting, "key = ?", "tg_chat_id").Error; err == nil {
		chatID = chatSetting.Value
	}
	return botToken, chatID
}

func sendOrLog(kind, text string) {
	botToken, chatID := GetGlobalTelegramConfig()
	if botToken == "" || chatID == "" {
		return
	}
	if err := SendTelegramMessage(botToken, chatID, text); err != nil {
		log.Printf("[Notify] Telegram %s notification failed: %v", kind, err)
	}
}

// NotifyTaskSuccess sends the launch success notification (the instance OCID was returned).
func NotifyTaskSuccess(task *storage.LaunchTask, profile *storage.OCIProfile, publicIP, ipv6, rootPass string) {
	ipText := publicIP
	if ipText == "" {
		ipText = "尚未分配，请稍后在实例页刷新"
	}

	text := fmt.Sprintf(`🎉 <b>OCI 实例创建成功</b>

👤 <b>账号:</b> %s
🏢 <b>区域:</b> %s
🖥️ <b>实例:</b> %s
⚙️ <b>规格:</b> %s (%0.1f OCPU / %0.1f GB / %d GB 引导卷)
🌐 <b>公网 IPv4:</b> <code>%s</code>
`, esc(profile.Name), esc(task.Region), esc(task.InstanceName), esc(task.Shape), task.OCPU, task.MemoryInGBs, task.BootVolumeSizeInGBs, esc(ipText))

	if ipv6 != "" {
		text += fmt.Sprintf("🌐 <b>IPv6:</b> <code>%s</code>\n", esc(ipv6))
	}
	if task.LoginMode == "root_password" && rootPass != "" {
		text += fmt.Sprintf("🔑 <b>Root 密码:</b> <code>%s</code> <i>(同时保存在实例云端标签中)</i>\n", esc(rootPass))
	}
	if publicIP != "" {
		text += fmt.Sprintf("\n💻 <b>登录:</b> <code>ssh root@%s</code>\n", esc(publicIP))
	}

	sendOrLog("success", text)
}

// NotifyTaskFatalError sends the fatal error alert
func NotifyTaskFatalError(task *storage.LaunchTask, profile *storage.OCIProfile, errMsg string) {
	text := fmt.Sprintf(`⚠️ <b>OCI 创建任务已熔断</b>

👤 <b>账号:</b> %s
🖥️ <b>实例:</b> %s
🛑 <b>状态:</b> 已停止（配置或凭据错误，不再重试）
❌ <b>原因:</b>
<code>%s</code>

💡 <i>请修正配置或凭据后在面板中重新启动任务。</i>`, esc(profile.Name), esc(task.InstanceName), esc(errMsg))

	sendOrLog("fatal", text)
}
