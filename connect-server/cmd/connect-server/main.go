// Command connect-server runs two small pieces of infrastructure for the
// hicalsoft.github.io /connect page, which otherwise relies entirely on
// third-party services for WebRTC peer matching:
//
//  1. A WebSocket signaling relay (see internal/relay) that lets two browsers
//     find each other and exchange SDP/ICE data. Actual call audio/video
//     never passes through this server — it's peer-to-peer.
//  2. A TURN server (pion/turn) that relays media when a direct peer-to-peer
//     connection can't be established (e.g. one peer is on cellular data
//     behind a symmetric NAT). Time-limited credentials are minted per the
//     browser's request via GET /turn-credentials and verified by the TURN
//     server using the same in-memory shared secret.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/pion/turn/v2"

	"connect-server/internal/relay"
)

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

func main() {
	httpAddr := flag.String("http-addr", envOr("HTTP_ADDR", "127.0.0.1:8091"), "address for the HTTP/WebSocket relay server (put behind nginx)")
	publicIP := flag.String("public-ip", os.Getenv("PUBLIC_IP"), "public IP address the TURN server is reachable at (required)")
	turnPort := flag.Int("turn-port", envOrInt("TURN_PORT", 3478), "UDP/TCP port for the TURN/STUN server")
	minRelayPort := flag.Int("min-relay-port", envOrInt("TURN_MIN_RELAY_PORT", 49160), "lowest port used for relayed media (forward this range on your router)")
	maxRelayPort := flag.Int("max-relay-port", envOrInt("TURN_MAX_RELAY_PORT", 49460), "highest port used for relayed media (forward this range on your router)")
	realm := flag.String("realm", envOr("TURN_REALM", "connect.marryislam.org"), "TURN realm")
	credentialTTL := flag.Duration("credential-ttl", envOrDuration("TURN_CREDENTIAL_TTL", time.Hour), "how long minted TURN credentials remain valid")
	sharedSecret := flag.String("shared-secret", os.Getenv("TURN_SHARED_SECRET"), "shared secret for minting/verifying TURN credentials (random if unset)")
	flag.Parse()

	if *publicIP == "" {
		log.Fatal("PUBLIC_IP (or -public-ip) is required")
	}

	if *sharedSecret == "" {
		generated, err := randomSecret(32)
		if err != nil {
			log.Fatalf("failed to generate shared secret: %v", err)
		}
		*sharedSecret = generated
		log.Printf("no TURN_SHARED_SECRET set — generated a random one for this process")
	}

	turnServer, err := startTURNServer(*publicIP, *turnPort, *minRelayPort, *maxRelayPort, *realm, *sharedSecret)
	if err != nil {
		log.Fatalf("failed to start TURN server: %v", err)
	}
	defer turnServer.Close()
	log.Printf("TURN/STUN server listening on UDP+TCP %s:%d (relay ports %d-%d)", *publicIP, *turnPort, *minRelayPort, *maxRelayPort)

	hub := relay.NewHub()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ws", hub.ServeWS)
	mux.HandleFunc("/turn-credentials", corsAllowAll(turnCredentialsHandler(*publicIP, *turnPort, *realm, *sharedSecret, *credentialTTL)))

	httpServer := &http.Server{
		Addr:         *httpAddr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("HTTP/WebSocket relay listening on %s (routes: /ws, /turn-credentials, /healthz)", *httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("shutting down")
	httpServer.Close()
}

func startTURNServer(publicIP string, port, minRelayPort, maxRelayPort int, realm, sharedSecret string) (*turn.Server, error) {
	udpListener, err := net.ListenPacket("udp4", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}

	tcpListener, err := net.Listen("tcp4", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		udpListener.Close()
		return nil, err
	}

	relayGenUDP := &turn.RelayAddressGeneratorPortRange{
		RelayAddress: net.ParseIP(publicIP),
		Address:      "0.0.0.0",
		MinPort:      uint16(minRelayPort),
		MaxPort:      uint16(maxRelayPort),
	}
	relayGenTCP := &turn.RelayAddressGeneratorPortRange{
		RelayAddress: net.ParseIP(publicIP),
		Address:      "0.0.0.0",
		MinPort:      uint16(minRelayPort),
		MaxPort:      uint16(maxRelayPort),
	}

	return turn.NewServer(turn.ServerConfig{
		Realm:       realm,
		AuthHandler: turn.NewLongTermAuthHandler(sharedSecret, nil),
		PacketConnConfigs: []turn.PacketConnConfig{
			{PacketConn: udpListener, RelayAddressGenerator: relayGenUDP},
		},
		ListenerConfigs: []turn.ListenerConfig{
			{Listener: tcpListener, RelayAddressGenerator: relayGenTCP},
		},
	})
}

func turnCredentialsHandler(publicIP string, port int, realm, sharedSecret string, ttl time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, err := turn.GenerateLongTermCredentials(sharedSecret, ttl)
		if err != nil {
			http.Error(w, "failed to generate credentials", http.StatusInternalServerError)
			return
		}

		portStr := strconv.Itoa(port)
		servers := []iceServer{
			{URLs: []string{"stun:" + publicIP + ":" + portStr}},
			{
				URLs: []string{
					"turn:" + publicIP + ":" + portStr + "?transport=udp",
					"turn:" + publicIP + ":" + portStr + "?transport=tcp",
				},
				Username:   username,
				Credential: password,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]interface{}{"iceServers": servers, "ttlSeconds": int(ttl.Seconds())})
		_ = realm // realm is used only inside the auth handler; kept here for clarity/future use
	}
}

func corsAllowAll(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envOrDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func randomSecret(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
