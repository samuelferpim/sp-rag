package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Sender knows how to push a text message back to WhatsApp. Declared as an
// interface so tests and the webhook can swap in a stub.
type Sender interface {
	SendText(ctx context.Context, phoneNumberID, to, body string) error
}

// MetaClient is the production Sender — it calls Meta's Graph API for the
// WhatsApp Cloud product: POST /{phone_number_id}/messages.
//
// Docs: https://developers.facebook.com/docs/whatsapp/cloud-api/reference/messages
type MetaClient struct {
	AccessToken string
	GraphURL    string // default "https://graph.facebook.com/v20.0"
	HTTP        *http.Client
}

func NewMetaClient(accessToken string) *MetaClient {
	return &MetaClient{
		AccessToken: accessToken,
		GraphURL:    "https://graph.facebook.com/v20.0",
		HTTP:        &http.Client{Timeout: 10 * time.Second},
	}
}

type textMessage struct {
	MessagingProduct string    `json:"messaging_product"`
	RecipientType    string    `json:"recipient_type"`
	To               string    `json:"to"`
	Type             string    `json:"type"`
	Text             textField `json:"text"`
}

type textField struct {
	PreviewURL bool   `json:"preview_url"`
	Body       string `json:"body"`
}

// SendText posts a text message. Returns nil on 2xx; wraps non-2xx bodies so
// the caller can log the Graph API error code.
func (m *MetaClient) SendText(ctx context.Context, phoneNumberID, to, body string) error {
	if phoneNumberID == "" || to == "" {
		return fmt.Errorf("whatsapp: phone_number_id and to are required")
	}
	payload := textMessage{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               to,
		Type:             "text",
		Text:             textField{Body: body},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/%s/messages", m.GraphURL, phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("whatsapp: graph api %d: %s", resp.StatusCode, string(respBody))
}
