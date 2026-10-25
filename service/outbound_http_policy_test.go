package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type outboundRoundTripFunc func(*http.Request) (*http.Response, error)

func (f outboundRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type outboundTrackingBody struct {
	io.Reader
	closed bool
}

func (b *outboundTrackingBody) Close() error {
	b.closed = true
	return nil
}

type recordedOutboundRequest struct {
	url           string
	method        string
	headers       map[string]string
	body          []byte
	key           string
	redirect      string
	redirectToken string
}

type outboundAdapterFixture struct {
	adapter  outboundHTTPAdapter
	requests []recordedOutboundRequest
	steps    []outboundAdapterStep
}

type outboundAdapterStep struct {
	status  int
	headers http.Header
	body    *outboundTrackingBody
	err     error
}

func newOutboundAdapterFixture(t *testing.T, kind string, steps ...outboundAdapterStep) *outboundAdapterFixture {
	t.Helper()
	fixture := &outboundAdapterFixture{steps: steps}
	next := func(request recordedOutboundRequest) (*http.Response, error) {
		fixture.requests = append(fixture.requests, request)
		require.LessOrEqual(t, len(fixture.requests), len(fixture.steps), "adapter received an unexpected request")
		step := fixture.steps[len(fixture.requests)-1]
		if step.err != nil {
			return nil, step.err
		}
		return &http.Response{
			StatusCode: step.status,
			Header:     step.headers.Clone(),
			Body:       step.body,
		}, nil
	}

	switch kind {
	case "direct":
		client := &http.Client{
			Transport: outboundRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var body []byte
				if req.Body != nil {
					var err error
					body, err = io.ReadAll(req.Body)
					require.NoError(t, err)
				}
				return next(recordedOutboundRequest{
					url:     req.URL.String(),
					method:  req.Method,
					headers: flattenOutboundHeaders(req.Header),
					body:    body,
				})
			}),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				t.Fatal("direct adapter must expose redirects to the common policy")
				return nil
			},
		}
		fixture.adapter = directOutboundHTTPAdapter{getClient: func() *http.Client { return client }}
	case "worker":
		redirectTokenCounter := 0
		fixture.adapter = workerOutboundHTTPAdapter{
			settings: system_setting.WorkerSettings{ValidKey: "worker-test-key"},
			newRedirectToken: func() (string, error) {
				redirectTokenCounter++
				return fmt.Sprintf("worker-test-redirect-token-%d", redirectTokenCounter), nil
			},
			do: func(req *WorkerRequest) (*http.Response, error) {
				response, err := next(recordedOutboundRequest{
					url:           req.URL,
					method:        req.Method,
					headers:       cloneOutboundHeaders(req.Headers),
					body:          append([]byte(nil), req.Body...),
					key:           req.Key,
					redirect:      req.Redirect,
					redirectToken: req.RedirectToken,
				})
				if response != nil {
					if response.Header == nil {
						response.Header = make(http.Header)
					}
					response.Header.Set(workerRedirectTokenHeader, req.RedirectToken)
				}
				return response, err
			},
		}
	default:
		t.Fatalf("unknown adapter kind %q", kind)
	}
	return fixture
}

func flattenOutboundHeaders(headers http.Header) map[string]string {
	flattened := make(map[string]string, len(headers))
	for key := range headers {
		flattened[key] = headers.Get(key)
	}
	return flattened
}

func configureOutboundPolicySSRF(t *testing.T) {
	t.Helper()
	testconfig.Set(t, "fetch_setting", map[string]string{
		"enable_ssrf_protection":     "true",
		"allow_private_ip":           "false",
		"domain_filter_mode":         "false",
		"ip_filter_mode":             "false",
		"domain_list":                "[]",
		"ip_list":                    "[]",
		"allowed_ports":              `["80","443","8080"]`,
		"apply_ip_filter_for_domain": "false",
	})
}

func workerSettingsOptionValues(settings system_setting.WorkerSettings) map[string]string {
	allowHTTP := "false"
	if settings.AllowHTTPImageRequest {
		allowHTTP = "true"
	}
	return map[string]string{
		"WorkerUrl":                          settings.URL,
		"WorkerValidKey":                     settings.ValidKey,
		"WorkerAllowHttpImageRequestEnabled": allowHTTP,
	}
}

