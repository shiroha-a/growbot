// Package misskey provides a minimal REST client for the Misskey API.
//
// The client targets only the endpoints required by growbot Phase 0:
// posting notes and verifying the bot account credentials. It depends on
// the standard library exclusively.
package misskey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNoteNotFound is returned by ShowNote when the note no longer exists (e.g.
// it was deleted), so callers can stop polling it.
var ErrNoteNotFound = errors.New("misskey: note not found")

// defaultTimeout bounds every API request when the caller does not supply
// a pre-configured *http.Client.
const defaultTimeout = 15 * time.Second

// Client is a Misskey REST API client bound to a single instance and token.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// NewClient returns a Client for the given Misskey instance base URL and bot
// token. A trailing slash on baseURL is trimmed so endpoint joining is
// predictable. When hc is nil a default *http.Client with a 15s timeout is
// used.
func NewClient(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      hc,
	}
}

// APIError represents a structured error returned by the Misskey API.
type APIError struct {
	Message string
	Code    string
	Status  int
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("misskey api error: status=%d code=%s message=%s", e.Status, e.Code, e.Message)
}

// apiErrorEnvelope mirrors the Misskey error response body shape.
type apiErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// post sends a JSON POST request to {baseURL}/api/{endpoint}.
//
// The bot token is always injected as the "i" field of the payload, so
// callers must not set it themselves. On a non-2xx response the body is
// parsed into an *APIError when possible; otherwise an error carrying the
// status and raw body is returned. When out is non-nil the success body is
// JSON-decoded into it.
func (c *Client) post(ctx context.Context, endpoint string, payload map[string]any, out any) error {
	// トークンは呼び出し側ではなくここで必ず注入する。payloadがnilでも安全に扱う。
	body := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		body[k] = v
	}
	body["i"] = c.token

	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("misskey: marshal payload: %w", err)
	}

	url := c.baseURL + "/api/" + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("misskey: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("misskey: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("misskey: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Misskeyの標準エラー形式を優先して解釈する。解釈できない場合は本文を保持する。
		var env apiErrorEnvelope
		if jsonErr := json.Unmarshal(respBody, &env); jsonErr == nil && (env.Error.Message != "" || env.Error.Code != "") {
			return &APIError{
				Message: env.Error.Message,
				Code:    env.Error.Code,
				Status:  resp.StatusCode,
			}
		}
		return fmt.Errorf("misskey: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// 2xxでも本文が空(204 No Content等)の場合があり、空ボディのデコードは
	// "unexpected end of JSON input" になるため試みない。
	if out != nil && len(bytes.TrimSpace(respBody)) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("misskey: decode response: %w", err)
		}
	}
	return nil
}

// CreateNote posts a note with the given text and returns the created note ID.
func (c *Client) CreateNote(ctx context.Context, text string) (string, error) {
	var out struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	if err := c.post(ctx, "notes/create", map[string]any{"text": text}, &out); err != nil {
		return "", err
	}
	// IDが無い成功応答を空IDで返すと呼び出し側が成功と誤認するため、明示的に失敗させる。
	if out.CreatedNote.ID == "" {
		return "", fmt.Errorf("misskey: notes/create returned empty note id")
	}
	return out.CreatedNote.ID, nil
}

// CreateReply posts a note replying to replyID with the given text and returns
// the created note ID.
func (c *Client) CreateReply(ctx context.Context, text, replyID string) (string, error) {
	var out struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	if err := c.post(ctx, "notes/create", map[string]any{"text": text, "replyId": replyID}, &out); err != nil {
		return "", err
	}
	if out.CreatedNote.ID == "" {
		return "", fmt.Errorf("misskey: notes/create returned empty note id")
	}
	return out.CreatedNote.ID, nil
}

// NoteEngagement is the engagement a note has accrued.
type NoteEngagement struct {
	Reactions int
	Replies   int
	Renotes   int
}

// ShowNote fetches a note's current engagement counts via notes/show. Reactions
// are summed across all emoji.
func (c *Client) ShowNote(ctx context.Context, noteID string) (NoteEngagement, error) {
	var out struct {
		RepliesCount int            `json:"repliesCount"`
		RenoteCount  int            `json:"renoteCount"`
		Reactions    map[string]int `json:"reactions"`
	}
	if err := c.post(ctx, "notes/show", map[string]any{"noteId": noteID}, &out); err != nil {
		// 削除済みなど「ノートが存在しない」失敗は恒久的なので専用エラーで知らせる。
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == "NO_SUCH_NOTE" {
			return NoteEngagement{}, fmt.Errorf("%w: %s", ErrNoteNotFound, noteID)
		}
		return NoteEngagement{}, err
	}
	total := 0
	for _, n := range out.Reactions {
		total += n
	}
	return NoteEngagement{Reactions: total, Replies: out.RepliesCount, Renotes: out.RenoteCount}, nil
}

// I calls the "i" endpoint to verify the credentials and returns the bot
// account's username.
func (c *Client) I(ctx context.Context) (string, error) {
	var out struct {
		Username string `json:"username"`
	}
	if err := c.post(ctx, "i", nil, &out); err != nil {
		return "", err
	}
	if out.Username == "" {
		return "", fmt.Errorf("misskey: i returned empty username")
	}
	return out.Username, nil
}
