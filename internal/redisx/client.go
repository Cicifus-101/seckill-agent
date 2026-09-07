package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"seckill-agent/internal/job"
)

type Config struct {
	Addr     string
	Password string
	DB       int
}

type Client struct {
	client *redis.Client
}

func New(cfg Config) *Client {
	return &Client{
		client: redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
	}
}

func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	value, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return value, err
}

func (c *Client) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *Client) Del(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *Client) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, value, ttl).Result()
}

func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	return c.client.Eval(ctx, script, keys, args...).Result()
}

func (c *Client) RPush(ctx context.Context, key string, values ...any) error {
	return c.client.RPush(ctx, key, values...).Err()
}

func (c *Client) BLPop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error) {
	return c.client.BLPop(ctx, timeout, keys...).Result()
}

func (c *Client) XGroupCreateMkStream(ctx context.Context, stream, group, start string) error {
	return c.client.XGroupCreateMkStream(ctx, stream, group, start).Err()
}

func (c *Client) XReadGroup(ctx context.Context, group, consumer, stream string, count int64, block time.Duration) ([]job.StreamMessage, error) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return flattenStreamMessages(streams), nil
}

func (c *Client) XAutoClaim(ctx context.Context, stream, group, consumer string, minIdle time.Duration, start string, count int64) (string, []job.StreamMessage, error) {
	messages, next, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   stream,
		Group:    group,
		Consumer: consumer,
		MinIdle:  minIdle,
		Start:    start,
		Count:    count,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return start, nil, nil
	}
	if err != nil {
		return start, nil, err
	}
	return next, flattenStreamMessages([]redis.XStream{{Messages: messages}}), nil
}

func (c *Client) XAck(ctx context.Context, stream, group string, ids ...string) error {
	return c.client.XAck(ctx, stream, group, ids...).Err()
}

func (c *Client) XAdd(ctx context.Context, stream string, values map[string]any) (string, error) {
	return c.client.XAdd(ctx, &redis.XAddArgs{Stream: stream, ID: "*", Values: values}).Result()
}

func flattenStreamMessages(streams []redis.XStream) []job.StreamMessage {
	out := make([]job.StreamMessage, 0)
	for _, stream := range streams {
		for _, message := range stream.Messages {
			jobID, ok := message.Values["job_id"]
			if !ok {
				continue
			}
			out = append(out, job.StreamMessage{ID: message.ID, JobID: fmt.Sprint(jobID)})
		}
	}
	return out
}
