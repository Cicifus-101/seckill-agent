package resilience

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type LockClient interface {
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
}

type DistributedLock struct {
	client LockClient
	prefix string
	ttl    time.Duration
}

func (l *DistributedLock) Renew(ctx context.Context, key, token string) (bool, error) {
	const script = `
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0`
	result, err := l.client.Eval(ctx, script, []string{l.prefix + key}, token, l.ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	switch value := result.(type) {
	case int64:
		return value == 1, nil
	case int:
		return value == 1, nil
	default:
		return false, nil
	}
}

func (l *DistributedLock) TTL() time.Duration {
	return l.ttl
}

func NewDistributedLock(client LockClient, prefix string, ttl time.Duration) *DistributedLock {
	if prefix == "" {
		prefix = "agent:lock:"
	}
	if ttl <= 0 {
		ttl = 120 * time.Second
	}
	return &DistributedLock{client: client, prefix: prefix, ttl: ttl}
}

func (l *DistributedLock) Acquire(ctx context.Context, key string) (string, bool, error) {
	token, err := randomToken()
	if err != nil {
		return "", false, err
	}
	ok, err := l.client.SetNX(ctx, l.prefix+key, token, l.ttl)
	if err != nil || !ok {
		return "", ok, err
	}
	return token, true, nil
}

func (l *DistributedLock) Release(ctx context.Context, key string, token string) error {
	const script = `
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0`
	_, err := l.client.Eval(ctx, script, []string{l.prefix + key}, token)
	return err
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("create lock token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
