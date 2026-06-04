package misskey

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestWSURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "http to ws", in: "http://h", want: "ws://h/streaming"},
		{name: "https to wss", in: "https://h", want: "wss://h/streaming"},
		{name: "host with port", in: "http://example.com:3000", want: "ws://example.com:3000/streaming"},
		{name: "empty", in: "", wantErr: true},
		{name: "unsupported scheme", in: "ftp://h", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wsURL(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseStreamMessageValidNote(t *testing.T) {
	frame := []byte(`{
		"type":"channel",
		"body":{
			"id":"growbot-local",
			"type":"note",
			"body":{
				"id":"note1",
				"text":"hello there",
				"userId":"u123",
				"user":{"username":"alice","host":null}
			}
		}
	}`)

	note, evt, ok := parseStreamMessage(frame)
	require.True(t, ok)
	require.Equal(t, "note", evt)
	require.Equal(t, "note1", note.ID)
	require.Equal(t, "hello there", note.Text)
	require.Equal(t, "u123", note.UserID)
	require.Equal(t, "alice", note.Username)
	// hostがnullのときはローカルとして空文字になる。
	require.Equal(t, "", note.Host)
}

func TestParseStreamMessageRemoteHost(t *testing.T) {
	frame := []byte(`{
		"type":"channel",
		"body":{
			"type":"note",
			"body":{
				"id":"note2",
				"text":"remote",
				"userId":"u9",
				"user":{"username":"bob","host":"remote.example"}
			}
		}
	}`)

	note, _, ok := parseStreamMessage(frame)
	require.True(t, ok)
	require.Equal(t, "remote.example", note.Host)
	require.Equal(t, "bob", note.Username)
}

func TestParseStreamMessageNonNote(t *testing.T) {
	// チャンネルフレームでない、または本文にノートが無いものは ok=false。
	cases := map[string][]byte{
		"connected frame": []byte(`{"type":"connected","body":{"id":"growbot-local"}}`),
		"empty note body": []byte(`{"type":"channel","body":{"type":"note","body":{"user":{"username":"a"}}}}`),
		"no note fields":  []byte(`{"type":"channel","body":{"type":"reaction","body":{}}}`),
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, ok := parseStreamMessage(frame)
			require.False(t, ok)
		})
	}
}

// TestParseStreamMessageEventType verifies the channel event type is surfaced so
// callers can filter (e.g. mention vs note).
func TestParseStreamMessageEventType(t *testing.T) {
	mention := []byte(`{"type":"channel","body":{"id":"growbot-main","type":"mention","body":{"id":"n9","text":"@bot hi","userId":"u1","user":{"username":"alice","host":null}}}}`)
	note, evt, ok := parseStreamMessage(mention)
	require.True(t, ok)
	require.Equal(t, "mention", evt)
	require.Equal(t, "n9", note.ID)
	require.Equal(t, "@bot hi", note.Text)
}

func TestParseStreamMessageMalformed(t *testing.T) {
	// 不正なJSONでもpanicせずfalseを返すこと。
	_, _, ok := parseStreamMessage([]byte(`{not json`))
	require.False(t, ok)

	_, _, ok = parseStreamMessage(nil)
	require.False(t, ok)

	_, _, ok = parseStreamMessage([]byte(``))
	require.False(t, ok)
}

func TestMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"acct1","username":"growbot"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", srv.Client())
	acct, err := c.Me(context.Background())
	require.NoError(t, err)
	require.Equal(t, "acct1", acct.ID)
	require.Equal(t, "growbot", acct.Username)
}

func TestMeEmptyID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"username":"growbot"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", srv.Client())
	_, err := c.Me(context.Background())
	require.Error(t, err)
}

// TestStreamTimelineRoundtrip exercises the full dial/connect/read loop
// against a local httptest WebSocket server that echoes a single note frame.
func TestStreamTimelineRoundtrip(t *testing.T) {
	gotConnect := make(chan map[string]any, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()

		// 1) クライアントが送るconnectフレームを受信して検証用に転送する。
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var connect map[string]any
		_ = json.Unmarshal(data, &connect)
		select {
		case gotConnect <- connect:
		default:
		}

		// 2) ノートフレームを1つ返す。
		noteFrame := []byte(`{"type":"channel","body":{"id":"growbot-local","type":"note","body":{"id":"n1","text":"hi","userId":"u1","user":{"username":"alice","host":null}}}}`)
		_ = conn.Write(ctx, websocket.MessageText, noteFrame)

		// 3) ノートフレームではないフレームも送り、無視されることを確かめる。
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"channel","body":{"type":"reaction","body":{"id":"x"}}}`))

		// クライアントの受信を待つため少しブロックする。
		<-ctx.Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret tok", srv.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan StreamNote, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.StreamTimeline(ctx, "localTimeline", func(n StreamNote) {
			select {
			case received <- n:
			default:
			}
			// 最初のノートを受け取ったらストリームを終了させる。
			cancel()
		})
	}()

	select {
	case note := <-received:
		require.Equal(t, "n1", note.ID)
		require.Equal(t, "hi", note.Text)
		require.Equal(t, "u1", note.UserID)
		require.Equal(t, "alice", note.Username)
	case <-ctx.Done():
		t.Fatalf("did not receive note before timeout: %v", ctx.Err())
	}

	// ctxキャンセルでクリーンに終了し、エラーを返さないこと。
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("StreamTimeline did not return after cancel")
	}

	// connectフレームの内容を検証する。
	select {
	case connect := <-gotConnect:
		require.Equal(t, "connect", connect["type"])
		body, ok := connect["body"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "localTimeline", body["channel"])
		require.Equal(t, "growbot-localTimeline", body["id"])
	default:
		t.Fatal("server did not capture a connect frame")
	}
}

func TestStreamTimelineBadURL(t *testing.T) {
	// 不正なbaseURLはダイヤル前にエラーになる。
	c := NewClient("ftp://example.com", "tok", nil)
	err := c.StreamTimeline(context.Background(), "localTimeline", nil)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "scheme"))
}

func TestRedactToken(t *testing.T) {
	err := fmt.Errorf("dial ws://h/streaming?i=SECRET: connection refused")
	red := redactToken(err, "SECRET")
	require.NotContains(t, red.Error(), "SECRET")
	require.Contains(t, red.Error(), "***")
	// 空トークンやnilエラーはそのまま返す。
	require.Equal(t, err, redactToken(err, ""))
	require.Nil(t, redactToken(nil, "x"))
}

func TestStreamDialErrorRedactsToken(t *testing.T) {
	// ダイヤル失敗時のエラーにトークンが含まれないことを検証する。
	const token = "supersecrettoken123"
	c := NewClient("http://127.0.0.1:1", token, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c.StreamTimeline(ctx, "localTimeline", nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), token)
}
