#!/usr/bin/env bash
# 服务器一次性初始化（对应需求文档 D6：平台不安装工具链，环境由使用者准备）。
# 装 nginx、建站点根目录、写 maxkb.xyz 的 vhost（HTTP）。
#
# 与仓库里 deploy/nginx-maxkb.xyz.conf 的差异（有意为之）：
#   1. 这台机器是内网机，maxkb.xyz 的 DNS 指向公网 IP，certbot 无法签发证书，
#      所以这里只开 80 端口，不写 HTTPS 段。
#   2. 加 default_server，这样直接用内网 IP 访问也能命中，便于验收。
set -euo pipefail

echo "=== [1/5] apt install nginx ==="
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq nginx >/dev/null
nginx -v

echo "=== [2/5] create web root ==="
mkdir -p /var/www/maxkb.xyz
chown -R www-data:www-data /var/www/maxkb.xyz
ls -ld /var/www/maxkb.xyz

echo "=== [3/5] write vhost ==="
cat > /etc/nginx/sites-available/maxkb.xyz <<'NGINXEOF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name maxkb.xyz www.maxkb.xyz _;

    root /var/www/maxkb.xyz;
    index index.html;

    add_header X-Content-Type-Options nosniff always;
    add_header X-Frame-Options SAMEORIGIN always;
    add_header Referrer-Policy strict-origin-when-cross-origin always;

    charset utf-8;

    # 干净的 URL：/features 也能命中 features.html
    location / {
        try_files $uri $uri.html $uri/ =404;
    }

    location ~* \.(css|js|svg|png|jpe?g|webp|ico|woff2?)$ {
        expires 30d;
        add_header Cache-Control "public, max-age=2592000";
        access_log off;
    }

    location ~* \.html$ {
        add_header Cache-Control "no-cache";
    }

    error_page 404 /404.html;
    location = /404.html {
        internal;
    }

    gzip on;
    gzip_comp_level 5;
    gzip_min_length 1024;
    gzip_types text/plain text/css application/javascript application/json
               image/svg+xml application/xml;

    access_log /var/log/nginx/maxkb.xyz.access.log;
    error_log  /var/log/nginx/maxkb.xyz.error.log;
}
NGINXEOF

rm -f /etc/nginx/sites-enabled/default
ln -sf /etc/nginx/sites-available/maxkb.xyz /etc/nginx/sites-enabled/maxkb.xyz

echo "=== [4/5] nginx -t ==="
nginx -t

echo "=== [5/5] enable + start ==="
systemctl enable nginx >/dev/null 2>&1 || true
systemctl restart nginx
systemctl is-active nginx
ss -lnt | grep -q ':80 ' && echo "port 80 listening"
echo PROVISION_OK
