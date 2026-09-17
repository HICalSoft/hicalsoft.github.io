# connect-server

Self-hosted signaling + TURN backend for the [`/connect`](../connect/index.html)
page on hicalsoft.github.io. Runs on your own infrastructure (a Raspberry Pi
behind `marryislam.org`) instead of depending on third-party TURN/signaling
services, which is what fixes phone ↔ laptop calls getting stuck "searching"
(mobile networks are almost always behind a NAT type that plain STUN can't
traverse — you need a TURN relay).

It's a single static Go binary providing:

1. **WebSocket signaling relay** (`/ws`) — lets two browsers on `/connect`
   find each other and swap WebRTC session info. Implements the exact pub/sub
   protocol Trystero's `createTopicStrategy` expects. No call data (audio,
   video) passes through this — only small text messages during setup.
2. **TURN/STUN server** (`pion/turn`, UDP+TCP port 3478) — relays call media
   when a direct peer-to-peer path can't be established. This is the part
   that actually fixes cross-network calls.
3. **`GET /turn-credentials`** — mints short-lived (1 hour default) TURN
   credentials the browser fetches before joining a call. No permanent shared
   secret is ever exposed to the browser.

## 1. Requirements on the Pi

- Go 1.15+ (already confirmed installed)
- A public IP reachable from the internet, with your router able to forward:
  - UDP **and** TCP port `3478` → the Pi
  - UDP **and** TCP port range `49160–49460` → the Pi (TURN relay media ports;
    range is configurable, see below)
- DNS: an `A`/`AAAA` record for `connect.marryislam.org` pointing at the same
  public IP as `marryislam.org`
- nginx already running (per the existing `marryislam.org` setup) with
  certbot for TLS

## 2. Build

From this directory, either build directly on the Pi:

```sh
go build -o connect-server ./cmd/connect-server
```

...or cross-compile from a dev machine and `scp` the binary over:

```sh
GOOS=linux GOARCH=arm64 go build -o connect-server-linux-arm64 ./cmd/connect-server
scp connect-server-linux-arm64 pi@<pi-host>:/opt/connect-server/connect-server
```

(Use `GOARCH=arm` instead of `arm64` if the Pi is running a 32-bit OS —
check with `uname -m`: `aarch64`/`arm64` = 64-bit, `armv7l` = 32-bit.)

## 3. Deploy

```sh
sudo useradd --system --no-create-home --shell /usr/sbin/nologin connect-server
sudo mkdir -p /opt/connect-server
sudo cp connect-server /opt/connect-server/
sudo chown -R connect-server:connect-server /opt/connect-server
sudo cp deploy/connect-server.service /etc/systemd/system/
# Edit PUBLIC_IP in the unit file (or the /etc/systemd/system copy) to your
# Pi's actual public IP address:
sudo systemctl edit --full connect-server.service
sudo systemctl daemon-reload
sudo systemctl enable --now connect-server
sudo systemctl status connect-server
```

Check it's alive:

```sh
curl http://127.0.0.1:8091/healthz          # -> ok
curl http://127.0.0.1:8091/turn-credentials # -> {"iceServers": [...]}
```

## 4. nginx + TLS

```sh
sudo cp deploy/nginx-connect.conf /etc/nginx/sites-available/connect.marryislam.org
sudo ln -s /etc/nginx/sites-available/connect.marryislam.org /etc/nginx/sites-enabled/
sudo certbot --nginx -d connect.marryislam.org
sudo nginx -t && sudo systemctl reload nginx
```

## 5. Router port forwarding

Forward these directly to the Pi's LAN IP (not through nginx — TURN is not
HTTP and nginx isn't in this path):

| Port(s)         | Protocol | Purpose                      |
|------------------|----------|------------------------------|
| 3478             | UDP+TCP  | STUN/TURN control            |
| 49160–49460      | UDP+TCP  | Relayed call media           |

## 6. Verify from the internet

```sh
curl https://connect.marryislam.org/healthz
curl https://connect.marryislam.org/turn-credentials
```

Both should work from outside your home network (e.g. from a phone on
cellular data, not your home WiFi) — that's the actual scenario being fixed.

The `/connect` page on hicalsoft.github.io already points at
`wss://connect.marryislam.org/ws` and
`https://connect.marryislam.org/turn-credentials` — once this is deployed and
DNS has propagated, phone ↔ laptop calls should connect normally.

## Configuration reference

All via environment variables (or equivalent `-flag`, see `-h`):

| Variable                | Default                     | Notes                                    |
|--------------------------|------------------------------|-------------------------------------------|
| `PUBLIC_IP`              | *(required)*                 | Public IP the TURN server is reachable at |
| `HTTP_ADDR`              | `127.0.0.1:8091`             | Signaling/credentials HTTP server, kept on loopback and reverse-proxied by nginx |
| `TURN_PORT`              | `3478`                      | STUN/TURN control port |
| `TURN_MIN_RELAY_PORT`    | `49160`                     | Start of relay media port range |
| `TURN_MAX_RELAY_PORT`    | `49460`                     | End of relay media port range |
| `TURN_REALM`             | `connect.marryislam.org`    | TURN realm string |
| `TURN_CREDENTIAL_TTL`    | `1h`                        | How long minted TURN credentials are valid |
| `TURN_SHARED_SECRET`     | *(random per boot)*          | Only needs to be pinned if you have multiple processes needing to share it |

## Local testing

```sh
go build -o /tmp/connect-server ./cmd/connect-server
/tmp/connect-server -public-ip=127.0.0.1 -http-addr=127.0.0.1:8091 -turn-port=3479 \
  -min-relay-port=49500 -max-relay-port=49550 -shared-secret=testsecret
curl http://127.0.0.1:8091/healthz
curl http://127.0.0.1:8091/turn-credentials
```
