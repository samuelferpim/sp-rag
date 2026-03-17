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

	// Use raw FT.SEARCH to avoid go-redis RESP3 parsing issues with FTSearchWithArgs
	rawResult, err := rc.client.Do(ctx,
		"FT.SEARCH", semanticIndexName, query,
		"PARAMS", "2", "vec", string(vecBytes),
		"SORTBY", "dist",
		"RETURN", "2", "dist", "data",
		"LIMIT", "0", "1",
		"DIALECT", "2",
	).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("semantic cache search: %w", err)
	}

	// Parse the raw response.
	// RESP3 returns a map: {"total_results": N, "results": [{...}, ...]}
	// RESP2 returns an array: [total, docId, [field, val, ...], ...]
	distVal, dataVal, found := parseRawSearchResult(rawResult)
	if !found {
		return nil, 0, nil
	}

	dist, err := strconv.ParseFloat(distVal, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("parse semantic distance: %w", err)
	}
	similarity := 1 - dist

	if similarity < rc.threshold {
		return nil, similarity, nil
	}

	return []byte(dataVal), similarity, nil
}

// parseRawSearchResult handles both RESP2 and RESP3 formats from FT.SEARCH.
func parseRawSearchResult(raw interface{}) (dist string, data string, found bool) {
	// RESP3 format: map with "total_results" and "results" keys
	if m, ok := raw.(map[interface{}]interface{}); ok {
		total, _ := toInt64(m["total_results"])
		if total == 0 {
			return "", "", false
		}
		results, ok := m["results"].([]interface{})
		if !ok || len(results) == 0 {
			return "", "", false
		}
		// Each result is a map with "id" and "extra_attributes"
		firstResult, ok := results[0].(map[interface{}]interface{})
		if !ok {
			return "", "", false
		}
		attrs, ok := firstResult["extra_attributes"].(map[interface{}]interface{})
		if !ok {
			return "", "", false
		}
		distVal := fmt.Sprint(attrs["dist"])
		dataVal := fmt.Sprint(attrs["data"])
		return distVal, dataVal, true
	}

	// RESP2 format: [total, docId, [field, value, ...], ...]
	if arr, ok := raw.([]interface{}); ok {
		if len(arr) < 3 {
			return "", "", false
		}
		total, _ := toInt64(arr[0])
		if total == 0 {
			return "", "", false
		}
		// arr[1] = docId, arr[2] = [field, value, field, value, ...]
		fields, ok := arr[2].([]interface{})
		if !ok {
			return "", "", false
		}
		fieldMap := make(map[string]string)
		for i := 0; i+1 < len(fields); i += 2 {
			fieldMap[fmt.Sprint(fields[i])] = fmt.Sprint(fields[i+1])
		}
		return fieldMap["dist"], fieldMap["data"], fieldMap["dist"] != ""
	}

	return "", "", false
}

// toInt64 converts various numeric types to int64.
func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	}
	return 0, false
}

// SetSemantic stores a response in the semantic cache with its query vector and permission hash.
func (rc *RedisCache) SetSemantic(ctx context.Context, queryVector []float32, permissions []string, data []byte) error {
	permHash := permissionHash(permissions)
	vecBytes := vectorToBytes(queryVector)
	key := semanticPrefix + uuid.New().String()

	pipe := rc.client.Pipeline()

	pipe.HSet(ctx, key, map[string]any{
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
