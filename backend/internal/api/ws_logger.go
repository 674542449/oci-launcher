package api

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Every socket holds its own (unpooled) Redis Pub/Sub connection; keep the count bounded.
const maxLogSockets = 64

var openLogSockets int64

var upgrader = websocket.Upgrader{
	// Same-origin only: a page on another origin must not be able to subscribe to task logs
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (no Origin header) already passed cookie auth
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	},
}

// HandleTaskLogWS streams task retry logs via WebSocket using Redis Pub/Sub.
// The route is registered behind RequireAuth (the session cookie is sent with the upgrade request).
func HandleTaskLogWS(c *gin.Context) {
	taskID, err := uuid.Parse(c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}
	var task storage.LaunchTask
	if err := storage.DB.Select("id").First(&task, "id = ?", taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	if atomic.AddInt64(&openLogSockets, 1) > maxLogSockets {
		atomic.AddInt64(&openLogSockets, -1)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many open log streams"})
		return
	}
	defer atomic.AddInt64(&openLogSockets, -1)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	pubsub := cache.RDB.Subscribe(ctx, "task:logs:"+taskID.String())
	defer pubsub.Close()
	ch := pubsub.Channel()

	// Read pump: detects client close and answers pings
	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				cancel()
				return
			}
		}
	}()

	// Keep proxies from idling the connection out while a task sleeps between attempts
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				return
			}
		}
	}
}
