/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bitwarden

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/bitwarden/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWardenSessionReuse(t *testing.T) {
	previous := newBitwardenClientFn
	t.Cleanup(func() { newBitwardenClientFn = previous })
	var clients []*testClient
	newBitwardenClientFn = func(_, _ *string) (sdk.BitwardenClientInterface, error) {
		c := &testClient{}
		clients = append(clients, c)
		return c, nil
	}
	handler := warden(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "/default")
	send := func(token, path, api, identity string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(WardenHeaderAccessToken, token)
		r.Header.Set(WardenHeaderStatePath, path)
		r.Header.Set(WardenHeaderAPIURL, api)
		r.Header.Set(WardenHeaderIdentityURL, identity)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code)
	}
	send("a", "", "", "")
	send("a", "/default", defaultAPIURL, defaultIdentityURL)
	require.Len(t, clients, 1, "equivalent defaults share a client")
	assert.Equal(t, "/default", *clients[0].statePath)
	send("b", "", "", "")
	send("a", "/other", "", "")
	send("a", "", "https://other-api", "")
	send("a", "", "", "https://other-identity")
	require.Len(t, clients, 5, "credentials, URLs, and effective state path isolate clients")
	assert.Equal(t, "/other", *clients[2].statePath)
	for _, client := range clients {
		assert.False(t, client.closed)
	}
	for i := 5; i <= maxCachedClients; i++ {
		send(fmt.Sprintf("token-%d", i), "", "", "")
	}
	require.Len(t, clients, maxCachedClients+1)
	for _, client := range clients[:maxCachedClients] {
		assert.True(t, client.closed, "eviction closes cached clients")
	}
	assert.False(t, clients[maxCachedClients].closed)
}

func TestWardenNoStateReauthenticates(t *testing.T) {
	previous := newBitwardenClientFn
	t.Cleanup(func() { newBitwardenClientFn = previous })
	var clients []*testClient
	newBitwardenClientFn = func(_, _ *string) (sdk.BitwardenClientInterface, error) {
		c := &testClient{}
		clients = append(clients, c)
		return c, nil
	}
	handler := warden(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "")
	for range 2 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(WardenHeaderAccessToken, testToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code)
	}
	require.Len(t, clients, 2)
	for _, client := range clients {
		assert.Nil(t, client.statePath)
		assert.True(t, client.closed)
	}
	// The per-request header overrides the empty default and enables reuse.
	for range 2 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(WardenHeaderAccessToken, testToken)
		r.Header.Set(WardenHeaderStatePath, "/header")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code)
	}
	require.Len(t, clients, 3)
	assert.Equal(t, "/header", *clients[2].statePath)
	assert.False(t, clients[2].closed)
}

func TestWardenConcurrentSessionReuse(t *testing.T) {
	previous := newBitwardenClientFn
	t.Cleanup(func() { newBitwardenClientFn = previous })
	var count int
	newBitwardenClientFn = func(_, _ *string) (sdk.BitwardenClientInterface, error) {
		count++
		return &testClient{}, nil
	}
	handler := warden(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "/state")
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(WardenHeaderAccessToken, testToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			assert.Equal(t, http.StatusOK, w.Code)
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, count)
}
