package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"airsim.local/mu300/internal/gateway"
	"airsim.local/mu300/internal/pairing"
)

// Exercise the public encrypted protocol, including code authentication, rather
// than invoking registration helpers directly. All identities are test fixtures.
func encryptedPairingClaim(t *testing.T, s *pairing.Session, code string, reg map[string]any) []byte {
	t.Helper()
	client, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverBytes, err := base64.RawURLEncoding.DecodeString(s.Public)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ecdh.X25519().NewPublicKey(serverBytes)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := client.ECDH(server)
	if err != nil {
		t.Fatal(err)
	}
	extract := hmac.New(sha256.New, []byte(s.ID))
	extract.Write(shared)
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write([]byte("AirSIMPair/v1:" + code))
	expand.Write([]byte{1})
	block, err := aes.NewCipher(expand.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(pairing.Claim{
		Version: 1, SessionID: s.ID,
		ClientPublic: base64.RawURLEncoding.EncodeToString(client.PublicKey().Bytes()),
		Sealed:       base64.RawURLEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, plain, []byte(s.ID))...)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func pairingFixture(t *testing.T, transport callTransport) *daemon {
	t.Helper()
	g, err := gateway.New(filepath.Join(t.TempDir(), "keys.json"), "mu300-test")
	if err != nil {
		t.Fatal(err)
	}
	return &daemon{gateway: g, deviceID: "mu300-test", client: &http.Client{Transport: transport}}
}

func pairingRegistrationFixture(key byte) map[string]any {
	return map[string]any{
		"device_id": "iphone-test", "device_secret": strings.Repeat("s", 32),
		"vowlan_secret": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{key}, 32)),
		"voip_token":    strings.Repeat("a", 64), "bundle_id": "com.example.airsim",
		"environment": "sandbox", "relay_url": "https://relay.example.test",
		"cloud_enabled":              false,
		"live_activity_presentation": "agent_status",
	}
}

func TestRepeatPairingAcknowledgesStoredRegistrationWithoutCloudReadiness(t *testing.T) {
	for _, secondStatus := range []string{
		`{"configured":false,"cloud_enabled":false,"call_push_ready":false,"message_push_ready":false}`,
		`{"configured":false,"cloud_enabled":false,"management_blocked":"local_mode"}`,
		`{"configured":false,"cloud_enabled":false,"management_blocked":"relay_pairing_required"}`,
		`{"configured":true,"cloud_enabled":false,"call_push_ready":false}`,
	} {
		t.Run(secondStatus, func(t *testing.T) {
			requests := 0
			d := pairingFixture(t, func(r *http.Request) (*http.Response, error) {
				requests++
				if r.Method != http.MethodPost || r.URL.String() != "http://127.0.0.1:8575/api/push/register" {
					t.Fatal("unexpected registration destination")
				}
				var reg map[string]any
				if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
					t.Fatal(err)
				}
				if reg["cloud_enabled"] != (requests == 1) || reg["android_agent_id"] != "mu300-test" || reg["vowlan_secret"] != nil {
					t.Fatal("pairing changed cloud policy or leaked the gateway key to Agent")
				}
				if reg["live_activity_presentation"] != "agent_status" {
					t.Fatal("gateway discarded the client's presentation contract")
				}
				status := `{"configured":true,"cloud_enabled":true}`
				if requests == 2 {
					status = secondStatus
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(status))}, nil
			})
			for attempt := 1; attempt <= 2; attempt++ {
				s, err := pairing.New(time.Now())
				if err != nil {
					t.Fatal(err)
				}
				d.session = s
				reg := pairingRegistrationFixture(byte(attempt))
				reg["cloud_enabled"] = attempt == 1
				body := encryptedPairingClaim(t, s, s.Code, reg)
				w := httptest.NewRecorder()
				d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(body)))
				var result map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if w.Code != 200 || result["configured"] != true || result["agent_id"] != "mu300-test" {
					t.Fatalf("attempt %d: stored pairing rejected by client contract: HTTP %d, %s", attempt, w.Code, w.Body.String())
				}
				if attempt == 2 {
					var status map[string]any
					json.Unmarshal([]byte(secondStatus), &status)
					if result["push_configured"] != status["configured"] || result["cloud_enabled"] != status["cloud_enabled"] || result["management_blocked"] != status["management_blocked"] {
						t.Fatal("pairing acknowledgement changed cloud readiness or management policy")
					}
				}
				if d.activeSession() != nil || !d.paired {
					t.Fatal("completed session still available")
				}
				if d.gateway.Diagnostics()["paired_clients"] != attempt {
					t.Fatal("successful pairing did not authorize the gateway key")
				}
				w = httptest.NewRecorder()
				d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(body)))
				if w.Code != http.StatusGone || requests != attempt {
					t.Fatal("successful pairing replay reached Agent")
				}
			}
		})
	}
}

