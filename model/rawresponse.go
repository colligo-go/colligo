package model

import (
	"time"

	"github.com/colligo-go/colligo/secret"
)

type RawResponse struct {
	Source      string          `json:"source"`
	URL         secret.Endpoint `json:"url"`
	StatusCode  int             `json:"status_code"`
	ContentType string          `json:"content_type"`
	FetchedAt   time.Time       `json:"fetched_at"`
	Body        []byte          `json:"-"`
}
