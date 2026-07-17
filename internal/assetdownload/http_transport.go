//go:build !rdev_bootstrap_focused

package assetdownload

import (
	"context"
	"fmt"
	"net/http"
)

type HTTPTransport struct {
	Client *http.Client
}

func (t HTTPTransport) Fetch(ctx context.Context, request TransportRequest) (TransportResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		return TransportResponse{}, err
	}
	if request.Offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", request.Offset))
	}
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return TransportResponse{}, err
	}
	return TransportResponse{
		StatusCode:    resp.StatusCode,
		ContentLength: resp.ContentLength,
		Body:          resp.Body,
	}, nil
}
