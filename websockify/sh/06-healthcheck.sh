#!/usr/bin/env bash
# 6. 健康检查
# 不只是「进程活着」：页面、状态接口、以及 WebSocket 背后的真实 VNC 后端都要能通。
set -e

PORT="${APP_PORT:-10063}"
VNC_SERVER="${VNC_SERVER:-127.0.0.1:5901}"
VNC_HOST="${VNC_SERVER%:*}"
VNC_PORT="${VNC_SERVER##*:}"
APP_DIR=/srv/apps/tio-websockify

echo '=== 等待 HTTP 就绪 ==='
for i in $(seq 1 60); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/vnc.html" || true)
  if [ "$code" = "200" ]; then
    break
  fi
  sleep 2
done

if [ "$code" != "200" ]; then
  echo "GET /vnc.html 返回 ${code}，未就绪"
  tail -60 "$APP_DIR/logs/startup.log" 2>/dev/null || true
  tail -60 "$APP_DIR/logs/startup-error.log" 2>/dev/null || true
  exit 1
fi
echo "  GET /vnc.html            -> 200"

echo
echo '=== 状态接口 ==='
body=$(curl -s "http://127.0.0.1:${PORT}/websocketify/index")
echo "  GET /websocketify/index  -> ${body}"

echo
echo '=== 静态资源 ==='
for res in /vnc.html /app/ui.js /core/rfb.js; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}${res}")
  printf '  %-16s -> %s\n' "$res" "$code"
done

echo
echo '=== WebSocket /websockify 端到端 ==='
python3 - "$PORT" "$VNC_HOST" "$VNC_PORT" <<'PY'
import base64, hashlib, os, socket, struct, sys

port, vnc_host, vnc_port = int(sys.argv[1]), sys.argv[2], int(sys.argv[3])

key = base64.b64encode(os.urandom(16)).decode()
req = (
    "GET /websockify HTTP/1.1\r\n"
    "Host: 127.0.0.1:%d\r\n" % port
) + (
    "Upgrade: websocket\r\n"
    "Connection: Upgrade\r\n"
    "Sec-WebSocket-Version: 13\r\n"
    "Sec-WebSocket-Key: %s\r\n"
    "Sec-WebSocket-Protocol: binary\r\n\r\n" % key
)

s = socket.create_connection(("127.0.0.1", port), timeout=10)
s.sendall(req.encode())

buf = b""
while b"\r\n\r\n" not in buf:
    chunk = s.recv(4096)
    if not chunk:
        raise SystemExit("server closed during handshake")
    buf += chunk
head, rest = buf.split(b"\r\n\r\n", 1)
status = head.split(b"\r\n")[0].decode()
print("  HTTP  ", status)
if "101" not in status:
    raise SystemExit("websocket upgrade failed")

expect = base64.b64encode(
    hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()
).decode()
if expect.lower() not in head.decode().lower():
    raise SystemExit("Sec-WebSocket-Accept mismatch")
print("  accept ok")

def frame(payload, opcode=2):
    mask = os.urandom(4)
    n = len(payload)
    header = bytes([0x80 | opcode])
    if n < 126:
        header += bytes([0x80 | n])
    elif n < 65536:
        header += bytes([0x80 | 126]) + struct.pack(">H", n)
    else:
        header += bytes([0x80 | 127]) + struct.pack(">Q", n)
    masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
    return header + mask + masked

def read_exact(sock, n):
    out = b""
    while len(out) < n:
        chunk = sock.recv(n - len(out))
        if not chunk:
            raise SystemExit("connection closed by relay (backend unreachable?)")
        out += chunk
    return out

def read_frame(sock):
    b0, b1 = read_exact(sock, 2)
    opcode = b0 & 0x0F
    length = b1 & 0x7F
    if length == 126:
        length = struct.unpack(">H", read_exact(sock, 2))[0]
    elif length == 127:
        length = struct.unpack(">Q", read_exact(sock, 8))[0]
    return opcode, read_exact(sock, length)

# 后端是 VNC：发一帧假数据，WebSocket 应保持打开并原样把后端数据回推。
s.sendall(frame(b"deploy-probe"))
s.settimeout(10)
opcode, payload = read_frame(s)
print("  ws frame", "opcode=%d" % opcode, "len=%d" % len(payload))
if len(payload) < 12:
    raise SystemExit("relay returned too little data: %r" % payload)
banner = payload[:12].decode("ascii", "replace")
print("  payload head:", banner.strip())
if not banner.startswith("RFB "):
    raise SystemExit("relay did not proxy the VNC banner: %r" % banner)

# 同时确认后端本身确实在监听（排除「恰好在别处成功」的可能）。
with socket.create_connection((vnc_host, vnc_port), timeout=5) as direct:
    direct_banner = direct.recv(12).decode("ascii", "replace").strip()
print("  backend :", vnc_host + ":" + str(vnc_port), "->", direct_banner)
s.close()
print("  relay ok")
PY

echo
echo '健康检查通过'