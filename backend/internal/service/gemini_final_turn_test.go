//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeGeminiFinalTurn(t *testing.T) {
	for _, action := range []string{"generateContent", "streamGenerateContent"} {
		for _, wrapped := range []bool{false, true} {
			name := action
			if wrapped {
				name += "/wrapped"
			}
			t.Run(name, func(t *testing.T) {
				body := []byte(`{"contents":[{"role":"user","parts":[{"text":"Hello"}]},{"role":"model","parts":[{"text":"Partial answer","thoughtSignature":"signature"}]}],"generationConfig":{"temperature":0.7},"unknown":{"keep":9007199254740993}}`)
				path := "contents"
				if wrapped {
					body = []byte(`{"model":"gemini-3.8-flash-high","project":"test","request":` + string(body) + `}`)
					path = "request.contents"
				}
				original := string(body)
				got := normalizeGeminiFinalTurn(body, action)
				require.Equal(t, original, string(body))
				require.Equal(t, int64(3), gjson.GetBytes(got, path+".#").Int())
				require.Equal(t, gjson.GetBytes(body, path+".0").Raw, gjson.GetBytes(got, path+".0").Raw)
				require.Equal(t, gjson.GetBytes(body, path+".1").Raw, gjson.GetBytes(got, path+".1").Raw)
				require.Equal(t, "user", gjson.GetBytes(got, path+".2.role").String())
				require.Equal(t, "Please continue.", gjson.GetBytes(got, path+".2.parts.0.text").String())
				require.Contains(t, string(got), `"unknown":{"keep":9007199254740993}`)
				require.Equal(t, got, normalizeGeminiFinalTurn(got, action))
			})
		}
	}
}

func TestNormalizeGeminiFinalTurnPreservesOtherRequests(t *testing.T) {
	tests := []struct {
		name   string
		action string
		body   string
	}{
		{"user", "generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`},
		{"implicit user", "generateContent", `{"contents":[{"parts":[{"text":"hi"}]}]}`},
		{"empty", "generateContent", `{"contents":[]}`},
		{"missing", "generateContent", `{}`},
		{"invalid array", "generateContent", `{"contents":{}}`},
		{"invalid json", "generateContent", `{"contents":[`},
		{"empty parts", "generateContent", `{"contents":[{"role":"model","parts":[]}]}`},
		{"count tokens", "countTokens", `{"contents":[{"role":"model","parts":[{"text":"hi"}]}]}`},
		{"function call", "generateContent", `{"contents":[{"role":"model","parts":[{"text":"Calling tool"},{"functionCall":{"name":"lookup","args":{}}}]}]}`},
		{"snake function call", "generateContent", `{"contents":[{"role":"model","parts":[{"function_call":{"name":"lookup","args":{}}}]}]}`},
		{"function response", "generateContent", `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"result":"ok"}}}]}]}`},
		{"claude prefill", "streamGenerateContent", `{"model":"claude-sonnet-4-6","request":{"contents":[{"role":"model","parts":[{"text":"hi"}]}]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.body, string(normalizeGeminiFinalTurn([]byte(test.body), test.action)))
		})
	}
}

func TestGeminiForwardNativeNormalizesFinalTurn(t *testing.T) {
	for _, action := range []string{"generateContent", "streamGenerateContent", "countTokens"} {
		t.Run(action, func(t *testing.T) {
			upstreamBody := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`
			contentType := "application/json"
			if action == "streamGenerateContent" {
				upstreamBody = "data: " + upstreamBody + "\n\n"
				contentType = "text/event-stream"
			} else if action == "countTokens" {
				upstreamBody = `{"totalTokens":1}`
			}
			svc := newGeminiSignalService(contentType, upstreamBody)
			ctx, _ := newGeminiNativeTestContext(t)
			body := []byte(`{"contents":[{"role":"model","parts":[{"text":"partial"}]}]}`)
			_, err := svc.ForwardNative(context.Background(), ctx, geminiSignalTestAccount(), "gemini-3.8-flash-high", action, action == "streamGenerateContent", body)
			require.NoError(t, err)
			upstream := svc.httpUpstream.(*geminiCompatHTTPUpstreamStub)
			sent, err := io.ReadAll(upstream.lastReq.Body)
			require.NoError(t, err)
			if action == "countTokens" {
				require.Equal(t, int64(1), gjson.GetBytes(sent, "contents.#").Int())
			} else {
				require.Equal(t, "user", gjson.GetBytes(sent, "contents.1.role").String())
			}
		})
	}
}

func TestAntigravityRetryLoopNormalizesFinalTurn(t *testing.T) {
	t.Setenv(antigravityForwardBaseURLEnv, "prod")
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}}
	svc := &AntigravityGatewayService{}
	result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:          context.Background(),
		account:      &Account{ID: 1, Platform: PlatformAntigravity},
		action:       "streamGenerateContent",
		body:         []byte(`{"model":"gemini-3.8-flash-high","request":{"contents":[{"role":"model","parts":[{"text":"partial"}]}]}}`),
		httpUpstream: upstream,
	})
	require.NoError(t, err)
	defer result.resp.Body.Close()
	sent, err := io.ReadAll(upstream.lastReq.Body)
	require.NoError(t, err)
	require.Equal(t, "user", gjson.GetBytes(sent, "request.contents.1.role").String())
}

func TestGeminiCompatibilityRoutesNormalizeFinalTurn(t *testing.T) {
	for _, route := range []string{"messages", "chat/completions"} {
		t.Run(route, func(t *testing.T) {
			svc := newGeminiSignalService("application/json", `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`)
			ctx, _ := newGeminiNativeTestContext(t)
			body := []byte(`{"model":"gemini-3.8-flash-high","max_tokens":32,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"partial"}]}`)
			var err error
			if route == "messages" {
				_, err = svc.Forward(context.Background(), ctx, geminiSignalTestAccount(), body)
			} else {
				_, err = svc.ForwardAsChatCompletions(context.Background(), ctx, geminiSignalTestAccount(), body)
			}
			require.NoError(t, err)
			upstream := svc.httpUpstream.(*geminiCompatHTTPUpstreamStub)
			sent, err := io.ReadAll(upstream.lastReq.Body)
			require.NoError(t, err)
			require.Equal(t, "model", gjson.GetBytes(sent, "contents.1.role").String())
			require.Equal(t, "user", gjson.GetBytes(sent, "contents.2.role").String())
		})
	}
}
