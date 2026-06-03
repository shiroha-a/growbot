package misskey

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateNote(t *testing.T) {
	var capturedPath string
	var capturedContentType string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedContentType = r.Header.Get("Content-Type")

		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &capturedBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"createdNote":{"id":"abc123"}}`))
	}))
	defer srv.Close()

	// 末尾スラッシュがトリムされることも合わせて検証する。
	c := NewClient(srv.URL+"/", "secret-token", srv.Client())

	id, err := c.CreateNote(context.Background(), "hello world")
	require.NoError(t, err)
	require.Equal(t, "abc123", id)

	require.Equal(t, "/api/notes/create", capturedPath)
	require.Equal(t, "application/json", capturedContentType)
	require.Equal(t, "secret-token", capturedBody["i"])
	require.Equal(t, "hello world", capturedBody["text"])
}

func TestCreateNoteAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token","code":"INVALID_TOKEN"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "bad-token", srv.Client())

	id, err := c.CreateNote(context.Background(), "hello")
	require.Error(t, err)
	require.Empty(t, id)

	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, http.StatusBadRequest, apiErr.Status)
	require.Equal(t, "invalid token", apiErr.Message)
	require.Equal(t, "INVALID_TOKEN", apiErr.Code)
}

func TestCreateNoteUnparseableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", srv.Client())

	_, err := c.CreateNote(context.Background(), "hello")
	require.Error(t, err)

	// 標準エラー形式でない場合は*APIErrorにならず、本文とステータスを含む。
	var apiErr *APIError
	require.False(t, errors.As(err, &apiErr))
	require.Contains(t, err.Error(), "500")
	require.Contains(t, err.Error(), "upstream down")
}

func TestI(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &capturedBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"username":"growbot","id":"u1"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "my-token", srv.Client())

	username, err := c.I(context.Background())
	require.NoError(t, err)
	require.Equal(t, "growbot", username)

	require.Equal(t, "/api/i", capturedPath)
	require.Equal(t, "my-token", capturedBody["i"])
}

func TestNewClientDefaultHTTPClient(t *testing.T) {
	c := NewClient("https://example.com/", "tok", nil)
	require.NotNil(t, c.hc)
	require.Equal(t, defaultTimeout, c.hc.Timeout)
	// 末尾スラッシュがトリムされていることを確認する。
	require.Equal(t, "https://example.com", c.baseURL)
}

func TestShowNote(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"repliesCount":2,"renoteCount":1,"reactions":{"a":3,"b":2}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", srv.Client())
	eng, err := c.ShowNote(context.Background(), "note1")
	require.NoError(t, err)
	require.Equal(t, 5, eng.Reactions) // 3 + 2
	require.Equal(t, 2, eng.Replies)
	require.Equal(t, 1, eng.Renotes)
	// リクエストに noteId とトークンが載っていること。
	require.Equal(t, "note1", gotBody["noteId"])
	require.Equal(t, "tok", gotBody["i"])
}

func TestShowNoteNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"No such note.","code":"NO_SUCH_NOTE"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", srv.Client())
	_, err := c.ShowNote(context.Background(), "gone")
	require.ErrorIs(t, err, ErrNoteNotFound)
}

func TestShowNoteNoReactions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"repliesCount":0,"renoteCount":0}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", srv.Client())
	eng, err := c.ShowNote(context.Background(), "n")
	require.NoError(t, err)
	require.Equal(t, 0, eng.Reactions)
}
