package plugins

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type CallbackKind string

const (
	CallbackResult   CallbackKind = "result"
	CallbackProgress CallbackKind = "progress"
)

const (
	PathResultCallback   = "/api/v1/jobs/{job_id}/result"
	PathProgressCallback = "/api/v1/jobs/{job_id}/progress"
)

type Signer struct {
	secret  []byte
	baseURL string
}

func NewSigner(secret []byte, baseURL string) *Signer {
	return &Signer{secret: secret, baseURL: baseURL}
}

func (s *Signer) sign(kind CallbackKind, jobID string, exp int64) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(string(kind) + "\n" + jobID + "\n" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Signer) url(kind CallbackKind, path, jobID string, exp int64) string {
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", s.sign(kind, jobID, exp))
	target := strings.TrimRight(s.baseURL, "/") + strings.ReplaceAll(path, "{job_id}", url.PathEscape(jobID))
	return target + "?" + q.Encode()
}

func (s *Signer) Callback(jobID string, deadline time.Time) pluginapi.Callback {
	exp := deadline.Add(10*time.Minute + SignatureGrace).Unix()
	return pluginapi.Callback{
		ResultURL:   s.url(CallbackResult, PathResultCallback, jobID, exp),
		ProgressURL: s.url(CallbackProgress, PathProgressCallback, jobID, exp),
	}
}

func (s *Signer) Verify(kind CallbackKind, jobID string, query url.Values, now time.Time) error {
	exp, err := strconv.ParseInt(query.Get("exp"), 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	got, err := hex.DecodeString(query.Get("sig"))
	if err != nil {
		return ErrInvalidSignature
	}
	want, err := hex.DecodeString(s.sign(kind, jobID, exp))
	if err != nil {
		return ErrInvalidSignature
	}
	if !hmac.Equal(got, want) {
		return ErrInvalidSignature
	}
	if now.Unix() > exp {
		return ErrExpiredSignature
	}
	return nil
}
