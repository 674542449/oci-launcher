package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"oci-panel/internal/api"
	"oci-panel/internal/cache"
	"oci-panel/internal/config"
	"oci-panel/internal/engine"
	"oci-panel/internal/storage"
)

func main() {
	log.Println("==================================================")
	log.Println("🚀 启动 OCI 免费额度多账号控制台 (Enterprise v2.0)")
	log.Println("==================================================")

	// 1. Load configuration
	cfg := config.LoadConfig()
	log.Printf("[Init] 运行环境: %s, 端口: %d", cfg.AppEnv, cfg.AppPort)

	// 2. Initialize PostgreSQL
	_, err := storage.InitDB(cfg.DBDSN)
	if err != nil {
		log.Fatalf("[FATAL] 无法连接 PostgreSQL 数据库: %v", err)
	}
	log.Println("[Init] PostgreSQL 数据库连接与自动迁移完成")

	// 3. Initialize Redis
	_, err = cache.InitRedis(cfg.RedisAddr, cfg.RedisPass)
	if err != nil {
		log.Fatalf("[FATAL] 无法连接 Redis 中间件: %v", err)
	}

	// 4. Resume active tasks from DB
	engine.ResumeAllRunningTasks()

	// 5. Setup Gin Router
	router := api.SetupRouter()

	srv := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", cfg.AppPort),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown listener
	go func() {
		log.Printf("🌐 OCI 后端 API 正在监听: http://0.0.0.0:%d", cfg.AppPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] HTTP 服务异常退出: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 正在优雅关闭服务...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("强制关闭服务: %v", err)
	}
	log.Println("✅ 服务已安全关闭。")
}
