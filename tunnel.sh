#!/usr/bin/env bash
# ==============================================================================
# Cloudflare Tunnel 一键开关（零公网端口访问控制台）
#
#   ./tunnel.sh <token>   写入 Token、启用 tunnel profile 并启动 cloudflared 容器
#   ./tunnel.sh           复用 .env 里已有的 Token 重新启动
#   ./tunnel.sh off       停用隧道并删除容器
#
# Token 来自 Cloudflare Zero Trust -> Networks -> Tunnels -> Create a tunnel -> Cloudflared，
# 复制安装命令里 eyJ 开头的那一串即可。隧道连上后在 Public Hostname 里添加：
#   Subdomain: oci   Domain: 你的域名   Type: HTTP   URL: frontend:80
# ==============================================================================
set -e
cd "$(dirname "$0")"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
ENV_FILE=".env"

if [ ! -f "$ENV_FILE" ]; then
  echo -e "${RED}[错误] 未找到 .env，请先运行 deploy.sh 完成部署${NC}"
  exit 1
fi

# set_env KEY VALUE : 更新或追加 .env 中的一项
set_env() {
  local key="$1" val="$2"
  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
  else
    echo "${key}=${val}" >> "$ENV_FILE"
  fi
}

if [ "${1:-}" = "off" ]; then
  set_env COMPOSE_PROFILES ""
  docker compose --profile tunnel rm -sf cloudflared >/dev/null 2>&1 || true
  echo -e "${GREEN}隧道已停用。控制台恢复为仅监听 127.0.0.1:8000${NC}"
  exit 0
fi

TOKEN="${1:-}"
if [ -z "$TOKEN" ]; then
  TOKEN=$(grep "^CLOUDFLARE_TUNNEL_TOKEN=" "$ENV_FILE" | head -n1 | cut -d= -f2-)
fi
if [ -z "$TOKEN" ]; then
  echo -e "${RED}用法: ./tunnel.sh <Cloudflare 隧道 Token>${NC}"
  echo "Token 在 Cloudflare Zero Trust 隧道页面的安装命令里，形如 cloudflared service install eyJhIjoi..."
  exit 1
fi
case "$TOKEN" in
  eyJ*) ;;
  *) echo -e "${RED}[错误] Token 应以 eyJ 开头，请只复制安装命令中 install 后面的那一串${NC}"; exit 1 ;;
esac

set_env CLOUDFLARE_TUNNEL_TOKEN "$TOKEN"
set_env COMPOSE_PROFILES "tunnel"
chmod 600 "$ENV_FILE" 2>/dev/null || true

if [ -f docker-compose.override.yml ] && grep -q "cloudflared" docker-compose.override.yml; then
  echo -e "${YELLOW}提示: docker-compose.override.yml 里也定义了 cloudflared，现在不再需要它，可以删除或改回只保留其他内容${NC}"
fi

echo -e "${GREEN}正在启动隧道容器...${NC}"
docker compose up -d

sleep 6
echo -e "${GREEN}---- cloudflared 最近日志 ----${NC}"
docker logs --tail 15 oci_cloudflared 2>&1 || true

if docker logs oci_cloudflared 2>&1 | grep -q "Registered tunnel connection"; then
  echo -e "${GREEN}✅ 隧道已连接${NC}"
else
  echo -e "${YELLOW}还没看到 \"Registered tunnel connection\"，稍等几秒后执行: docker logs --tail 20 oci_cloudflared${NC}"
fi

cat <<EOF

下一步，在 Cloudflare Zero Trust 里为这条隧道添加公共主机名：
  Networks -> Tunnels -> 你的隧道 -> Configure -> Public Hostname -> Add a public hostname
    Subdomain : oci            （可自定）
    Domain    : 你托管在 Cloudflare 的域名
    Type      : HTTP
    URL       : frontend:80    （容器内网地址，不要写服务器 IP）
保存后访问 https://oci.你的域名 即可，无需在 OCI 安全列表开放任何端口。
EOF