func TestOutboundHTTPPolicyAdapters(t *testing.T) {
	configureOutboundPolicySSRF(t)

	adapterKinds := []string{"direct", "worker"}
	for _, kind := range adapterKinds {
		t.Run(kind, func(t *testing.T) {
			t.Run("forwards headers and body", func(t *testing.T) {
				body := &outboundTrackingBody{Reader: bytes.NewReader(nil)}
				fixture := newOutboundAdapterFixture(t, kind, outboundAdapterStep{
					status:  http.StatusNoContent,
					headers: make(http.Header),
					body:    body,
				})

				resp, err := doOutboundHTTPRequest(context.Background(), fixture.adapter, outboundRequest{
					URL:    "http://8.8.8.8/notify",
					Method: http.MethodPost,
					Headers: map[string]string{
						"Content-Type": "application/json",
						"User-Agent":   "policy-test/1.0",
						"X-Test":       "kept",
					},
					Body: []byte(`{"ok":true}`),
				}, notificationOutboundPolicy())

				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Len(t, fixture.requests, 1)
				got := fixture.requests[0]
				assert.Equal(t, http.MethodPost, got.method)
				assert.Equal(t, []byte(`{"ok":true}`), got.body)
				assert.Equal(t, "application/json", got.headers["Content-Type"])
				assert.Equal(t, "policy-test/1.0", got.headers["User-Agent"])
				assert.Equal(t, "kept", got.headers["X-Test"])
				assert.True(t, body.closed, "notification policy owns and closes the response body")
				if kind == "worker" {
					assert.Equal(t, "worker-test-key", got.key)
					assert.Equal(t, workerManualRedirectMode, got.redirect)
					assert.Equal(t, "worker-test-redirect-token-1", got.redirectToken)
				}
			})

			t.Run("rejects non-2xx and closes body", func(t *testing.T) {
				body := &outboundTrackingBody{Reader: bytes.NewReader(nil)}
				fixture := newOutboundAdapterFixture(t, kind, outboundAdapterStep{
					status:  http.StatusBadGateway,
					headers: make(http.Header),
					body:    body,
				})

				resp, err := doOutboundHTTPRequest(context.Background(), fixture.adapter, outboundRequest{
					URL:    "http://8.8.8.8/notify",
					Method: http.MethodGet,
				}, notificationOutboundPolicy())

				assert.Nil(t, resp)
				var statusErr *outboundHTTPStatusError
				require.ErrorAs(t, err, &statusErr)
				assert.Equal(t, http.StatusBadGateway, statusErr.StatusCode)
				assert.True(t, body.closed, "an error response must never leak its body")
			})

			t.Run("leaves download body to caller", func(t *testing.T) {
				body := &outboundTrackingBody{Reader: bytes.NewReader([]byte("file"))}
				fixture := newOutboundAdapterFixture(t, kind, outboundAdapterStep{
					status:  http.StatusNotFound,
					headers: make(http.Header),
					body:    body,
				})

				resp, err := doOutboundHTTPRequest(context.Background(), fixture.adapter, outboundRequest{
					URL: "http://8.8.8.8/file",
				}, downloadOutboundPolicy())

				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.False(t, body.closed, "download callers own successful response bodies")
				require.NoError(t, resp.Body.Close())
				assert.True(t, body.closed)
			})

			t.Run("follows redirect with shared method and header rules", func(t *testing.T) {
				redirectBody := &outboundTrackingBody{Reader: bytes.NewReader(nil)}
				finalBody := &outboundTrackingBody{Reader: bytes.NewReader(nil)}
				fixture := newOutboundAdapterFixture(t, kind,
					outboundAdapterStep{
						status: http.StatusFound,
						headers: http.Header{
							"Location": {"http://1.1.1.1/next"},
						},
						body: redirectBody,
					},
					outboundAdapterStep{
						status:  http.StatusNoContent,
						headers: make(http.Header),
						body:    finalBody,
					},
				)

				_, err := doOutboundHTTPRequest(context.Background(), fixture.adapter, outboundRequest{
					URL:    "http://8.8.8.8/start",
					Method: http.MethodPost,
					Headers: map[string]string{
						"Authorization":       "Bearer secret",
						"Content-Type":        "application/json",
						"X-Test":              "kept",
						"X-Webhook-Signature": "signature",
					},
					Body: []byte(`{"redirect":true}`),
				}, notificationOutboundPolicy())

				require.NoError(t, err)
				require.Len(t, fixture.requests, 2)
				if kind == "worker" {
					assert.Equal(t, workerManualRedirectMode, fixture.requests[0].redirect)
					assert.Equal(t, workerManualRedirectMode, fixture.requests[1].redirect)
					assert.NotEmpty(t, fixture.requests[0].redirectToken)
					assert.NotEmpty(t, fixture.requests[1].redirectToken)
					assert.NotEqual(t, fixture.requests[0].redirectToken, fixture.requests[1].redirectToken)
				}
				redirected := fixture.requests[1]
				assert.Equal(t, "http://1.1.1.1/next", redirected.url)
				assert.Equal(t, http.MethodGet, redirected.method)
				assert.Empty(t, redirected.body)
				assert.Equal(t, "kept", redirected.headers["X-Test"])
				assert.NotContains(t, redirected.headers, "Authorization")
				assert.NotContains(t, redirected.headers, "X-Webhook-Signature")
				assert.True(t, redirectBody.closed)
				assert.True(t, finalBody.closed)
			})

			for _, originCase := range []struct {
				name     string
				startURL string
				nextURL  string
			}{
				{
					name:     "strips credentials on HTTPS downgrade",
					startURL: "https://8.8.8.8/start",
					nextURL:  "http://8.8.8.8/next",
				},
				{
					name:     "strips credentials on port change",
					startURL: "http://8.8.8.8/start",
					nextURL:  "http://8.8.8.8:8080/next",
				},
			} {
				t.Run(originCase.name, func(t *testing.T) {
					fixture := newOutboundAdapterFixture(t, kind,
						outboundAdapterStep{
							status: http.StatusTemporaryRedirect,
							headers: http.Header{
								"Location": {originCase.nextURL},
							},
							body: &outboundTrackingBody{Reader: bytes.NewReader(nil)},
						},
						outboundAdapterStep{
							status:  http.StatusNoContent,
							headers: make(http.Header),
							body:    &outboundTrackingBody{Reader: bytes.NewReader(nil)},
						},
					)

					_, err := doOutboundHTTPRequest(context.Background(), fixture.adapter, outboundRequest{
						URL:    originCase.startURL,
						Method: http.MethodPost,
						Headers: map[string]string{
							"Authorization":       "Bearer secret",
							"Cookie":              "session=secret",
							"X-Webhook-Signature": "signature",
							"X-Test":              "kept",
						},
						Body: []byte(`{"kept":true}`),
					}, notificationOutboundPolicy())

					require.NoError(t, err)
					require.Len(t, fixture.requests, 2)
					redirected := fixture.requests[1]
					assert.NotContains(t, redirected.headers, "Authorization")
					assert.NotContains(t, redirected.headers, "Cookie")
					assert.NotContains(t, redirected.headers, "X-Webhook-Signature")
					assert.Equal(t, "kept", redirected.headers["X-Test"])
					assert.Equal(t, http.MethodPost, redirected.method)
					assert.Equal(t, []byte(`{"kept":true}`), redirected.body)
				})
			}

			t.Run("blocks initial SSRF before adapter", func(t *testing.T) {
				fixture := newOutboundAdapterFixture(t, kind)

				resp, err := doOutboundHTTPRequest(context.Background(), fixture.adapter, outboundRequest{
					URL: "http://127.0.0.1/private",
				}, downloadOutboundPolicy())

				assert.Nil(t, resp)
				var rejected *outboundURLRejectedError
				require.ErrorAs(t, err, &rejected)
				assert.False(t, rejected.Redirect)
				assert.Empty(t, fixture.requests)
			})

			t.Run("blocks redirect SSRF and closes prior body", func(t *testing.T) {
				redirectBody := &outboundTrackingBody{Reader: bytes.NewReader(nil)}
				fixture := newOutboundAdapterFixture(t, kind, outboundAdapterStep{
					status: http.StatusTemporaryRedirect,
					headers: http.Header{
						"Location": {"http://127.0.0.1/private"},
					},
					body: redirectBody,
				})

				resp, err := doOutboundHTTPRequest(context.Background(), fixture.adapter, outboundRequest{
					URL: "http://8.8.8.8/start",
				}, downloadOutboundPolicy())

				assert.Nil(t, resp)
				var rejected *outboundURLRejectedError
				require.ErrorAs(t, err, &rejected)
				assert.True(t, rejected.Redirect)
				assert.Len(t, fixture.requests, 1)
				assert.True(t, redirectBody.closed)
			})

			t.Run("closes response returned with adapter error", func(t *testing.T) {
				adapterErr := errors.New("adapter failed")
				body := &outboundTrackingBody{Reader: bytes.NewReader(nil)}
				adapter := outboundHTTPAdapterFunc(func(context.Context, outboundRequest) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusBadGateway, Body: body}, adapterErr
				})

				resp, err := doOutboundHTTPRequest(context.Background(), adapter, outboundRequest{
					URL: "http://8.8.8.8/start",
				}, downloadOutboundPolicy())

				assert.Nil(t, resp)
				assert.ErrorIs(t, err, adapterErr)
				assert.True(t, body.closed)
			})
		})
	}
}

