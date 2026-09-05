package api

import (
	"context"
	"log"
	"net/http"

	"oci-panel/internal/cache"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Origin checked by reverse proxy and CSP
	},
}

// HandleTaskLogWS streams task retry logs via WebSocket using Redis Pub/Sub
func HandleTaskLogWS(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID required"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	channelName := "task:logs:" + taskID
	pubsub := cache.RDB.Subscribe(ctx, channelName)
	defer pubsub.Close()

	ch := pubsub.Channel()

	// Read pump to detect client close
	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				cancel()
				break
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
			if err != nil {
				return
			}
		}
	}
}
