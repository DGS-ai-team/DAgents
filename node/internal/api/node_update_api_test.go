package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/update"
)

func TestNodeUpdateAndVerifiedPackageDownload(t *testing.T) {
	payload := []byte("verified release package")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	packageRequests := 0
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/releases/check":
			if got := r.Header.Get(update.TokenHeader); got != "node-secret" {
				t.Errorf("check token = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"latest":"2.0.0","upgrade_available":true,"asset":{"download_url":"/release/package","filename":"dagents.tar.gz","sha256":"`+checksum+`","size_bytes":`+strconv.Itoa(len(payload))+`}}`)
		case "/release/package":
			packageRequests++
			if got := r.Header.Get(update.TokenHeader); got != "node-secret" {
				t.Errorf("package token = %q", got)
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer manage.Close()

	cfg := testConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.Update.Enabled = boolPtr(true)
	cfg.Manage.URL = manage.URL
	cfg.Manage.NodeToken = "node-secret"
	srv := NewServer(cfg, nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	statusResp, err := http.Get(ts.URL + "/v1/node/update")
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	statusResp.Body.Close()
	if statusResp.StatusCode != http.StatusOK || status["latest_version"] != "2.0.0" || status["upgrade_available"] != true {
		t.Fatalf("status code=%d body=%#v", statusResp.StatusCode, status)
	}
	if _, ok := status["apply_available"]; ok {
		t.Fatalf("Node update status must not claim Shell install ability: %#v", status)
	}

	packageResp, err := http.Get(ts.URL + "/v1/node/update/package")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(packageResp.Body)
	packageResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if packageResp.StatusCode != http.StatusOK || string(got) != string(payload) {
		t.Fatalf("package code=%d body=%q", packageResp.StatusCode, got)
	}
	if packageResp.Header.Get("Content-Type") != "application/octet-stream" || packageResp.Header.Get("X-Node-Package-SHA256") != checksum || packageResp.Header.Get("X-Node-Package-Version") != "2.0.0" {
		t.Fatalf("package headers = %#v", packageResp.Header)
	}
	if packageRequests != 1 {
		t.Fatalf("package requests = %d", packageRequests)
	}
}

func TestNodeUpdatePackageRejectsRemoteAndQueryRequests(t *testing.T) {
	cfg := testConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.Update.Enabled = boolPtr(true)
	cfg.Manage.URL = "http://manage.example.test"
	srv := NewServer(cfg, nil, WithSkipStore())

	remote := httptest.NewRecorder()
	remoteReq := httptest.NewRequest(http.MethodGet, "/v1/node/update/package", nil)
	remoteReq.RemoteAddr = "192.0.2.1:1234"
	srv.Handler().ServeHTTP(remote, remoteReq)
	if remote.Code != http.StatusForbidden {
		t.Fatalf("remote package status = %d", remote.Code)
	}

	query := httptest.NewRecorder()
	queryReq := httptest.NewRequest(http.MethodGet, "/v1/node/update/package?url=http://attacker.test/package", nil)
	queryReq.RemoteAddr = "127.0.0.1:1234"
	srv.Handler().ServeHTTP(query, queryReq)
	if query.Code != http.StatusBadRequest {
		t.Fatalf("query package status = %d", query.Code)
	}
	if _, err := validatedManageAssetURL("https://manage.example.test", "https://evil.example.test/package"); err == nil {
		t.Fatal("foreign origin was accepted")
	}
	if _, err := validatedManageAssetURL("https://manage.example.test", "//manage.example.test:444/package"); err == nil {
		t.Fatal("same host with a different port was accepted")
	}
}

func TestNodeUpdatePackageDoesNotFollowManageRedirect(t *testing.T) {
	redirectRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectRequests++
		if r.Header.Get(update.TokenHeader) != "" {
			t.Errorf("Node token leaked through redirect")
		}
	}))
	defer target.Close()
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/releases/check" {
			_, _ = io.WriteString(w, `{"latest":"2.0.0","upgrade_available":true,"asset":{"download_url":"/package","filename":"pkg","sha256":"`+strings.Repeat("a", 64)+`","size_bytes":1}}`)
			return
		}
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	}))
	defer manage.Close()
	cfg := testConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.Update.Enabled = boolPtr(true)
	cfg.Manage.URL = manage.URL
	cfg.Manage.NodeToken = "node-secret"
	srv := NewServer(cfg, nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	statusResp, err := http.Get(ts.URL + "/v1/node/update")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, statusResp.Body)
	statusResp.Body.Close()
	resp, err := http.Get(ts.URL + "/v1/node/update/package")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || redirectRequests != 0 {
		t.Fatalf("package status=%d redirect target calls=%d", resp.StatusCode, redirectRequests)
	}
}

func TestNodeUpdatePackageRejectsForeignOriginCandidate(t *testing.T) {
	packageRequests := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		packageRequests++
	}))
	defer foreign.Close()
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/releases/check" {
			_, _ = io.WriteString(w, `{"latest":"2.0.0","upgrade_available":true,"asset":{"download_url":"`+foreign.URL+`/package","filename":"pkg","sha256":"`+strings.Repeat("a", 64)+`","size_bytes":1}}`)
			return
		}
		t.Errorf("Manage package endpoint should not be used")
	}))
	defer manage.Close()
	cfg := testConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.Update.Enabled = boolPtr(true)
	cfg.Manage.URL = manage.URL
	cfg.Manage.NodeToken = "node-secret"
	srv := NewServer(cfg, nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	status, err := http.Get(ts.URL + "/v1/node/update")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, status.Body)
	status.Body.Close()
	resp, err := http.Get(ts.URL + "/v1/node/update/package")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || packageRequests != 0 {
		t.Fatalf("package status=%d foreign-origin requests=%d", resp.StatusCode, packageRequests)
	}
}

func TestNodeUpdatePackageRejectsSizeAndChecksumMismatch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		assetSize int
		checksum  string
	}{
		{name: "size", assetSize: 5, checksum: strings.Repeat("a", 64)},
		{name: "checksum", assetSize: 4, checksum: strings.Repeat("a", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/releases/check" {
					_, _ = io.WriteString(w, `{"latest":"2.0.0","upgrade_available":true,"asset":{"download_url":"/package","filename":"pkg","sha256":"`+tc.checksum+`","size_bytes":`+strconv.Itoa(tc.assetSize)+`}}`)
					return
				}
				_, _ = io.WriteString(w, "data")
			}))
			defer manage.Close()
			cfg := testConfig(t)
			cfg.Manage.Enabled = true
			cfg.Manage.Update.Enabled = boolPtr(true)
			cfg.Manage.URL = manage.URL
			srv := NewServer(cfg, nil, WithSkipStore())
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()
			status, err := http.Get(ts.URL + "/v1/node/update")
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, status.Body)
			status.Body.Close()
			resp, err := http.Get(ts.URL + "/v1/node/update/package")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway || strings.Contains(resp.Header.Get("Content-Type"), "octet-stream") {
				t.Fatalf("status=%d content-type=%q body=%s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
			}
		})
	}
}

func TestNodeUpdatePackageRejectsControlCharactersInFilename(t *testing.T) {
	for _, name := range []string{"bad\rname.zip", "bad\nname.zip", "bad\x01name.zip"} {
		if !hasControlCharacters(name) {
			t.Errorf("control character was not detected in %q", name)
		}
	}
}
