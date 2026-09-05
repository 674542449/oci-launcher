#!/usr/bin/env bash
# ==============================================================================
# OCI 免费额度多账号控制台 - Ubuntu / Debian 一键依赖检测与安全部署脚本
# ==============================================================================
set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}=====================================================================${NC}"
echo -e "${GREEN}🚀 OCI 免费额度多账号控制台 (Enterprise v2.0) 自动化安装程序${NC}"
echo -e "${BLUE}=====================================================================${NC}"

# 1. Check Root Privileges
if [ "$EUID" -ne 0 ]; then
  echo -e "${RED}[错误] 请使用 sudo 或 root 用户权限运行此脚本！${NC}"
  exit 1
fi

# 2. Check OS distribution
if [ -f /etc/os-release ]; then
  . /etc/os-release
  OS=$ID
else
  echo -e "${RED}[错误] 无法识别当前操作系统，仅支持 Ubuntu 或 Debian 系统！${NC}"
  exit 1
fi

echo -e "${GREEN}[1/5] 检测到操作系统: ${NAME} (${VERSION_ID})${NC}"

# 3. Update packages and install dependencies
echo -e "${BLUE}[2/5] 正在安装基础系统依赖 (curl, git, ca-certificates, openssl)...${NC}"
apt-get update -y
apt-get install -y curl git ca-certificates openssl gnupg lsb-release

# 4. Check and install Docker & Docker Compose
echo -e "${BLUE}[3/5] 检测 Docker 与容器运行时...${NC}"
if ! command -v docker &> /dev/null; then
  echo -e "${YELLOW}未检测到 Docker，正在自动安装官方最新 Docker...${NC}"
  curl -fsSL https://get.docker.com | bash
  systemctl enable --now docker
  echo -e "${GREEN}Docker 安装成功！${NC}"
else
  echo -e "${GREEN}Docker 已安装: $(docker --version)${NC}"
fi

# Check Docker Compose plugin
if ! docker compose version &> /dev/null; then
  echo -e "${YELLOW}正在安装 Docker Compose 插件...${NC}"
  apt-get install -y docker-compose-plugin
fi

# 5. Generate Random Secrets for .env
echo -e "${BLUE}[4/5] 正在生成高熵加密密钥与环境配置...${NC}"
PROJECT_DIR=$(pwd)

if [ ! -f .env ]; then
  DB_PASS=$(openssl rand -base64 18 | tr -dc 'a-zA-Z0-9')
  REDIS_PASS=$(openssl rand -base64 18 | tr -dc 'a-zA-Z0-9')
  MASTER_KEY=$(openssl rand -base64 24 | tr -dc 'a-zA-Z0-9!@#$%^&*')

  cat <<EOF > .env
# OCI Panel Production Secrets
POSTGRES_DB=oci_panel
POSTGRES_USER=oci_admin
POSTGRES_PASSWORD=${DB_PASS}
REDIS_PASSWORD=${REDIS_PASS}
MASTER_KEY=${MASTER_KEY}
ALLOWED_IPS=
CLOUDFLARE_TUNNEL_TOKEN=
EOF
  chmod 600 .env
  echo -e "${GREEN}已自动生成高强随机密码并写入 .env (权限已锁定为 600)${NC}"
fi

# 6. Build and start services via Docker Compose
echo -e "${BLUE}[5/5] 正在构建并启动多容器服务 (PostgreSQL, Redis, Go 后端, Vue3 前端)...${NC}"
docker compose up -d --build

echo -e "\n${GREEN}=====================================================================${NC}"
echo -e "${GREEN}🎉 恭喜！OCI 免费额度多账号控制台部署成功并已在后台稳定运行！${NC}"
echo -e "${GREEN}=====================================================================${NC}"
echo -e "🌐 本地访问地址: ${YELLOW}http://127.0.0.1:8000${NC}"
echo -e "🔒 安全机制说明:"
echo -e "  1. 系统默认仅监听 ${YELLOW}127.0.0.1:8000${NC} 本地回环，公网无法直接探测，杜绝全网扫描。"
echo -e "  2. 推荐使用 Cloudflare Tunnel 映射域名访问（实现零公网端口暴露）。"
echo -e "  3. 首次访问请打开页面完成管理员账号与 2FA (TOTP) 身份验证器绑定！"
echo -e "=====================================================================\n"
