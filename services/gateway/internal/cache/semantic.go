package cache

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	semanticPrefix    = "cache:semantic:"
	semanticIndexName = "idx:semantic_cache"
)

// EnsureIndex creates the RediSearch vector index for semantic cache if it doesn't exist.
func (rc *RedisCache) EnsureIndex(ctx context.Context) error {
	// Check if index already exists
	_, err := rc.client.FTInfo(ctx, semanticIndexName).Result()
	if err == nil {
		return nil
	}

	err = rc.client.FTCreate(ctx, semanticIndexName,
		&redis.FTCreateOptions{
			OnHash: true,
			Prefix: []interface{}{semanticPrefix},
		},
		&redis.FieldSchema{
			FieldName: "vec",
			FieldType: redis.SearchFieldTypeVector,
			VectorArgs: &redis.FTVectorArgs{
				FlatOptions: &redis.FTFlatOptions{
					Type:           "FLOAT32",
					Dim:            rc.dims,
					DistanceMetric: "COSINE",
				},
			},
		},
		&redis.FieldSchema{
			FieldName: "perm",
			FieldType: redis.SearchFieldTypeTag,
		},
		&redis.FieldSchema{
			FieldName: "data",
			FieldType: redis.SearchFieldTypeText,
			NoIndex:   true,
		},
	).Err()
	if err != nil {
		return fmt.Errorf("create semantic index: %w", err)
	}

	return nil
}

// GetSemantic searches the semantic cache for a similar query vector with matching permissions.
// Returns (data, similarity, error). data is nil on cache miss or below-threshold match.
func (rc *RedisCache) GetSemantic(ctx context.Context, queryVector []float32, permissions []string) ([]byte, float64, error) {
	permHash := permissionHash(permissions)
	vecBytes := vectorToBytes(queryVector)

	// KNN search filtered by permission hash
	// RediSearch COSINE distance: 0 = identical, 2 = opposite
	query := fmt.Sprintf("(@perm:{%s})=>[KNN 1 @vec $vec AS dist]", permHash)

	res, err := rc.client.FTSearchWithArgs(ctx, semanticIndexName, query, &redis.FTSearchOptions{
		Params: map[string]interface{}{
			"vec": string(vecBytes),
		},
		DialectVersion: 2,
	}).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("semantic cache search: %w", err)
	}

	if res.Total == 0 {
		return nil, 0, nil
	}

	doc := res.Docs[0]

	// Parse distance → similarity (cosine distance is 0-2, similarity = 1 - distance)
	distStr, ok := doc.Fields["dist"]
	if !ok {
		return nil, 0, nil
	}
	dist, err := strconv.ParseFloat(distStr, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("parse semantic distance: %w", err)
	}
	similarity := 1 - dist

	if similarity < rc.threshold {
		return nil, similarity, nil
	}

	data, ok := doc.Fields["data"]
	if !ok {
		return nil, similarity, nil
	}

	return []byte(data), similarity, nil
}

// SetSemantic stores a response in the semantic cache with its query vector and permission hash.
func (rc *RedisCache) SetSemantic(ctx context.Context, queryVector []float32, permissions []string, data []byte) error {
	permHash := permissionHash(permissions)
	vecBytes := vectorToBytes(queryVector)
	key := semanticPrefix + uuid.New().String()

	pipe := rc.client.Pipeline()

	pipe.HSet(ctx, key, map[string]interface{}{
		"vec":  string(vecBytes),
		"perm": permHash,
		"data": string(data),
	})
	pipe.Expire(ctx, key, rc.ttl)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("semantic cache set: %w", err)
	}

	return nil
}

// vectorToBytes converts a float32 slice to little-endian bytes for RediSearch VECTOR fields.
func vectorToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
