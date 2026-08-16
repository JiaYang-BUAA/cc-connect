package weixin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxQuoteRouterResponse = 16 << 10

type quoteRouteRequest struct {
	QuoteText              string `json:"quote_text"`
	ReplyText              string `json:"reply_text"`
	MessageID              string `json:"message_id"`
	UserID                 string `json:"user_id"`
	ReferencedMessageID    string `json:"referenced_message_id,omitempty"`
	ReferencedCreateTimeMs int64  `json:"referenced_create_time_ms,omitempty"`
}

type quoteRouteResponse struct {
	Handled bool   `json:"handled"`
	Message string `json:"message"`
}

type quoteStatusRequest struct {
	MessageID string `json:"message_id"`
	UserID    string `json:"user_id"`
}

func validateQuoteRouterURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("weixin: invalid codex_quote_router_url: %w", err)
	}
	if u.Scheme != "http" {
		return "", fmt.Errorf("weixin: codex_quote_router_url must use http on loopback")
	}
	host := strings.TrimSpace(u.Hostname())
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("weixin: codex_quote_router_url must target loopback")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/route"
	}
	return u.String(), nil
}

func (p *Platform) routeQuotedReply(
	ctx context.Context, quoteText, replyText, messageID, userID,
	referencedMessageID string, referencedCreateTimeMs int64,
) (bool, string, error) {
	if p.quoteRouterURL == "" || p.quoteRouterClient == nil {
		return false, "Codex 引用回复路由尚未启用。", nil
	}
	payload, err := json.Marshal(quoteRouteRequest{
		QuoteText:              quoteText,
		ReplyText:              replyText,
		MessageID:              messageID,
		UserID:                 userID,
		ReferencedMessageID:    referencedMessageID,
		ReferencedCreateTimeMs: referencedCreateTimeMs,
	})
	if err != nil {
		return false, "", err
	}
	return p.postQuoteRouter(ctx, p.quoteRouterURL, payload)
}

func (p *Platform) routePinnedStatus(
	ctx context.Context, messageID, userID string,
) (bool, string, error) {
	if p.quoteRouterURL == "" || p.quoteRouterClient == nil {
		return false, "Codex 任务状态路由尚未启用。", nil
	}
	u, err := url.Parse(p.quoteRouterURL)
	if err != nil {
		return false, "", err
	}
	u.Path = "/status"
	u.RawQuery = ""
	payload, err := json.Marshal(quoteStatusRequest{MessageID: messageID, UserID: userID})
	if err != nil {
		return false, "", err
	}
	return p.postQuoteRouter(ctx, u.String(), payload)
}

func (p *Platform) routePinnedPushToggle(
	ctx context.Context, messageID, userID string,
) (bool, string, error) {
	if p.quoteRouterURL == "" || p.quoteRouterClient == nil {
		return false, "Codex 置顶任务回复推送路由尚未启用。", nil
	}
	u, err := url.Parse(p.quoteRouterURL)
	if err != nil {
		return false, "", err
	}
	u.Path = "/toggle"
	u.RawQuery = ""
	payload, err := json.Marshal(quoteStatusRequest{MessageID: messageID, UserID: userID})
	if err != nil {
		return false, "", err
	}
	return p.postQuoteRouter(ctx, u.String(), payload)
}

func (p *Platform) postQuoteRouter(
	ctx context.Context, endpoint string, payload []byte,
) (bool, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.quoteRouterToken != "" {
		req.Header.Set("X-Codex-Quote-Token", p.quoteRouterToken)
	}
	resp, err := p.quoteRouterClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxQuoteRouterResponse+1))
	if err != nil {
		return false, "", err
	}
	if len(raw) > maxQuoteRouterResponse {
		return false, "", fmt.Errorf("quote router response too large")
	}
	var result quoteRouteResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, "", fmt.Errorf("quote router response: %w", err)
	}
	if strings.TrimSpace(result.Message) == "" && resp.StatusCode >= 400 {
		result.Message = "Codex 引用回复路由暂时不可用。"
	}
	return result.Handled, strings.TrimSpace(result.Message), nil
}

func newQuoteRouterHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
}
