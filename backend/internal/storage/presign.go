package storage

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PresignGet issues a query-string SigV4 URL that lets anyone holding it GET
// the object for ttl. Used for /v1/generate result URLs and reference
// read-back.
func (c *Client) PresignGet(key string, ttl time.Duration) (string, error) {
	return c.presign(http.MethodGet, key, "", ttl)
}

// PresignPut issues a query-string SigV4 URL bound to PUT with the exact
// contentType signed in — the uploader must send that Content-Type verbatim
// (same contract vivid upstreams expect on /v1/uploads sessions).
func (c *Client) PresignPut(key, contentType string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(contentType) == "" {
		return "", fmt.Errorf("presign put requires contentType")
	}
	return c.presign(http.MethodPut, key, contentType, ttl)
}

// presign builds an AWS SigV4 presigned URL (UNSIGNED-PAYLOAD). Signed headers
// are host (+content-type for PUT). Validity is capped at 7 days by SigV4.
func (c *Client) presign(method, key, contentType string, ttl time.Duration) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("rustfs not configured")
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if ttl > 7*24*time.Hour {
		ttl = 7 * 24 * time.Hour
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"

	signedHeaders := "host"
	if contentType != "" {
		signedHeaders = "content-type;host"
	}

	q := map[string]string{
		"X-Amz-Algorithm":     "AWS4-HMAC-SHA256",
		"X-Amz-Credential":    c.ak + "/" + scope,
		"X-Amz-Date":          amzDate,
		"X-Amz-Expires":       strconv.FormatInt(int64(ttl.Seconds()), 10),
		"X-Amz-SignedHeaders": signedHeaders,
	}
	canonicalURI := "/" + uriEncode(c.bucket+"/"+strings.TrimPrefix(key, "/"), true)
	canonicalQuery := canonicalQueryString(q)

	var canonHeaders strings.Builder
	if contentType != "" {
		canonHeaders.WriteString("content-type:" + contentType + "\n")
	}
	canonHeaders.WriteString("host:" + c.host + "\n")

	canonicalRequest := strings.Join([]string{
		method, canonicalURI, canonicalQuery, canonHeaders.String(), signedHeaders, "UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, scope, hexSHA256([]byte(canonicalRequest)),
	}, "\n")
	sig := hex.EncodeToString(hmacSHA256(signingKey(c.sk, dateStamp), []byte(stringToSign)))

	// Rebuild the query with url.Values so the signature chars get escaped
	// correctly, then append X-Amz-Signature verbatim (hex, safe).
	vals := url.Values{}
	for k, v := range q {
		vals.Set(k, v)
	}
	return c.endpoint + canonicalURI + "?" + vals.Encode() + "&X-Amz-Signature=" + sig, nil
}
