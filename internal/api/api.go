package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"x-ui-exporter/internal/metrics"

	"github.com/digilolnet/client3xui"
)

type APIConfig struct {
	BaseURL            string
	ApiUsername        string
	ApiPassword        string
	InsecureSkipVerify bool
	ClientsBytesRows   int
}

type APIClient struct {
	config     APIConfig
	httpClient *http.Client
}

func NewAPIClient(cfg APIConfig) *APIClient {
	return &APIClient{
		config: cfg,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: cfg.InsecureSkipVerify,
				},
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
			Timeout: 30 * time.Second,
		},
	}
}

var (
	cookieCache struct {
		Cookie    http.Cookie
		ExpiresAt time.Time
		sync.Mutex
	}

	// Response object pools
	apiResponsePool = sync.Pool{
		New: func() interface{} {
			return &client3xui.ApiResponse{}
		},
	}
	serverStatusPool = sync.Pool{
		New: func() interface{} {
			return &client3xui.ServerStatusResponse{}
		},
	}
	inboundsResponsePool = sync.Pool{
		New: func() interface{} {
			return &client3xui.GetInboundsResponse{}
		},
	}

	// Buffer pool for request bodies
	bufferPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
)

func (a *APIClient) GetAuthToken() (*http.Cookie, error) {
	cookieCache.Lock()
	defer cookieCache.Unlock()

	remainingTime := time.Until(cookieCache.ExpiresAt).Minutes()
	if cookieCache.Cookie.Name != "" && remainingTime > 0 {
		return &cookieCache.Cookie, nil
	}

	path := a.config.BaseURL + "/login"
	data := url.Values{
		"username": {a.config.ApiUsername},
		"password": {a.config.ApiPassword},
	}

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(data.Encode())
	defer bufferPool.Put(buf)

	req, err := http.NewRequest("POST", path, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var loginResp struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return nil, err
	}

	if !loginResp.Success {
		return nil, fmt.Errorf("authentication failed: %s", loginResp.Msg)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authentication: code %s", resp.Status)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "3x-ui" {
			cookieCache.Cookie = *cookie
			cookieCache.ExpiresAt = time.Now().Add(time.Minute * 59)
		}
	}

	if cookieCache.Cookie.Name == "" {
		return nil, fmt.Errorf("no cookies found in auth response")
	}

	return &cookieCache.Cookie, nil
}

func (a *APIClient) FetchOnlineUsersCount(cookie *http.Cookie) error {
	// Try the new path first for 3X-UI v2.7.0+
	body, err := a.sendRequest("/panel/api/inbounds/onlines", http.MethodPost, cookie)
	if err != nil || len(body) == 0 {
		body, err = a.sendRequest("/panel/inbound/onlines", http.MethodPost, cookie)
		if err != nil {
			return fmt.Errorf("onlines (old path fallback): %w", err)
		}
	}

	response := apiResponsePool.Get().(*client3xui.ApiResponse)
	defer apiResponsePool.Put(response)

	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("unmarshaling response: %w", err)
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(response.Obj, &arr); err != nil {
		return fmt.Errorf("converting Obj as array: %w", err)
	}

	metrics.OnlineUsersCount.Set(float64(len(arr)))

	return nil
}

func (a *APIClient) FetchServerStatus(cookie *http.Cookie) error {
	// Clear old version metric to avoid accumulating obsolete label values
	metrics.XrayVersion.Reset()

	// Try GET first for 3X-UI v2.7.0+
	body, err := a.sendRequest("/panel/api/server/status", http.MethodGet, cookie)
	if err != nil || len(body) == 0 {
		body, err = a.sendRequest("/server/status", http.MethodPost, cookie)
		if err != nil {
			return fmt.Errorf("server status (POST fallback): %w", err)
		}
	}

	response := serverStatusPool.Get().(*client3xui.ServerStatusResponse)
	defer serverStatusPool.Put(response)

	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("unmarshaling response: %w", err)
	}

	// XRay metrics
	xrayVersion := strings.ReplaceAll(response.Obj.Xray.Version, ".", "")
	num, _ := strconv.ParseFloat(xrayVersion, 64)
	metrics.XrayVersion.WithLabelValues(response.Obj.Xray.Version).Set(num)

	// Panel metrics
	metrics.PanelThreads.Set(float64(response.Obj.AppStats.Threads))
	metrics.PanelMemory.Set(float64(response.Obj.AppStats.Mem))
	metrics.PanelUptime.Set(float64(response.Obj.AppStats.Uptime))

	return nil
}

func (a *APIClient) FetchInboundsList(cookie *http.Cookie) error {
	// Clear old metric values to avoid exposing stale data from previous
	// updates. Resetting ensures obsolete label combinations are removed
	// before setting new values.
	metrics.InboundUp.Reset()
	metrics.InboundDown.Reset()
	metrics.ClientUp.Reset()
	metrics.ClientDown.Reset()

	body, err := a.sendRequest("/panel/api/inbounds/list", http.MethodGet, cookie)
	if err != nil {
		return fmt.Errorf("inbounds list: %w", err)
	}

	response := inboundsResponsePool.Get().(*client3xui.GetInboundsResponse)
	defer inboundsResponsePool.Put(response)

	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("unmarshaling response: %w", err)
	}

	for _, inbound := range response.Obj {
		iid := strconv.Itoa(inbound.ID)
		metrics.InboundUp.WithLabelValues(
			iid, inbound.Remark,
		).Set(float64(inbound.Up))

		metrics.InboundDown.WithLabelValues(
			iid, inbound.Remark,
		).Set(float64(inbound.Down))

		n := a.config.ClientsBytesRows
		if n == 0 {
			for _, client := range inbound.ClientStats {
				cid := strconv.Itoa(client.ID)
				metrics.ClientUp.WithLabelValues(
					cid, client.Email,
				).Set(float64(client.Up))

				metrics.ClientDown.WithLabelValues(
					cid, client.Email,
				).Set(float64(client.Down))
			}
		} else {
			// Top N by Upload
			sortedUp := make([]client3xui.ClientStat, len(inbound.ClientStats))
			copy(sortedUp, inbound.ClientStats)
			sort.Slice(sortedUp, func(i, j int) bool {
				return sortedUp[i].Up > sortedUp[j].Up
			})
			for i := 0; i < n && i < len(sortedUp); i++ {
				client := sortedUp[i]
				metrics.ClientUp.WithLabelValues(
					strconv.Itoa(client.ID), client.Email,
				).Set(float64(client.Up))
			}

			// Top N by Download
			sortedDown := make([]client3xui.ClientStat, len(inbound.ClientStats))
			copy(sortedDown, inbound.ClientStats)
			sort.Slice(sortedDown, func(i, j int) bool {
				return sortedDown[i].Down > sortedDown[j].Down
			})
			for i := 0; i < n && i < len(sortedDown); i++ {
				client := sortedDown[i]
				metrics.ClientDown.WithLabelValues(
					strconv.Itoa(client.ID), client.Email,
				).Set(float64(client.Down))
			}
		}
	}

	return nil
}

func (a *APIClient) createRequest(method, path string, cookie *http.Cookie) (*http.Request, error) {
	requestUrl := fmt.Sprintf("%s%s", a.config.BaseURL, path)

	req, err := http.NewRequest(method, requestUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.AddCookie(cookie)
	return req, nil
}

func (a *APIClient) sendRequest(path, method string, cookie *http.Cookie) ([]byte, error) {
	req, err := a.createRequest(method, path, cookie)
	if err != nil {
		return nil, err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	return io.ReadAll(resp.Body)
}
