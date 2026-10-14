package adobe

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// captured from live firefly.adobe.com traffic:
//
//	ark: 91818c89a54748463.1048135404|r=ap-southeast-1|…|rid=84|ag=101|…
//	ftr: 40d7ba7c70ff478e9c212073ec8254c4_1791795834777_27064_UDF43-mnf-a4_31ck_s/Clo68HDL4=-3629-v2_tt
//
// The Arkose slot is deliberately emitted empty rather than synthesized — see
// buildARPSessionID — so the expected ftr ends in "_31ck__tt".
var (
	arkPat = regexp.MustCompile(`^[0-9a-f]{17}\.[1-9][0-9]{9}\|r=ap-southeast-1\|.*\|rid=[0-9]{1,2}\|ag=101\|`)
	ftrPat = regexp.MustCompile(`^[0-9a-f]{32}_[0-9]{13}_[0-9]{4,5}_UDF43-mnf-a4_31ck__tt$`)
)

func TestARPSessionIDShape(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(buildARPSessionID("tok-a"))
	if err != nil {
		t.Fatalf("not base64: %v", err)
	}
	var got struct{ Sid, Ark, Ftr, Bfp string }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if len(got.Bfp) != 36 {
		t.Errorf("bfp not a uuid: %q", got.Bfp)
	}
	if !arkPat.MatchString(got.Ark) {
		t.Errorf("ark shape mismatch:\n%s", got.Ark)
	}
	if !ftrPat.MatchString(got.Ftr) {
		t.Errorf("ftr shape mismatch:\n%s", got.Ftr)
	}
	if len(got.Sid) != 36 {
		t.Errorf("sid not a uuid: %q", got.Sid)
	}
}

// ark must differ per call — a frozen blob is a cross-account correlation key.
func TestARKVariesAcrossCalls(t *testing.T) {
	if buildARKBlob() == buildARKBlob() {
		t.Error("ark is constant across calls")
	}
}

// pid is stable per token but distinct across tokens.
func TestPIDStablePerToken(t *testing.T) {
	defer ReleasePID("tok-x")
	defer ReleasePID("tok-y")
	if allocPID("tok-x") != allocPID("tok-x") {
		t.Error("pid changed for the same token")
	}
	if allocPID("tok-x") == allocPID("tok-y") {
		t.Error("two tokens share a pid")
	}
}

// A real browser keeps sid / devHex / bfp for the whole session; only the ftr
// timestamp advances. Re-randomizing them per call is a bot signal.
func TestIdentityStablePerToken(t *testing.T) {
	decode := func(token string) map[string]string {
		raw, err := base64.StdEncoding.DecodeString(buildARPSessionID(token))
		if err != nil {
			t.Fatalf("not base64: %v", err)
		}
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("not json: %v", err)
		}
		return m
	}
	a, b := decode("tok-s"), decode("tok-s")
	if a["sid"] != b["sid"] || a["bfp"] != b["bfp"] {
		t.Error("sid/bfp changed across calls for the same token")
	}
	if a["ftr"][:39] != b["ftr"][:39] {
		t.Error("ftr device hex changed across calls for the same token")
	}
}

// CacheARP stores a cookie-derived arp which getARPSessionID prefers over
// synthesis.
func TestCacheARPPreferred(t *testing.T) {
	c := NewClient("", "")
	CacheARP("tok-c", "eyJjb29raWUiOiJhYXJkZWYa")
	if got := c.getARPSessionID("tok-c"); got != "eyJjb29raWUiOiJhYXJkZWYa" {
		t.Errorf("cached arp not preferred: %q", got)
	}
}

// refreshARPTimestamp bumps only the ftr timestamp segment; devHex, pid, the
// Arkose slot, and every other payload field pass through untouched.
func TestRefreshARPTimestamp(t *testing.T) {
	mk := func(ftr string) string {
		b, _ := json.Marshal(map[string]string{
			"sid": "4e54df9a-79cf-4569-b40d-fa793e38b727",
			"ftr": ftr,
			"ark": "ark_blob",
			"bfp": "bf",
		})
		return base64.StdEncoding.EncodeToString(b)
	}
	old := "40d7ba7c70ff478e9c212073ec8254c4_1791795834777_27064_UDF43-mnf-a4_31ck__tt"
	raw, err := base64.StdEncoding.DecodeString(refreshARPTimestamp(mk(old)))
	if err != nil {
		t.Fatalf("refreshed arp not base64: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not json: %v", err)
	}
	parts := strings.Split(m["ftr"], "_")
	if len(parts) != 7 {
		t.Fatalf("ftr segment count changed: %q", m["ftr"])
	}
	if parts[0] != "40d7ba7c70ff478e9c212073ec8254c4" || parts[2] != "27064" || parts[4] != "31ck" || parts[6] != "tt" {
		t.Errorf("ftr fields mangled: %q", m["ftr"])
	}
	if parts[1] == "1791795834777" {
		t.Error("ftr timestamp not bumped")
	}
	if m["sid"] != "4e54df9a-79cf-4569-b40d-fa793e38b727" || m["ark"] != "ark_blob" || m["bfp"] != "bf" {
		t.Errorf("non-ftr fields mangled: %v", m)
	}
	// malformed inputs pass through unchanged
	if got := refreshARPTimestamp("!!!not-base64"); got != "!!!not-base64" {
		t.Errorf("garbage input changed: %q", got)
	}
	if got := refreshARPTimestamp(mk("too_short")); got != mk("too_short") {
		t.Error("short ftr changed")
	}
}

// ARPFromCookie returns the sherlockToken verbatim when present, and rebuilds
// the JSON from component cookies otherwise.
func TestARPFromCookie(t *testing.T) {
	if got := ARPFromCookie("a=1; sherlockToken=eyJhIjoxfQ==; b=2"); got != "eyJhIjoxfQ==" {
		t.Errorf("sherlockToken not returned verbatim: %q", got)
	}
	if got := ARPFromCookie("a=1"); got != "" {
		t.Errorf("expected empty for cookie without arp pieces, got %q", got)
	}
	raw, err := base64.StdEncoding.DecodeString(ARPFromCookie(
		"ff_session_guid=4e54df9a-79cf-4569-b40d-fa793e38b727; forterToken=ft_x%3D-v2; arkose=ark_blob; bfp=bf"))
	if err != nil {
		t.Fatalf("reassembled arp not base64: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if m["sid"] != "4e54df9a-79cf-4569-b40d-fa793e38b727" || m["ftr"] != "ft_x=-v2" || m["ark"] != "ark_blob" || m["bfp"] != "bf" {
		t.Errorf("reassembled arp mismatch: %v", m)
	}
}