func TestWorkerOutboundHTTPAdapterRequiresManualRedirectAcknowledgement(t *testing.T) {
	configureOutboundPolicySSRF(t)

	for _, testCase := range []struct {
		name                string
		acknowledgement     string
		expectContractError bool
	}{
		{name: "legacy worker without acknowledgement", expectContractError: true},
		{name: "wrong acknowledgement", acknowledgement: "origin-controlled-value", expectContractError: true},
		{name: "matching acknowledgement", acknowledgement: "one-time-token"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := &outboundTrackingBody{Reader: bytes.NewReader([]byte("response"))}
			var workerRequest *WorkerRequest
			adapter := workerOutboundHTTPAdapter{
				settings: system_setting.WorkerSettings{ValidKey: "worker-key"},
				newRedirectToken: func() (string, error) {
					return "one-time-token", nil
				},
				do: func(request *WorkerRequest) (*http.Response, error) {
					workerRequest = request
					header := make(http.Header)
					if testCase.acknowledgement != "" {
						header.Set(workerRedirectTokenHeader, testCase.acknowledgement)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     header,
						Body:       body,
					}, nil
				},
			}

			response, err := doOutboundHTTPRequest(context.Background(), adapter, outboundRequest{
				URL: "http://8.8.8.8/file",
			}, downloadOutboundPolicy())

			require.NotNil(t, workerRequest)
			assert.Equal(t, workerManualRedirectMode, workerRequest.Redirect)
			assert.Equal(t, "one-time-token", workerRequest.RedirectToken)
			if testCase.expectContractError {
				assert.Nil(t, response)
				var contractErr *outboundWorkerRedirectContractError
				require.ErrorAs(t, err, &contractErr)
				assert.True(t, body.closed, "a response from a non-compliant worker must be closed")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.Empty(t, response.Header.Get(workerRedirectTokenHeader), "internal acknowledgement must not reach callers")
			assert.False(t, body.closed, "the caller owns a successful download response")
			require.NoError(t, response.Body.Close())
		})
	}
}

