package assetdownload

import (
	"context"
	"io"
)

type Transport interface {
	Fetch(context.Context, TransportRequest) (TransportResponse, error)
}

type TransportRequest struct {
	URL      string
	Offset   int64
	MaxBytes int64
}

type TransportResponse struct {
	StatusCode    int
	ContentLength int64
	Body          io.ReadCloser
}
