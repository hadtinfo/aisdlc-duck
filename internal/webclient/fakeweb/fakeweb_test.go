package fakeweb_test

import (
	"context"
	"testing"
	"time"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

func TestPubkeyAndSignedStateVerifyWithServedKey(t *testing.T) {
	s := fakeweb.New(t)
	c := &webclient.Client{BaseURL: s.URL(), Token: "ingest"}
	var pk struct{ PublicKey, KeyID string }
	if err := c.GetJSON(context.Background(), "/api/cli/pubkey", &pk); err != nil {
		t.Fatal(err)
	}
	key, err := snapshot.PublicKeyFromBase64(pk.PublicKey)
	if err != nil || snapshot.KeyID(key) != pk.KeyID {
		t.Fatalf("khóa: %v", err)
	}
	var env snapshot.SignedEnvelope
	if err := c.PostJSON(context.Background(), "/api/cli/submissions", map[string]string{"gate": "BRD", "hash": "h1"}, &env); err != nil {
		t.Fatal(err)
	}
	payload, ok := env.Verify(key)
	if !ok {
		t.Fatal("chữ ký sai")
	}
	st, err := snapshot.FeatureGateStateFromJSON(payload)
	if err != nil || len(st.Submissions) != 1 || st.Submissions[0].Hash != "h1" {
		t.Fatalf("%+v %v", st, err)
	}
	reqs := s.RequestsFor("POST", "/api/cli/submissions")
	if len(reqs) != 1 || reqs[0].Auth != "Bearer ingest" || reqs[0].Field("gate") != "BRD" {
		t.Errorf("request ghi lại: %+v", reqs)
	}
}

func TestWrongSignerFailsVerification(t *testing.T) {
	s := fakeweb.New(t)
	s2 := fakeweb.New(t)
	s.Update(func(s *fakeweb.Server) { s.Signer = s2.Priv })
	var env snapshot.SignedEnvelope
	c := &webclient.Client{BaseURL: s.URL()}
	if err := c.GetJSON(context.Background(), "/api/cli/gates", &env); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.Verify(s.Pub); ok {
		t.Error("chữ ký của khóa khác phải bị từ chối")
	}
}

func TestOverrideDropAndDelay(t *testing.T) {
	s := fakeweb.New(t)
	c := &webclient.Client{BaseURL: s.URL()}
	s.Update(func(s *fakeweb.Server) { s.Override["GET /api/cli/gates"] = fakeweb.JSONResp(503, `{"error":"bận"}`) })
	if err := c.GetJSON(context.Background(), "/api/cli/gates", nil); err == nil || err.Error() != "Web trả HTTP 503: bận" {
		t.Errorf("override: %v", err)
	}
	s.Update(func(s *fakeweb.Server) { s.Drop = true })
	if err := c.GetJSON(context.Background(), "/api/cli/pubkey", nil); err == nil {
		t.Error("drop phải lỗi mạng")
	}
	s.Update(func(s *fakeweb.Server) { s.Drop = false; s.Hang = true })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.GetJSON(ctx, "/api/cli/pubkey", nil); err == nil {
		t.Error("hang phải hết giờ")
	}
}