func TestWorkerOutboundHTTPAdapterRequiresTargetBindingAcknowledgement(t *testing.T) {
	binding := &WorkerTargetBinding{
		Version: workerTargetBindingVersion,
		Scheme:  "https",
		Host:    "safe.example",
		Port:    443,
		IPs:     []string{"1.1.1.1", "8.8.8.8"},
	}

	for _, testCase := range []struct {
		name                string
		acknowledge         func(http.Header, string, *WorkerTargetBinding)
		expectContractError bool
	}{
		{
			name:                "missing target acknowledgement",
			expectContractError: true,
		},
		{
			name: "origin controlled headers cannot forge proof",
			acknowledge: func(header http.Header, _ string, _ *WorkerTargetBinding) {
				header.Set(workerTargetIPHeader, "1.1.1.1")
				header.Set(workerTargetProofHeader, "origin-controlled-value")
			},
			expectContractError: true,
		},
		{
			name: "proof for an IP outside the approved set is rejected",
			acknowledge: func(header http.Header, token string, target *WorkerTargetBinding) {
				connectedIP := "127.0.0.1"
				header.Set(workerTargetIPHeader, connectedIP)
				header.Set(workerTargetProofHeader, signWorkerTargetProof(token, *target, connectedIP))
			},
			expectContractError: true,
		},
		{
			name: "matching host and IP binding",
			acknowledge: func(header http.Header, token string, target *WorkerTargetBinding) {
				connectedIP := "1.1.1.1"
				header.Set(workerTargetIPHeader, connectedIP)
				header.Set(workerTargetProofHeader, signWorkerTargetProof(token, *target, connectedIP))
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := &outboundTrackingBody{Reader: bytes.NewReader([]byte("response"))}
			var workerRequest *WorkerRequest
			adapter := workerOutboundHTTPAdapter{
				settings: system_setting.WorkerSettings{ValidKey: "worker-key"},
				newRedirectToken: func() (string, error) {
					return "one-time-token", nil
				},
				resolveTarget: func(context.Context, string) (*WorkerTargetBinding, error) {
					copy := *binding
					copy.IPs = append([]string(nil), binding.IPs...)
					return &copy, nil
				},
				do: func(request *WorkerRequest) (*http.Response, error) {
					workerRequest = request
					header := make(http.Header)
					header.Set(workerRedirectTokenHeader, request.RedirectToken)
					if testCase.acknowledge != nil {
						testCase.acknowledge(header, request.RedirectToken, request.Target)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     header,
						Body:       body,
					}, nil
				},
			}

			response, err := doOutboundHTTPRequest(context.Background(), adapter, outboundRequest{
				URL: "https://safe.example/file",
			}, outboundHTTPPolicy{
				acceptStatus: func(int) bool { return true },
				validateURL:  func(string) error { return nil },
			})

			require.NotNil(t, workerRequest)
			require.NotNil(t, workerRequest.Target)
			assert.Equal(t, binding, workerRequest.Target)
			if testCase.expectContractError {
				assert.Nil(t, response)
				var contractErr *outboundWorkerTargetContractError
				require.ErrorAs(t, err, &contractErr)
				assert.True(t, body.closed, "a response without a valid target binding proof must be closed")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.Empty(t, response.Header.Get(workerTargetIPHeader))
			assert.Empty(t, response.Header.Get(workerTargetProofHeader))
			assert.False(t, body.closed)
			require.NoError(t, response.Body.Close())
		})
	}
}

func TestWorkerOutboundHTTPAdapterKeepsV1CompatibilityWithoutTargetBinding(t *testing.T) {
	body := &outboundTrackingBody{Reader: bytes.NewReader([]byte("response"))}
	adapter := workerOutboundHTTPAdapter{
		newRedirectToken: func() (string, error) {
			return "one-time-token", nil
		},
		resolveTarget: func(context.Context, string) (*WorkerTargetBinding, error) {
			return nil, nil
		},
		do: func(request *WorkerRequest) (*http.Response, error) {
			header := make(http.Header)
			header.Set(workerRedirectTokenHeader, request.RedirectToken)
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: body}, nil
		},
	}

	response, err := adapter.Do(context.Background(), outboundRequest{URL: "https://safe.example/file"})

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Nil(t, response.Request)
	assert.False(t, body.closed)
	require.NoError(t, response.Body.Close())
}

