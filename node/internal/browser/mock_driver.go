package browser

import "context"

// Driver 向 dagents-browser 发送内部请求。
type Driver interface {
	Call(ctx context.Context, req Request) (Response, error)
	Close() error
}

// MockDriver 供单测使用的内存 driver。
type MockDriver struct {
	Handler func(ctx context.Context, req Request) (Response, error)
	Closed  bool
}

func (m *MockDriver) Call(ctx context.Context, req Request) (Response, error) {
	if m == nil || m.Handler == nil {
		return Response{OK: false, Error: "mock driver not configured"}, nil
	}
	return m.Handler(ctx, req)
}

func (m *MockDriver) Close() error {
	if m != nil {
		m.Closed = true
	}
	return nil
}
