package platform

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Objects struct {
	Config Config
	HTTP   *http.Client
}

var objectKeyPattern = regexp.MustCompile(`^[a-f0-9-]{36}/[a-f0-9-]{36}\.(csv|json)$`)

func (s Objects) Put(ctx context.Context, key string, data []byte) error {
	if !objectKeyPattern.MatchString(key) {
		return fmt.Errorf("INVALID_OBJECT_KEY")
	}
	if s.Config.R2Endpoint == "" {
		if s.Config.Env == "production" {
			return fmt.Errorf("STORAGE_NOT_CONFIGURED")
		}
		p := filepath.Join(s.Config.StorageDir, key)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		return os.WriteFile(p, data, 0600)
	}
	_, e := s.request(ctx, "PUT", key, nil, data)
	return e
}
func (s Objects) Get(ctx context.Context, key string) ([]byte, error) {
	if !objectKeyPattern.MatchString(key) {
		return nil, fmt.Errorf("INVALID_OBJECT_KEY")
	}
	if s.Config.R2Endpoint == "" {
		if s.Config.Env == "production" {
			return nil, fmt.Errorf("STORAGE_NOT_CONFIGURED")
		}
		return os.ReadFile(filepath.Join(s.Config.StorageDir, key))
	}
	return s.request(ctx, "GET", key, nil, nil)
}
func (s Objects) Delete(ctx context.Context, key string) error {
	if !objectKeyPattern.MatchString(key) {
		return fmt.Errorf("INVALID_OBJECT_KEY")
	}
	if s.Config.R2Endpoint == "" {
		e := os.Remove(filepath.Join(s.Config.StorageDir, key))
		if os.IsNotExist(e) {
			return nil
		}
		return e
	}
	_, e := s.request(ctx, "DELETE", key, nil, nil)
	return e
}
func (s Objects) DeleteWorkspace(ctx context.Context, wid string) error {
	if !objectKeyPattern.MatchString(wid + "/00000000-0000-0000-0000-000000000000.json") {
		return fmt.Errorf("INVALID_WORKSPACE")
	}
	if s.Config.R2Endpoint == "" {
		return os.RemoveAll(filepath.Join(s.Config.StorageDir, wid))
	}
	next := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {wid + "/"}}
		if next != "" {
			q.Set("continuation-token", next)
		}
		b, e := s.request(ctx, "GET", "", q, nil)
		if e != nil {
			return e
		}
		var list struct {
			Items []struct {
				Key string `xml:"Key"`
			} `xml:"Contents"`
			More bool   `xml:"IsTruncated"`
			Next string `xml:"NextContinuationToken"`
		}
		if e = xml.Unmarshal(b, &list); e != nil {
			return e
		}
		for _, v := range list.Items {
			if e = s.Delete(ctx, v.Key); e != nil {
				return e
			}
		}
		if !list.More {
			return nil
		}
		if list.Next == "" || list.Next == next {
			return fmt.Errorf("INVALID_STORAGE_PAGINATION")
		}
		next = list.Next
	}
}
func hmacBytes(key []byte, value string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(value))
	return m.Sum(nil)
}
func (s Objects) request(ctx context.Context, method, key string, q url.Values, data []byte) ([]byte, error) {
	endpoint := strings.TrimRight(s.Config.R2Endpoint, "/") + "/" + s.Config.R2Bucket + "/" + key
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("INVALID_STORAGE_ENDPOINT")
	}
	u.RawQuery = q.Encode()
	now := time.Now().UTC()
	date := now.Format("20060102")
	stamp := now.Format("20060102T150405Z")
	payload := sha256.Sum256(data)
	payloadHash := hex.EncodeToString(payload[:])
	canonicalHeaders := "host:" + u.Host + "\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + stamp + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{method, u.EscapedPath(), u.RawQuery, canonicalHeaders, signedHeaders, payloadHash}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	scope := date + "/auto/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	k := hmacBytes([]byte("AWS4"+s.Config.R2SecretKey), date)
	k = hmacBytes(k, "auto")
	k = hmacBytes(k, "s3")
	k = hmacBytes(k, "aws4_request")
	req, e := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.Config.R2AccessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+hex.EncodeToString(hmacBytes(k, toSign)))
	client := s.HTTP
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("STORAGE_UNAVAILABLE")
	}
	defer res.Body.Close()
	// S3 DELETE is logically idempotent for cleanup. Some compatible stores
	// return 404 for a repeated delete, which is already the desired state.
	if method == http.MethodDelete && res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("STORAGE_UNAVAILABLE")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 64*1024*1024+1))
	if len(b) > 64*1024*1024 {
		return nil, fmt.Errorf("OBJECT_TOO_LARGE")
	}
	return b, e
}