func TestWorkerOutboundHTTPAdapterRefreshesTargetBindingForRedirect(t *testing.T) {
	bindingsByURL := map[string]*WorkerTargetBinding{
		"https://first.example/start": {
			Version: workerTargetBindingVersion,
			Scheme:  "https",
			Host:    "first.example",
			Port:    443,
			IPs:     []string{"1.1.1.1"},
		},
		"https://next.example/file": {
			Version: workerTargetBindingVersion,
			Scheme:  "https",
			Host:    "next.example",
			Port:    443,
			IPs:     []string{"8.8.8.8"},
		},
	}
	var received []WorkerRequest
	tokenCounter := 0
	adapter := workerOutboundHTTPAdapter{
		newRedirectToken: func() (string, error) {
			tokenCounter++
			return fmt.Sprintf("token-%d", tokenCounter), nil
		},
		resolveTarget: func(_ context.Context, rawURL string) (*WorkerTargetBinding, error) {
			binding, ok := bindingsByURL[rawURL]
			if !ok {
				return nil, fmt.Errorf("unexpected target URL %s", rawURL)
			}
			return cloneWorkerTargetBinding(binding), nil
		},
		do: func(request *WorkerRequest) (*http.Response, error) {
			received = append(received, *request)
			connectedIP := request.Target.IPs[0]
			header := make(http.Header)
			header.Set(workerRedirectTokenHeader, request.RedirectToken)
			header.Set(workerTargetIPHeader, connectedIP)
			header.Set(workerTargetProofHeader, signWorkerTargetProof(request.RedirectToken, *request.Target, connectedIP))
			status := http.StatusOK
			if len(received) == 1 {
				status = http.StatusTemporaryRedirect
				header.Set("Location", "https://next.example/file")
			}
			return &http.Response{
				StatusCode: status,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("response")),
			}, nil
		},
	}

	response, err := doOutboundHTTPRequest(context.Background(), adapter, outboundRequest{
		URL: "https://first.example/start",
	}, outboundHTTPPolicy{
		acceptStatus: func(int) bool { return true },
		validateURL:  func(string) error { return nil },
	})

	require.NoError(t, err)
	require.NotNil(t, response)
	require.NoError(t, response.Body.Close())
	require.Len(t, received, 2)
	assert.Equal(t, "token-1", received[0].RedirectToken)
	assert.Equal(t, "token-2", received[1].RedirectToken)
	assert.Equal(t, bindingsByURL["https://first.example/start"], received[0].Target)
	assert.Equal(t, bindingsByURL["https://next.example/file"], received[1].Target)
}

func TestWorkerOutboundHTTPAdapterFailsBeforeRequestWhenTargetBindingFails(t *testing.T) {
	resolveErr := errors.New("resolved target is blocked")
	workerCalled := false
	adapter := workerOutboundHTTPAdapter{
		resolveTarget: func(context.Context, string) (*WorkerTargetBinding, error) {
			return nil, resolveErr
		},
		do: func(*WorkerRequest) (*http.Response, error) {
			workerCalled = true
			return nil, nil
		},
	}

	response, err := adapter.Do(context.Background(), outboundRequest{URL: "https://safe.example/file"})

	assert.Nil(t, response)
	assert.ErrorIs(t, err, resolveErr)
	assert.False(t, workerCalled)
}

