package misskey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Stream liveness: ping the server periodically so a silently half-open
// connection (no FIN/RST) is detected and the read loop unblocks to reconnect.
const (
	streamPingInterval = 30 * time.Second
	streamPingTimeout  = 10 * time.Second
)

// Account is the minimal identity of a Misskey account as returned by the
// "i" endpoint.
type Account struct {
	ID       string
	Username string
}

// Me calls the "i" endpoint and returns the authenticated bot account.
//
// It returns an error when the response does not contain an account ID, so
// callers never treat a malformed success response as a valid identity.
func (c *Client) Me(ctx context.Context) (*Account, error) {
	var out struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := c.post(ctx, "i", nil, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("misskey: i returned empty account id")
	}
	return &Account{ID: out.ID, Username: out.Username}, nil
}

// StreamNote is a note delivered over the streaming API, flattened to the
// fields growbot consumes.
type StreamNote struct {
	ID       string
	Text     string
	UserID   string
	Username string
	Host     string
	IsBot    bool
}

// wsURL converts an http(s) base URL into the corresponding ws(s) streaming
// endpoint by swapping the scheme and appending "/streaming".
//
// It returns an error for empty or unparseable input, or for schemes other
// than http/https.
func wsURL(baseURL string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("misskey: empty base url")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("misskey: parse base url: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("misskey: unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("misskey: missing host in base url %q", baseURL)
	}
	// Misskeyのストリーミングは常にルート直下 /streaming で提供されるため、
	// baseURLにサブパスがあっても誤ルーティングしないよう絶対パスで指定する。
	u.Path = "/streaming"
	u.RawQuery = ""
	return u.String(), nil
}

// redactedError carries a sanitized message while preserving the original error
// chain so errors.Is/As still work on the underlying cause.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// redactToken returns an error whose rendered message has the secret token
// replaced with a placeholder, while keeping the original error reachable via
// Unwrap.
//
// ストリーミングURLは ?i=<token> でトークンを運ぶため、ダイヤル失敗時の
// *url.Error 文字列にトークンが含まれる。ログへ平文で漏れないよう伏字化する。
func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, token) {
		return err
	}
	return &redactedError{msg: strings.ReplaceAll(msg, token, "***"), err: err}
}

// streamFrame mirrors the nested envelope of a Misskey streaming message.
//
// Only the fields required to classify the frame and extract a note are
// decoded; every other field is ignored.
type streamFrame struct {
	Type string `json:"type"`
	Body struct {
		// "type" here distinguishes the channel event (e.g. "note").
		Type string `json:"type"`
		Body struct {
			ID     string  `json:"id"`
			Text   *string `json:"text"`
			UserID string  `json:"userId"`
			User   struct {
				Username string  `json:"username"`
				Host     *string `json:"host"`
				IsBot    *bool   `json:"isBot"`
			} `json:"user"`
		} `json:"body"`
	} `json:"body"`
}

// parseStreamMessage decodes a streaming frame, returning the channel event
// type (e.g. "note", "mention", "reply") and the embedded note.
//
// It returns ok=true only for a top-level "channel" frame whose embedded note
// has a non-empty id or a non-nil text; callers filter by the event type. All
// other frames, and any malformed JSON, yield ok=false without panicking.
func parseStreamMessage(data []byte) (StreamNote, string, bool) {
	var f streamFrame
	if err := json.Unmarshal(data, &f); err != nil {
		return StreamNote{}, "", false
	}
	if f.Type != "channel" {
		return StreamNote{}, "", false
	}

	inner := f.Body.Body
	// テキストがnull(*stringがnil)でも、本文が空文字のノートは存在しうる。
	// idがあるか、textが提供されている(nilでない)場合のみノートとして扱う。
	if inner.ID == "" && inner.Text == nil {
		return StreamNote{}, f.Body.Type, false
	}

	note := StreamNote{
		ID:       inner.ID,
		UserID:   inner.UserID,
		Username: inner.User.Username,
	}
	if inner.Text != nil {
		note.Text = *inner.Text
	}
	if inner.User.Host != nil {
		note.Host = *inner.User.Host
	}
	if inner.User.IsBot != nil {
		note.IsBot = *inner.User.IsBot
	}
	return note, f.Body.Type, true
}

