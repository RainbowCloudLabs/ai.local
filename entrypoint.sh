#!/bin/sh
DATA_DIR="/etc/ai.local"

echo "[ai.local Ingress] Energizing container infrastructure under $DATA_DIR..."

# Smart TLS shield: Auto-generate self-signed certificates if missing during boot
if [ ! -f "$DATA_DIR/ai.local.crt" ] || [ ! -f "$DATA_DIR/ai.local.key" ]; then
  echo "[ai.local Ingress] TLS credentials missing. Auto-generating self-signed certificates..."
  ai-local-server -d "$DATA_DIR" -gen-cert
fi

echo "[ai.local Ingress] Shielding verified. Handing over PID 1 to Go Data-Plane Engine..."
echo "=========================================================================="

# 3. Use exec to replace shell with Go process, allowing precise SIGTERM / Ctrl+C signal trapping
exec ai-local-server "$@"