func TestResolveWorkerTargetBinding(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		applyIPFilter   bool
		resolved        []net.IPAddr
		expectBinding   *WorkerTargetBinding
		expectErrorText string
	}{
		{
			name:          "binds every validated address in canonical order",
			applyIPFilter: true,
			resolved: []net.IPAddr{
				{IP: net.ParseIP("8.8.8.8")},
				{IP: net.ParseIP("1.1.1.1")},
				{IP: net.ParseIP("8.8.8.8")},
			},
			expectBinding: &WorkerTargetBinding{
				Version: workerTargetBindingVersion,
				Scheme:  "https",
				Host:    "safe.example",
				Port:    443,
				IPs:     []string{"1.1.1.1", "8.8.8.8"},
			},
		},
		{
			name:          "rejects an address set containing a private rebound target",
			applyIPFilter: true,
			resolved: []net.IPAddr{
				{IP: net.ParseIP("8.8.8.8")},
				{IP: net.ParseIP("127.0.0.1")},
			},
			expectErrorText: "private IP address not allowed",
		},
		{
			name:          "keeps v1 when domain IP filtering is disabled",
			applyIPFilter: false,
			resolved:      []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testconfig.Set(t, "fetch_setting", map[string]string{
				"enable_ssrf_protection":     "true",
				"allow_private_ip":           "false",
				"domain_filter_mode":         "false",
				"ip_filter_mode":             "false",
				"domain_list":                "[]",
				"ip_list":                    "[]",
				"allowed_ports":              `["80","443"]`,
				"apply_ip_filter_for_domain": fmt.Sprintf("%t", testCase.applyIPFilter),
			})
			resolver := staticSSRFResolver{"safe.example": testCase.resolved}

			binding, err := resolveWorkerTargetBinding(context.Background(), resolver, "https://SAFE.example/file")

			if testCase.expectErrorText != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.expectErrorText)
				assert.Nil(t, binding)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.expectBinding, binding)
		})
	}
}

