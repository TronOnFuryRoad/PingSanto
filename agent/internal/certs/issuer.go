package certs

import "context"

type Request struct {
	Server  string
	Token   string
	Labels  map[string]string
	DataDir string
	AgentID string
}

type Response struct {
	AgentID    string
	CertPEM    []byte
	KeyPEM     []byte
	CAPEM      []byte
	ConfigYAML []byte
}

type Issuer interface {
	Enroll(ctx context.Context, req Request) (*Response, error)
}

type NoopIssuer struct{}

func NewNoopIssuer() NoopIssuer {
	return NoopIssuer{}
}

func (NoopIssuer) Enroll(ctx context.Context, req Request) (*Response, error) {
	return &Response{}, nil
}
