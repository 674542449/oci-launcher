package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"oci-panel/internal/storage"
)

type TelegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"` // HTML or MarkdownV2
}

// SendTelegramMessage sends a message to Telegram Bot
func SendTelegramMessage(botToken, chatID, messageText string) error {
	if botToken == "" || chatID == "" {
		return nil
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status: %d", resp.StatusCode)
	}

	return nil
}

// GetGlobalTelegramConfig retrieves global bot token and chat id from settings
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

// NotifyTaskSuccess sends rich success notification
func NotifyTaskSuccess(task *storage.LaunchTask, profile *storage.OCIProfile, publicIP, ipv6, rootPass string) {
	botToken, chatID := GetGlobalTelegramConfig()
	if botToken == "" || chatID == "" {
		return
	}

	sshCmd := fmt.Sprintf("ssh root@%s", publicIP)

	text := fmt.Sprintf(`🎉 <b>【OCI 免费实例抢机成功通知】</b>

👤 <b>账号别名:</b> %s
🏢 <b>租户区域:</b> %s
🖥️ <b>实例名称:</b> %s
⚙️ <b>硬件规格:</b> %s (%0.1f OCPU / %0.1f GB 内存 / %d GB 引导卷)
🌐 <b>公网 IPv4:</b> <code>%s</code>
`, profile.Name, task.Region, task.InstanceName, task.Shape, task.OCPU, task.MemoryInGBs, task.BootVolumeSizeInGBs, publicIP)

	if ipv6 != "" {
		text += fmt.Sprintf("🌐 <b>公网 IPv6:</b> <code>%s</code>\n", ipv6)
	}

	if task.LoginMode == "root_password" && rootPass != "" {
		text += fmt.Sprintf("🔑 <b>Root 安全密码:</b> <code>%s</code> <i>(已自动持久化保存在实例云端标签)</i>\n", rootPass)
	}

	text += fmt.Sprintf("\n💻 <b>一键登录命令:</b>\n<code>%s</code>\n\n✅ <i>开机任务已自动停止。</i>", sshCmd)

	_ = SendTelegramMessage(botToken, chatID, text)
}

// NotifyTaskFatalError sends fatal error alert
func NotifyTaskFatalError(task *storage.LaunchTask, profile *storage.OCIProfile, errMsg string) {
	botToken, chatID := GetGlobalTelegramConfig()
	if botToken == "" || chatID == "" {
		return
	}

	text := fmt.Sprintf(`⚠️ <b>【OCI 抢机任务异常熔断告警】</b>

👤 <b>账号别名:</b> %s
🖥️ <b>实例名称:</b> %s
🛑 <b>任务状态:</b> 立即停止 (Fatal Error)
❌ <b>错误详情:</b>
<code>%s</code>

💡 <i>提示: 该错误属于非容量类配置/权限错误，系统已实施自动熔断，避免带病无效空转。</i>`, profile.Name, task.InstanceName, errMsg)

	_ = SendTelegramMessage(botToken, chatID, text)
}