func TestWorkerTargetBindingWireContract(t *testing.T) {
	binding := WorkerTargetBinding{
		Version: workerTargetBindingVersion,
		Scheme:  "https",
		Host:    "safe.example",
		Port:    443,
		IPs:     []string{"1.1.1.1", "8.8.8.8"},
	}

	for _, testCase := range []struct {
		name     string
		target   *WorkerTargetBinding
		expected string
	}{
		{
			name:     "v1 omits target",
			expected: `{"url":"https://safe.example/file","key":"worker-key","redirect":"manual","redirect_token":"one-time-token"}`,
		},
		{
			name:     "v2 includes canonical target binding",
			target:   &binding,
			expected: `{"url":"https://safe.example/file","key":"worker-key","redirect":"manual","redirect_token":"one-time-token","target":{"version":"2","scheme":"https","host":"safe.example","port":443,"ips":["1.1.1.1","8.8.8.8"]}}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := json.Marshal(WorkerRequest{
				URL:           "https://safe.example/file",
				Key:           "worker-key",
				Redirect:      workerManualRedirectMode,
				RedirectToken: "one-time-token",
				Target:        testCase.target,
			})

			require.NoError(t, err)
			assert.JSONEq(t, testCase.expected, string(payload))
		})
	}
}

func TestWorkerTargetProofKnownVector(t *testing.T) {
	binding := WorkerTargetBinding{
		Version: workerTargetBindingVersion,
		Scheme:  "https",
		Host:    "safe.example",
		Port:    443,
		IPs:     []string{"1.1.1.1", "8.8.8.8"},
	}

	proof := signWorkerTargetProof("one-time-token", binding, "1.1.1.1")

	assert.Equal(t, "PxgQsqWmBeh6-WFUaWJXeV4r1Y4exb4I7DdS_nTMyyE", proof)
}

func TestWorkerOutboundHTTPAdapterFailsClosedWhenTokenGenerationFails(t *testing.T) {
	tokenErr := errors.New("random source unavailable")
	for _, testCase := range []struct {
		name      string
		token     string
		tokenErr  error
		assertErr func(*testing.T, error)
	}{
		{
			name:     "random source error",
			tokenErr: tokenErr,
			assertErr: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, tokenErr)
			},
		},
		{
			name: "empty token",
			assertErr: func(t *testing.T, err error) {
				assert.EqualError(t, err, "failed to create worker redirect token: token is empty")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			called := false
			adapter := workerOutboundHTTPAdapter{
				newRedirectToken: func() (string, error) {
					return testCase.token, testCase.tokenErr
				},
				do: func(*WorkerRequest) (*http.Response, error) {
					called = true
					return nil, nil
				},
			}

			response, err := adapter.Do(context.Background(), outboundRequest{URL: "https://example.com"})

			assert.Nil(t, response)
			require.Error(t, err)
			testCase.assertErr(t, err)
			assert.False(t, called, "the worker must not receive a request without a verifiable redirect token")
		})
	}
}

func TestWorkerOutboundHTTPAdapterDoesNotFollowWorkerResponseRedirects(t *testing.T) {
	testconfig.Set(t, "fetch_setting", map[string]string{
		"enable_ssrf_protection": "false",
	})

	for _, testCase := range []struct {
		name            string
		status          int
		location        func(string) string
		initialMethod   string
		initialBody     []byte
		expectedNextURL func(string) string
		expectedMethod  string
		expectedBody    []byte
	}{
		{
			name:            "relative 302",
			status:          http.StatusFound,
			location:        func(string) string { return "/next" },
			initialMethod:   http.MethodGet,
			expectedNextURL: func(string) string { return "https://8.8.8.8/next" },
			expectedMethod:  http.MethodGet,
		},
		{
			name:            "absolute 302",
			status:          http.StatusFound,
			location:        func(targetURL string) string { return targetURL + "/next" },
			initialMethod:   http.MethodGet,
			expectedNextURL: func(targetURL string) string { return targetURL + "/next" },
			expectedMethod:  http.MethodGet,
		},
		{
			name:            "absolute 307 keeps sensitive POST inside worker protocol",
			status:          http.StatusTemporaryRedirect,
			location:        func(targetURL string) string { return targetURL + "/next" },
			initialMethod:   http.MethodPost,
			initialBody:     []byte(`{"secret":"payload"}`),
			expectedNextURL: func(targetURL string) string { return targetURL + "/next" },
			expectedMethod:  http.MethodPost,
			expectedBody:    []byte(`{"secret":"payload"}`),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var targetCalls atomic.Int32
			targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer targetServer.Close()

			var replacementWorkerCalls atomic.Int32
			replacementWorkerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				replacementWorkerCalls.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer replacementWorkerServer.Close()

			var workerCalls atomic.Int32
			var escapedWorkerPathCalls atomic.Int32
			workerRequests := make(chan WorkerRequest, 2)
			workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					escapedWorkerPathCalls.Add(1)
					w.WriteHeader(http.StatusTeapot)
					return
				}
				workerCalls.Add(1)
				var request WorkerRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode worker request: %v", err)
					http.Error(w, "invalid worker request", http.StatusBadRequest)
					return
				}
				workerRequests <- request
				w.Header().Set(workerRedirectTokenHeader, request.RedirectToken)
				if workerCalls.Load() == 1 {
					if err := system_setting.UpdateWorkerSettings(map[string]string{
						"WorkerUrl":                          replacementWorkerServer.URL,
						"WorkerValidKey":                     "replacement-worker-key",
						"WorkerAllowHttpImageRequestEnabled": "false",
					}); err != nil {
						t.Errorf("replace worker settings between redirects: %v", err)
					}
					w.Header().Set("Location", testCase.location(targetServer.URL))
					w.WriteHeader(testCase.status)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer workerServer.Close()

			originalWorkerSettings := system_setting.GetWorkerSettings()
			originalHTTPClient := httpClient
			require.NoError(t, system_setting.UpdateWorkerSettings(map[string]string{
				"WorkerUrl":                          workerServer.URL,
				"WorkerValidKey":                     "worker-secret-key",
				"WorkerAllowHttpImageRequestEnabled": "true",
			}))
			httpClient = workerServer.Client()
			t.Cleanup(func() {
				if err := system_setting.UpdateWorkerSettings(workerSettingsOptionValues(originalWorkerSettings)); err != nil {
					t.Errorf("restore worker settings: %v", err)
				}
				httpClient = originalHTTPClient
			})

			adapter, throughWorker := configuredOutboundHTTPAdapter()
			require.True(t, throughWorker)
			response, err := doOutboundHTTPRequest(context.Background(), adapter, outboundRequest{
				URL:    "https://8.8.8.8/start",
				Method: testCase.initialMethod,
				Body:   testCase.initialBody,
			}, outboundHTTPPolicy{
				acceptStatus: func(int) bool { return true },
				validateURL:  func(string) error { return nil },
			})

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, int32(2), workerCalls.Load(), "the policy must make the second worker request")
			assert.Zero(t, replacementWorkerCalls.Load(), "redirects must retain the worker URL captured at operation start")
			assert.Zero(t, escapedWorkerPathCalls.Load(), "relative redirects must not be resolved against WorkerUrl")
			assert.Zero(t, targetCalls.Load(), "the Worker POST and its secrets must never be replayed to Location")

			first := <-workerRequests
			second := <-workerRequests
			assert.Equal(t, workerManualRedirectMode, first.Redirect)
			assert.Equal(t, workerManualRedirectMode, second.Redirect)
			assert.NotEqual(t, first.RedirectToken, second.RedirectToken)
			assert.Equal(t, "worker-secret-key", first.Key)
			assert.Equal(t, first.Key, second.Key, "redirects must retain the worker key captured at operation start")
			assert.Equal(t, testCase.expectedNextURL(targetServer.URL), second.URL)
			assert.Equal(t, testCase.expectedMethod, second.Method)
			assert.Equal(t, testCase.expectedBody, []byte(second.Body))
		})
	}
}

func TestGotifyOutboundRequestUsesOneUserAgent(t *testing.T) {
	request, err := buildGotifyOutboundRequest("https://push.example.com/", "token", 5, "title", "message")
	require.NoError(t, err)
	assert.Equal(t, "NewAPI-Gotify-Notify/1.0", request.Headers["User-Agent"])
}

func TestOutboundNotificationBusinessContracts(t *testing.T) {
	configureOutboundPolicySSRF(t)
	request := outboundRequest{URL: "http://8.8.8.8/notify", Method: http.MethodPost}

	for _, subject := range []string{"webhook", "bark", "gotify"} {
		t.Run(subject, func(t *testing.T) {
			for _, throughWorker := range []bool{false, true} {
				name := "direct"
				expected := "failed to send " + subject + " request: send failed"
				if throughWorker {
					name = "worker"
					expected = "failed to send " + subject + " request through worker: send failed"
				}
				t.Run(name+" send error", func(t *testing.T) {
					adapter := outboundHTTPAdapterFunc(func(context.Context, outboundRequest) (*http.Response, error) {
						return nil, errors.New("send failed")
					})
					err := sendOutboundNotification(context.Background(), adapter, throughWorker, subject, request)
					assert.EqualError(t, err, expected)
				})
			}

			t.Run("status error", func(t *testing.T) {
				body := &outboundTrackingBody{Reader: bytes.NewReader(nil)}
				adapter := outboundHTTPAdapterFunc(func(context.Context, outboundRequest) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusBadGateway,
						Header:     make(http.Header),
						Body:       body,
					}, nil
				})
				err := sendOutboundNotification(context.Background(), adapter, false, subject, request)
				assert.EqualError(t, err, subject+" request failed with status code: 502")
				assert.True(t, body.closed)
			})
		})
	}
}

func TestOutboundNotificationInitialSSRFErrorOwnership(t *testing.T) {
	configureOutboundPolicySSRF(t)
	request := outboundRequest{URL: "http://127.0.0.1/private"}
	adapter := outboundHTTPAdapterFunc(func(context.Context, outboundRequest) (*http.Response, error) {
		t.Fatal("SSRF rejection must happen before the adapter")
		return nil, nil
	})

	directErr := sendOutboundNotification(context.Background(), adapter, false, "webhook", request)
	require.Error(t, directErr)
	assert.True(t, strings.HasPrefix(directErr.Error(), "request reject: "))

	workerErr := sendOutboundNotification(context.Background(), adapter, true, "webhook", request)
	require.Error(t, workerErr)
	assert.True(t, strings.HasPrefix(workerErr.Error(), "failed to send webhook request through worker: request reject: "))
}

func TestOutboundNotificationRequestBuilders(t *testing.T) {
	t.Run("webhook keeps signature and worker authorization", func(t *testing.T) {
		payload := []byte(`{"message":"signed"}`)
		direct := buildWebhookOutboundRequest("https://hooks.example.com", "secret", payload, false)
		worker := buildWebhookOutboundRequest("https://hooks.example.com", "secret", payload, true)

		assert.Equal(t, "application/json", direct.Headers["Content-Type"])
		assert.Equal(t, generateSignature("secret", payload), direct.Headers["X-Webhook-Signature"])
		assert.NotContains(t, direct.Headers, "Authorization")
		assert.Equal(t, "Bearer secret", worker.Headers["Authorization"])
		assert.Equal(t, direct.Headers["X-Webhook-Signature"], worker.Headers["X-Webhook-Signature"])
		assert.Equal(t, payload, worker.Body)
	})

	t.Run("bark keeps escaped template and user agent", func(t *testing.T) {
		request := buildBarkOutboundRequest("https://bark.example.com/{{title}}/{{content}}", "a title", "a/b")
		assert.Equal(t, "https://bark.example.com/a+title/a%2Fb", request.URL)
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "OneAPI-Bark-Notify/1.0", request.Headers["User-Agent"])
	})

	t.Run("gotify keeps payload and clamps priority", func(t *testing.T) {
		request, err := buildGotifyOutboundRequest("https://push.example.com/", "a/b", 99, "title", "message")
		require.NoError(t, err)
		assert.Equal(t, "https://push.example.com/message?token=a%2Fb", request.URL)
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "application/json; charset=utf-8", request.Headers["Content-Type"])
		assert.Equal(t, "NewAPI-Gotify-Notify/1.0", request.Headers["User-Agent"])

		var payload struct {
			Title    string `json:"title"`
			Message  string `json:"message"`
			Priority int    `json:"priority"`
		}
		require.NoError(t, common.Unmarshal(request.Body, &payload))
		assert.Equal(t, "title", payload.Title)
		assert.Equal(t, "message", payload.Message)
		assert.Equal(t, 5, payload.Priority)
	})
}