// streamChannel connects to a streaming channel and invokes onEvent(eventType,
// note) for each note-bearing frame until ctx is canceled or the connection
// fails.
//
// A canceled or deadline-exceeded context is treated as a clean shutdown and
// returns nil; any other read failure is returned wrapped.
func (c *Client) streamChannel(ctx context.Context, channel, channelID string, onEvent func(eventType string, note StreamNote)) error {
	u, err := wsURL(c.baseURL)
	if err != nil {
		return err
	}
	// トークンはクエリ文字列で渡す。url.QueryEscapeでエスケープを保証する。
	u = u + "?i=" + url.QueryEscape(c.token)

	conn, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		// ダイヤル失敗のエラー文字列にはトークン付きURLが露出するため伏字化する。
		return fmt.Errorf("misskey: streaming dial: %w", redactToken(err, c.token))
	}
	// CloseNowはクローズハンドシェイク(ピアのclose応答待ち)を行わず即座に閉じる。
	// 応答しない/停止したMisskey相手でも終了がブロックしないようにする。
	defer conn.CloseNow()

	// Misskeyのnoteフレームは埋め込みユーザーやリノートを含み、
	// coder/websocketの既定読み取り上限32KiBを超えうるため引き上げる。
	conn.SetReadLimit(1 << 20)

	// 半開接続(TCPが無音で死ぬ)を検知するため定期的にpingを送り、失敗したら
	// readを中断して呼び出し側の再接続ループに復帰できるようにする。
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	pingFailed := make(chan struct{})
	go func() {
		ticker := time.NewTicker(streamPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-readCtx.Done():
				return
			case <-ticker.C:
				pctx, pcancel := context.WithTimeout(readCtx, streamPingTimeout)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil {
					// 親ctxのキャンセルではない=本当にpingが通らない場合のみ中断する。
					if readCtx.Err() == nil {
						close(pingFailed)
						cancelRead()
					}
					return
				}
			}
		}
	}()

	connect := map[string]any{
		"type": "connect",
		"body": map[string]any{
			"channel": channel,
			"id":      channelID,
		},
	}
	payload, err := json.Marshal(connect)
	if err != nil {
		return fmt.Errorf("misskey: marshal connect frame: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		// ctxキャンセルによる書き込み失敗はクリーンな終了として扱う。
		if isContextDone(ctx, err) {
			return nil
		}
		return fmt.Errorf("misskey: write connect frame: %w", err)
	}

	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			// pingが落ちて中断された場合は、再接続を促すためエラーを返す。
			select {
			case <-pingFailed:
				return fmt.Errorf("misskey: streaming ping failed (half-open connection)")
			default:
			}
			if isContextDone(ctx, err) {
				return nil
			}
			return fmt.Errorf("misskey: streaming read: %w", err)
		}
		if note, eventType, ok := parseStreamMessage(data); ok && onEvent != nil {
			onEvent(eventType, note)
		}
	}
}

// StreamLocalTimeline subscribes to the local timeline and invokes onNote for
// each posted note until ctx is canceled.
func (c *Client) StreamLocalTimeline(ctx context.Context, onNote func(StreamNote)) error {
	return c.streamChannel(ctx, "localTimeline", "growbot-local", func(eventType string, note StreamNote) {
		if eventType == "note" && onNote != nil {
			onNote(note)
		}
	})
}

// StreamMain subscribes to the main channel and invokes onMention for each
// mention or reply directed at the bot until ctx is canceled.
func (c *Client) StreamMain(ctx context.Context, onMention func(StreamNote)) error {
	return c.streamChannel(ctx, "main", "growbot-main", func(eventType string, note StreamNote) {
		if (eventType == "mention" || eventType == "reply") && onMention != nil {
			onMention(note)
		}
	})
}

// isContextDone reports whether err originates from ctx being canceled or
// exceeding its deadline, treating those as a clean streaming shutdown.
func isContextDone(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
