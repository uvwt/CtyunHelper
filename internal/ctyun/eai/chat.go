package eai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) QueryTenants(ctx context.Context) ([]Tenant, error) {
	envelope, err := c.requestJSON(ctx, http.MethodGet, "/ai/portal/v2/user/queryUserTenantInfo", nil, nil)
	if err != nil {
		return nil, err
	}
	if envelope.ResultCode != "0" {
		return nil, resultError("获取租户", envelope)
	}
	var values []Tenant
	if err := json.Unmarshal(envelope.Data, &values); err != nil {
		var nested struct {
			Data []Tenant `json:"data"`
		}
		if nestedErr := json.Unmarshal(envelope.Data, &nested); nestedErr != nil {
			return nil, fmt.Errorf("eai: 解析租户列表: %w", err)
		}
		values = nested.Data
	}
	if len(values) > 0 && c.tenant == nil {
		selected := values[0]
		c.tenant = &selected
	}
	return values, nil
}

func (c *Client) QueryModels(ctx context.Context) ([]Model, error) {
	envelope, err := c.requestJSON(ctx, http.MethodGet, "/ai/portal/v2/openai/chat/queryModels", url.Values{"type": {"all"}}, nil)
	if err != nil {
		return nil, err
	}
	if envelope.ResultCode != "0" {
		return nil, resultError("获取模型", envelope)
	}
	var values []Model
	if err := json.Unmarshal(envelope.Data, &values); err != nil {
		return nil, fmt.Errorf("eai: 解析模型列表: %w", err)
	}
	return values, nil
}

func ChooseModel(values []Model) (string, error) {
	available := make([]Model, 0, len(values))
	for _, value := range values {
		if value.KeyModel != "" && strings.EqualFold(value.Status, "avaiable") {
			available = append(available, value)
		}
	}
	if len(available) == 0 {
		for _, value := range values {
			if value.KeyModel != "" {
				available = append(available, value)
			}
		}
	}
	if len(available) == 0 {
		return "", fmt.Errorf("eai: 没有可用模型")
	}
	for _, value := range available {
		if strings.EqualFold(value.Type, "text") || strings.EqualFold(value.Type, "chat") {
			return value.KeyModel, nil
		}
	}
	return available[0].KeyModel, nil
}

func (c *Client) SendMessage(ctx context.Context, text string) (ChatResult, error) {
	if err := c.Authorize(ctx); err != nil {
		return ChatResult{}, err
	}
	tenants, err := c.QueryTenants(ctx)
	if err != nil {
		return ChatResult{}, err
	}
	tenant := c.tenant
	if tenant == nil && len(tenants) > 0 {
		tenant = &tenants[0]
	}
	if tenant == nil || tenant.TenantID == 0 {
		return ChatResult{}, fmt.Errorf("eai: 账号没有可用租户")
	}
	models, err := c.QueryModels(ctx)
	if err != nil {
		return ChatResult{}, err
	}
	keyModel, err := ChooseModel(models)
	if err != nil {
		return ChatResult{}, err
	}
	if strings.TrimSpace(text) == "" {
		text = "你好"
	}
	body := struct {
		KeyModel string `json:"key_model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream         bool  `json:"stream"`
		ClientRetry    bool  `json:"client_retry"`
		WebSearch      bool  `json:"web_search"`
		TenantID       int64 `json:"tenantId"`
		EnableThinking bool  `json:"enable_thinking"`
	}{
		KeyModel: keyModel,
		Stream:   true, ClientRetry: false, WebSearch: false,
		TenantID: tenant.TenantID, EnableThinking: false,
	}
	body.Messages = append(body.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: text})
	rawBody, err := json.Marshal(body)
	if err != nil {
		return ChatResult{}, fmt.Errorf("eai: 编码聊天请求: %w", err)
	}
	path := "/ai/portal/v3/openai/chat/completions"
	headers, err := c.signedHeaders(nil, rawBody, tenant.HeaderID())
	if err != nil {
		return ChatResult{}, err
	}
	headers.Set("Content-Type", "application/json")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.eaiHost+c.rewritePath(path), bytes.NewReader(rawBody))
	if err != nil {
		return ChatResult{}, err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return ChatResult{}, fmt.Errorf("eai: 聊天请求: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ChatResult{}, fmt.Errorf("eai: 聊天 HTTP %d", response.StatusCode)
	}
	events, err := parseSSE(response.Body)
	if err != nil {
		return ChatResult{}, err
	}
	if len(events) == 0 {
		return ChatResult{}, fmt.Errorf("eai: 聊天未返回 SSE 事件")
	}
	result := ChatResult{Model: keyModel, TenantID: tenant.TenantID, EventCount: len(events)}
	for _, event := range events {
		if event.ConversationID != "" {
			result.ConversationID = event.ConversationID
		} else if event.ID != "" {
			result.ConversationID = event.ID
		}
		switch strings.ToLower(event.Status) {
		case "finish", "finished", "done":
			result.Finished = true
		}
	}
	return result, nil
}

func parseSSE(reader io.Reader) ([]chatEvent, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var events []chatEvent
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event chatEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("eai: 读取 SSE: %w", err)
	}
	return events, nil
}
