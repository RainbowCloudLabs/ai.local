# Deploy AI.local on MikroTik RouterOS (Container)

This guide explains how to run **AI.local** on RouterOS using an OCI image (`ai.local-oci.tar.gz`) and mount APML config from disk.

> Tested conceptually for RouterOS v7 with container package enabled.

---

## 1) Prerequisites

- RouterOS v7 (matching architecture package)
- `container-7.x.npk` installed
- Enough storage (internal disk or USB/SATA)
- OCI image file: `ai.local-oci.tar.gz`
- APML file: `ai.local.apml`

---

## 2) Install container package

1. Download **Extra packages ZIP** matching your exact RouterOS version and architecture.
2. Extract and locate `container-7.x.npk`.
3. Upload to router via Winbox (**Files** window drag-and-drop).
4. Reboot router:
   ```routeros
   /system/reboot
   ```

---

## 3) Enable container mode (important)

```routeros
/system/device-mode/update container=yes
```

Power-cycle as required by RouterOS policy (physical restart window).

Verify after boot:

```routeros
/system/device-mode/print
```

Ensure:
- `container: yes`

---

## 4) Configure container runtime

```routeros
/container/config/set registry-url=https://registry-1.docker.io tmpdir=sata1/pull
/container/config/set ram-high=256MiB
```

> `tmpdir` should point to a writable disk path with enough space.

---

## 5) Network setup for container

Create isolated bridge + subnet + veth:

```routeros
/interface/bridge/add name=docker-bridge
/ip/address/add address=172.27.0.1/24 interface=docker-bridge
/interface/veth/add name=veth1 address=172.27.0.2/24 gateway=172.27.0.1
/interface/bridge/port add bridge=docker-bridge interface=veth1
/ip/firewall/nat add chain=srcnat action=masquerade src-address=172.27.0.0/24
```

Notes:
- Container IP in this guide: `172.27.0.2`
- If your environment already uses `docker0` on this subnet, pick a different private subnet.

---

## 6) DNS and inbound 443 redirect

Create static DNS entry:

```routeros
/ip/dns/static/add name=ai.gateway address=172.27.0.2 ttl=00:00:10
```

Forward HTTPS 443 to container port 8443:

```routeros
/ip/firewall/nat/add chain=dstnat dst-address=172.27.0.2 protocol=tcp dst-port=443 action=dst-nat to-addresses=172.27.0.2 to-ports=8443 comment="ai.gateway 443->8443 redirect"
```

If clients connect to the router/LAN IP, you may instead need a dstnat rule matching router-facing destination IP/interface.

Also ensure input/forward firewall policy allows required traffic from LAN.

---

## 7) Prepare APML mount path

Create host directory and mount into container:

```routeros
/container/mounts/add src=sata1/ai.local/ dst=/etc/ai.local
```

Check mount list ID:

```routeros
/container/mounts/print
```

Upload your `ai.local.apml` into `sata1/ai.local/`.

---

## 8) Import OCI image and create container

Upload `ai.local-oci.tar.gz` to router (example path: `sata1/docker-images/ai.local-oci.tar.gz`), then:

```routeros
/container/add \
    file=sata1/docker-images/ai.local-oci.tar.gz \
    interface=veth1 \
    root-dir=sata1/ai-container \
    mountlists=0 \
    logging=yes
```

`mountlists=0` must match your `/container/mounts/print` list ID.

---

## 9) Start container

```routeros
/container/start 0
```

Enable auto-start on reboot:

```routeros
/container/set 0 start-on-boot=yes
```

---

## 10) Verify status and logs

```routeros
/container/print detail
/log/print where message~"container"
```

Optional connectivity checks from LAN client:
- `curl -k https://ai.gateway/openai/v1/models`
- Or your gateway health endpoint if defined

---

## 11) Troubleshooting

### Container does not start
- Check storage paths (`file=`, `root-dir=`, `tmpdir`)
- Check mount path exists and is readable/writable
- Inspect `/log/print where message~"container"`

### DNS resolves but connection fails
- Verify dstnat/firewall rules
- Confirm container listens on expected port (`8443` in this guide)
- Verify certificate/SAN includes `ai.gateway`

### APML not loaded
- Confirm mount target path inside container is correct (`/etc/ai.local`)
- Confirm `ai.local.apml` exists in mounted source directory

---

## 12) Security recommendations

- Use valid TLS certs (avoid permanent `-k` usage in clients)
- Keep provider API keys inside AI.local only
- Rotate internal keys periodically
- Restrict inbound access to trusted LAN/VPN segments