func TestPairingDoesNotAcknowledgeRejectedOrInvalidRegistrationResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"rejected", 400, `{"error":"invalid_registration"}`},
		{"unavailable", 503, `{}`},
		{"invalid-json", 200, `{`},
		{"null", 200, `null`},
		{"missing-status", 200, `{}`},
		{"wrong-status-type", 200, `{"configured":"true"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pairingFixture(t, func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			s, err := pairing.New(time.Now())
			if err != nil {
				t.Fatal(err)
			}
			d.session = s
			body := encryptedPairingClaim(t, s, s.Code, pairingRegistrationFixture(1))
			w := httptest.NewRecorder()
			d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(body)))
			if w.Code != http.StatusBadGateway || d.paired || d.gateway.Diagnostics()["paired_clients"] != 0 {
				t.Fatal("failed Agent registration acknowledged as paired")
			}
		})
	}
}

func TestWrongPairingCodeNeverReachesRegistration(t *testing.T) {
	d := pairingFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("wrong code reached Agent")
		return nil, nil
	})
	s, err := pairing.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.session = s
	body := encryptedPairingClaim(t, s, "not-the-code", pairingRegistrationFixture(1))
	w := httptest.NewRecorder()
	d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(body)))
	if w.Code != http.StatusUnauthorized || d.paired || d.gateway.Diagnostics()["paired_clients"] != 0 {
		t.Fatal("wrong code accepted or persisted")
	}
}

func TestPairingInvalidGatewayDoesNotReplaceAgentRegistration(t *testing.T) {
	d := pairingFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid gateway key reached Agent")
		return nil, nil
	})
	reg := pairingRegistrationFixture(99)
	reg["vowlan_secret"] = "invalid-key"
	s, err := pairing.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.session = s
	w := httptest.NewRecorder()
	d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(encryptedPairingClaim(t, s, s.Code, reg))))
	if w.Code != http.StatusBadRequest || d.paired || d.activeSession() != nil {
		t.Fatal("gateway failure accepted")
	}
}

func TestSeventeenthLocalPairingAndStorageFailure(t *testing.T) {
	for _, failStorage := range []bool{false, true} {
		t.Run(fmt.Sprint(failStorage), func(t *testing.T) {
			d := pairingFixture(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("local pairing contacted cloud registration")
				return nil, nil
			})
			path := filepath.Join(t.TempDir(), "keys.json")
			var err error
			d.gateway, err = gateway.New(path, d.deviceID)
			if err != nil {
				t.Fatal(err)
			}
			for i := byte(0); i < 16; i++ {
				if err := d.gateway.Add(pairingRegistrationFixture(i)["vowlan_secret"].(string)); err != nil {
					t.Fatal(err)
				}
			}
			before := d.gateway.Clients()
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if failStorage {
				if err := os.Mkdir(path+".tmp", 0700); err != nil {
					t.Fatal(err)
				}
			}
			s, err := pairing.New(time.Now())
			if err != nil {
				t.Fatal(err)
			}
			d.session = s
			reg := pairingRegistrationFixture(99)
			reg["local_mode"] = true
			body := encryptedPairingClaim(t, s, s.Code, reg)
			w := httptest.NewRecorder()
			d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(body)))
			if failStorage {
				var reply map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
					t.Fatal(err)
				}
				if w.Code != 500 || reply["error"] != "gateway_key_storage_failed" || reply["retry_with_new_code"] != true || reply["message"] == "" || d.paired {
					t.Fatal("storage failure misrepresented")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(original, after) || len(d.gateway.Clients()) != 16 {
					t.Fatal("failed save mutated pairings")
				}
			} else if w.Code != 200 || !d.paired || !strings.Contains(w.Body.String(), `"configured":true`) {
				t.Fatal("17th pairing rejected", w.Code, w.Body.String())
			}
			w = httptest.NewRecorder()
			d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(body)))
			if w.Code != http.StatusGone {
				t.Fatal("spent code replay was not closed")
			}
			w = httptest.NewRecorder()
			d.pairInfo(w, httptest.NewRequest("GET", "/pair/v1/info", nil))
			if w.Code != http.StatusGone || d.activeSession() != nil {
				t.Fatal("spent code still advertised")
			}
			if failStorage {
				if err := os.Remove(path + ".tmp"); err != nil {
					t.Fatal(err)
				}
				s, err = pairing.New(time.Now())
				if err != nil {
					t.Fatal(err)
				}
				d.session = s
				w = httptest.NewRecorder()
				d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(encryptedPairingClaim(t, s, s.Code, reg))))
				if w.Code != 200 {
					t.Fatal("fresh code cannot recover after storage repair")
				}
			}
			restored, err := gateway.New(path, d.deviceID)
			if err != nil || len(restored.Clients()) != 17 {
				t.Fatal("17th pairing not durable", err)
			}
			for i, c := range before {
				if restored.Clients()[i].ID != c.ID {
					t.Fatal("existing pairing changed")
				}
			}
		})
	}
}
