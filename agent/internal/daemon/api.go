package daemon

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tanq16/isane/internal/agentproto"
)

const (
	pollTimeout = 45 * time.Second
	callTimeout = 30 * time.Second
	maxBody     = 8 << 20
)

type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("server answered %d", e.Status)
	}
	return fmt.Sprintf("server answered %d: %s", e.Status, e.Body)
}

func (e *StatusError) Terminal() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

type API struct {
	base   string
	handle string
	token  string
	poll   *http.Client
	call   *http.Client
}

func NewAPI(base, handle, token string) *API {
	return &API{
		base:   strings.TrimSuffix(base, "/"),
		handle: handle,
		token:  token,
		poll:   &http.Client{Timeout: pollTimeout},
		call:   &http.Client{Timeout: callTimeout},
	}
}

func (a *API) Hello(ctx context.Context) error {
	return a.do(ctx, a.call, http.MethodPost, "/api/agent/hello", nil, nil)
}

func (a *API) Register(ctx context.Context, argv []string, allowHistory bool) error {
	return a.do(ctx, a.call, http.MethodPost, "/api/agent/register",
		agentproto.RegisterRequest{Argv: argv, AllowHistory: allowHistory}, nil)
}

func (a *API) Deregister(ctx context.Context) error {
	return a.do(ctx, a.call, http.MethodPost, "/api/agent/deregister", struct{}{}, nil)
}

func (a *API) Jobs(ctx context.Context) ([]agentproto.Job, error) {
	var out agentproto.JobList
	if err := a.do(ctx, a.poll, http.MethodGet, "/api/agent/jobs", nil, &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

func (a *API) PostResult(ctx context.Context, jobID string, res agentproto.ResultRequest) error {
	return a.do(ctx, a.call, http.MethodPost, "/api/agent/jobs/"+jobID+"/result", res, nil)
}

func (a *API) do(ctx context.Context, hc *http.Client, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("X-Isane-Agent", a.handle)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		return &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(detail))}
	}
	if out == nil {
		return nil
	}
	return json.UnmarshalRead(io.LimitReader(resp.Body, maxBody), out)
}
