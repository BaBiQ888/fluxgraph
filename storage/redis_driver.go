package storage

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// GoRedisDriver wraps a standard go-redis client to satisfy our internal 
// decoupled RedisClient interface.
type GoRedisDriver struct {
	client redis.UniversalClient
}

func NewRedisDriver(url string, poolSize int) (*GoRedisDriver, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.PoolSize = poolSize

	client := redis.NewClient(opts)
	// Health check
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &GoRedisDriver{client: client}, nil
}

func (d *GoRedisDriver) Get(ctx context.Context, key string) (string, error) {
	val, err := d.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", ErrRedisKeyNotFound
	}
	return val, err
}

func (d *GoRedisDriver) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return d.client.Set(ctx, key, value, ttl).Err()
}

func (d *GoRedisDriver) RPush(ctx context.Context, key string, values ...string) error {
	return d.client.RPush(ctx, key, values).Err()
}

func (d *GoRedisDriver) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return d.client.LRange(ctx, key, start, stop).Result()
}

func (d *GoRedisDriver) LTrim(ctx context.Context, key string, start, stop int64) error {
	return d.client.LTrim(ctx, key, start, stop).Err()
}

func (d *GoRedisDriver) ZAdd(ctx context.Context, key string, score float64, member string) error {
	return d.client.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

func (d *GoRedisDriver) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return d.client.ZRange(ctx, key, start, stop).Result()
}

func (d *GoRedisDriver) ZRemRangeByRank(ctx context.Context, key string, start, stop int64) error {
	return d.client.ZRemRangeByRank(ctx, key, start, stop).Err()
}

func (d *GoRedisDriver) TxExec(ctx context.Context, fn func(tx RedisClient) error) error {
	_, err := d.client.Pipelined(ctx, func(p redis.Pipeliner) error {
		// Note: go-redis pipelines are for batching, not strict ACID transactions.
		// For strict ACID with WATCH, we would use d.client.Watch().
		// we simplify for the current use case as per tech-selection § 3.1.
		adapter := &pipelinerAdapter{p: p}
		return fn(adapter)
	})
	return err
}

// Internal adapter to make go-redis.Pipeliner satisfy our RedisClient interface.
type pipelinerAdapter struct {
	p redis.Pipeliner
}

func (a *pipelinerAdapter) Get(ctx context.Context, key string) (string, error) {
	// Not typical for write-only pipelines, but satisfying interface.
	return "", nil 
}
func (a *pipelinerAdapter) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return a.p.Set(ctx, key, value, ttl).Err()
}
func (a *pipelinerAdapter) RPush(ctx context.Context, key string, values ...string) error {
	return a.p.RPush(ctx, key, values).Err()
}
func (a *pipelinerAdapter) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return nil, nil
}
func (a *pipelinerAdapter) LTrim(ctx context.Context, key string, start, stop int64) error {
	return a.p.LTrim(ctx, key, start, stop).Err()
}
func (a *pipelinerAdapter) ZAdd(ctx context.Context, key string, score float64, member string) error {
	return a.p.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}
func (a *pipelinerAdapter) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return nil, nil
}
func (a *pipelinerAdapter) ZRemRangeByRank(ctx context.Context, key string, start, stop int64) error {
	return a.p.ZRemRangeByRank(ctx, key, start, stop).Err()
}
func (a *pipelinerAdapter) TxExec(ctx context.Context, fn func(tx RedisClient) error) error {
	return nil // no nested pipelines
}
