package hostawake

import (
	"context"
	"fmt"
)

type Lease struct {
	Enabled bool   `json:"enabled"`
	Method  string `json:"method"`
	Detail  string `json:"detail,omitempty"`
	close   func() error
}

func (l Lease) Close() error {
	if l.close == nil {
		return nil
	}
	return l.close()
}

func Disabled() Lease {
	return Lease{
		Enabled: false,
		Method:  "disabled",
		Detail:  "keep-awake disabled by operator",
	}
}

func unavailable(method string, err error) Lease {
	lease := Lease{
		Enabled: false,
		Method:  method,
	}
	if err != nil {
		lease.Detail = err.Error()
	}
	return lease
}

func commandUnavailable(name string) Lease {
	return unavailable(name, fmt.Errorf("%s unavailable; keep the visible host window active and ensure OS sleep policy allows long-running work", name))
}

func Acquire(ctx context.Context) Lease {
	return acquire(ctx)
}
